package mm

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
)

const mbmphSize = 28 // on-disk size of the MBMPH header

// MBMPColor is a palette-indexed color with alpha.
type MBMPColor struct {
	Index uint8 // 8-bit palette index
	A     uint8 // 0 = transparent, 255 = opaque
}

// RGBA implements color.Color. The index is used as a grayscale proxy.
func (c MBMPColor) RGBA() (r, g, b, a uint32) {
	a = uint32(c.A)
	a |= a << 8
	v := uint32(c.Index)
	v |= v << 8
	// premultiply
	r = v * a / 0xffff
	g = r
	b = r
	return
}

type mbmpModel struct{}

func (mbmpModel) Convert(c color.Color) color.Color {
	r, _, _, a := c.RGBA()
	return MBMPColor{Index: uint8(r >> 8), A: uint8(a >> 8)}
}

// MBMPColorModel is the color.Model for MBMPImage pixels.
var MBMPColorModel color.Model = mbmpModel{}

// MBMPImage is a decoded MBMP (Masked Bitmap) chunk.
//
// Pixels are stored in bounding-rect space with stride = Rect.Dx().
// Index i = (y-Rect.Min.Y)*Rect.Dx() + (x-Rect.Min.X).
// Rect coordinates are in MBMP reference-point space; Min.X/Y may be negative.
type MBMPImage struct {
	Pix   []uint8 // palette indices
	Alpha []uint8 // 0 = transparent, 255 = opaque; same layout as Pix
	Rect  image.Rectangle
	Mask  bool   // if true, all opaque pixels use Fill (no per-pixel index stored)
	Fill  uint8  // fill index used when Mask is true
	OSK   uint16 // osk field from MBMPH
}

// ColorModel implements image.Image.
func (m *MBMPImage) ColorModel() color.Model { return MBMPColorModel }

// Bounds implements image.Image.
func (m *MBMPImage) Bounds() image.Rectangle { return m.Rect }

// At implements image.Image.
func (m *MBMPImage) At(x, y int) color.Color {
	if !(image.Point{X: x, Y: y}).In(m.Rect) {
		return MBMPColor{}
	}
	dx := m.Rect.Dx()
	i := (y-m.Rect.Min.Y)*dx + (x - m.Rect.Min.X)
	return MBMPColor{Index: m.Pix[i], A: m.Alpha[i]}
}

// ReadMBMP reads an MBMP chunk from r and returns the decoded image.
func ReadMBMP(r io.Reader) (*MBMPImage, error) {
	hdr := make([]byte, mbmphSize)
	if _, err := io.ReadFull(r, hdr); err != nil {
		return nil, fmt.Errorf("mbmp: read header: %w", err)
	}

	// The bo field is the first 2 bytes, always little-endian on the wire:
	//   0x0001 = file is LE (kboCur)
	//   0x0100 = file is BE (kboOther, written on a big-endian machine)
	boRaw := binary.LittleEndian.Uint16(hdr[0:2])
	var order binary.ByteOrder
	switch boRaw {
	case 0x0001:
		order = binary.LittleEndian
	case 0x0100:
		order = binary.BigEndian
	default:
		return nil, fmt.Errorf("mbmp: unknown byte order 0x%04X", boRaw)
	}

	osk := order.Uint16(hdr[2:4])
	fMask := hdr[4] != 0
	bFill := hdr[5]
	swReserved := order.Uint16(hdr[6:8])
	xpLeft := int(int32(order.Uint32(hdr[8:12])))
	ypTop := int(int32(order.Uint32(hdr[12:16])))
	xpRight := int(int32(order.Uint32(hdr[16:20])))
	ypBottom := int(int32(order.Uint32(hdr[20:24])))
	cb := int(order.Uint32(hdr[24:28]))

	if swReserved != 0 {
		return nil, fmt.Errorf("mbmp: non-zero swReserved 0x%04X", swReserved)
	}

	dxp := xpRight - xpLeft
	dyp := ypBottom - ypTop

	// Empty bounding rect.
	if dxp <= 0 || dyp <= 0 {
		if cb != mbmphSize {
			return nil, fmt.Errorf("mbmp: empty rect but cb=%d, expected %d", cb, mbmphSize)
		}
		return &MBMPImage{
			Rect: image.Rectangle{
				Min: image.Point{X: xpLeft, Y: ypTop},
				Max: image.Point{X: xpLeft, Y: ypTop},
			},
			Mask: fMask,
			Fill: bFill,
			OSK:  osk,
		}, nil
	}

	// Read rgcb: dyp × int16 row byte-lengths.
	rgcbRaw := make([]byte, dyp*2)
	if _, err := io.ReadFull(r, rgcbRaw); err != nil {
		return nil, fmt.Errorf("mbmp: read rgcb: %w", err)
	}
	rgcb := make([]int, dyp)
	pixTotal := 0
	for i := range rgcb {
		v := int(int16(order.Uint16(rgcbRaw[i*2 : i*2+2])))
		if v < 0 {
			return nil, fmt.Errorf("mbmp: negative rgcb[%d]=%d", i, v)
		}
		rgcb[i] = v
		pixTotal += v
	}

	if cb != mbmphSize+dyp*2+pixTotal {
		return nil, fmt.Errorf("mbmp: cb=%d, expected %d", cb, mbmphSize+dyp*2+pixTotal)
	}

	pixData := make([]byte, pixTotal)
	if pixTotal > 0 {
		if _, err := io.ReadFull(r, pixData); err != nil {
			return nil, fmt.Errorf("mbmp: read pixel data: %w", err)
		}
	}

	stride := dxp
	pix := make([]uint8, stride*dyp)
	alpha := make([]uint8, stride*dyp)
	pos := 0
	for y := 0; y < dyp; y++ {
		rowLen := rgcb[y]
		if rowLen == 0 {
			continue // all-transparent row
		}
		rowEnd := pos + rowLen
		x := 0
		for pos < rowEnd {
			transCount := int(pixData[pos])
			pos++
			x += transCount
			if x > dxp {
				return nil, fmt.Errorf("mbmp: row %d: transparent run overflows width (%d > %d)", y, x, dxp)
			}
			if pos >= rowEnd {
				break
			}
			opaqueCount := int(pixData[pos])
			pos++
			if x+opaqueCount > dxp {
				return nil, fmt.Errorf("mbmp: row %d: opaque run overflows width (%d > %d)", y, x+opaqueCount, dxp)
			}
			base := y*stride + x
			for i := 0; i < opaqueCount; i++ {
				alpha[base+i] = 255
			}
			if fMask {
				for i := 0; i < opaqueCount; i++ {
					pix[base+i] = bFill
				}
			} else {
				if pos+opaqueCount > len(pixData) {
					return nil, fmt.Errorf("mbmp: row %d: pixel data out of bounds", y)
				}
				copy(pix[base:base+opaqueCount], pixData[pos:pos+opaqueCount])
				pos += opaqueCount
			}
			x += opaqueCount
		}
		if pos != rowEnd {
			return nil, fmt.Errorf("mbmp: row %d: consumed %d of %d bytes", y, pos-(rowEnd-rowLen), rowLen)
		}
	}

	return &MBMPImage{
		Pix:   pix,
		Alpha: alpha,
		Rect: image.Rectangle{
			Min: image.Point{X: xpLeft, Y: ypTop},
			Max: image.Point{X: xpRight, Y: ypBottom},
		},
		Mask: fMask,
		Fill: bFill,
		OSK:  osk,
	}, nil
}

// WriteMBMP encodes img as an MBMP chunk and writes it to w.
// img.Rect must be tight: the first and last rows must each contain at least
// one opaque pixel. Returns the total number of bytes written.
func WriteMBMP(img *MBMPImage, w io.Writer) (int, error) {
	rect := img.Rect
	dxp := rect.Dx()
	dyp := rect.Dy()

	const kbMax = 255

	rgcb := make([]uint16, dyp)
	rowData := make([][]byte, dyp)

	for y := 0; y < dyp; y++ {
		rowBase := y * dxp

		// Skip rows with no opaque pixels (leave rgcb[y]=0).
		hasOpaque := false
		for x := 0; x < dxp; x++ {
			if img.Alpha[rowBase+x] != 0 {
				hasOpaque = true
				break
			}
		}
		if !hasOpaque {
			continue
		}

		buf := make([]byte, 0, dxp+dxp/2)
		fTrans := true
		cbRun := 0
		for x := 0; ; {
			atEnd := x >= dxp
			var curTrans bool
			if !atEnd {
				curTrans = img.Alpha[rowBase+x] == 0
			}

			if atEnd || curTrans != fTrans || cbRun == kbMax {
				// Skip trailing transparent run at end of row.
				if fTrans && atEnd {
					break
				}
				buf = append(buf, byte(cbRun))
				if !fTrans {
					if !img.Mask {
						buf = append(buf, img.Pix[rowBase+x-cbRun:rowBase+x]...)
					}
					if atEnd {
						break
					}
				}
				cbRun = 0
				fTrans = !fTrans
			} else {
				cbRun++
				x++
			}
		}

		rgcb[y] = uint16(len(buf))
		rowData[y] = buf
	}

	pixTotal := 0
	for _, v := range rgcb {
		pixTotal += int(v)
	}
	cb := mbmphSize + dyp*2 + pixTotal

	// Build header (always write as little-endian / kboCur).
	hdr := make([]byte, mbmphSize)
	binary.LittleEndian.PutUint16(hdr[0:2], 0x0001) // bo = kboCur (LE)
	binary.LittleEndian.PutUint16(hdr[2:4], img.OSK)
	if img.Mask {
		hdr[4] = 1
	}
	hdr[5] = img.Fill
	// hdr[6:8] = swReserved = 0 (zero-initialized)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(int32(rect.Min.X)))
	binary.LittleEndian.PutUint32(hdr[12:16], uint32(int32(rect.Min.Y)))
	binary.LittleEndian.PutUint32(hdr[16:20], uint32(int32(rect.Max.X)))
	binary.LittleEndian.PutUint32(hdr[20:24], uint32(int32(rect.Max.Y)))
	binary.LittleEndian.PutUint32(hdr[24:28], uint32(cb))

	total := 0
	n, err := w.Write(hdr)
	total += n
	if err != nil {
		return total, err
	}

	// Write rgcb.
	rgcbBytes := make([]byte, dyp*2)
	for i, v := range rgcb {
		binary.LittleEndian.PutUint16(rgcbBytes[i*2:i*2+2], v)
	}
	n, err = w.Write(rgcbBytes)
	total += n
	if err != nil {
		return total, err
	}

	// Write pixel data rows.
	for _, row := range rowData {
		if len(row) == 0 {
			continue
		}
		n, err = w.Write(row)
		total += n
		if err != nil {
			return total, err
		}
	}

	return total, nil
}
