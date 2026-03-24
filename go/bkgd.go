package mm

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
)

// ZBMPImage is a decoded ZBMP (Z-buffer) chunk.
//
// Pixels are stored in bounding-rect space with stride = Rect.Dx().
// Index i = (y-Rect.Min.Y)*Rect.Dx() + (x-Rect.Min.X).
// Values are 16-bit z-depths: 0x0000 = closest, 0xFFFF = farthest (cleared).
type ZBMPImage struct {
	Pix  []uint16 // z-values, row-major
	Rect image.Rectangle
}

// ReadZBMP reads a ZBMP chunk from r and returns the decoded z-buffer.
func ReadZBMP(r io.Reader) (*ZBMPImage, error) {
	// ZBMPF header: bo(2), osk(2), xpLeft(2), ypTop(2), dxp(2), dyp(2) = 12 bytes
	var hdr [12]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("zbmp: read header: %w", err)
	}
	bo := int16(binary.LittleEndian.Uint16(hdr[0:2]))
	var u16 func([]byte) uint16
	switch bo {
	case 0x0001:
		u16 = binary.LittleEndian.Uint16
	case 0x0100:
		u16 = binary.BigEndian.Uint16
	default:
		return nil, fmt.Errorf("zbmp: unknown byte order 0x%04X", uint16(bo))
	}
	xpLeft := int(int16(u16(hdr[4:6])))
	ypTop := int(int16(u16(hdr[6:8])))
	dxp := int(int16(u16(hdr[8:10])))
	dyp := int(int16(u16(hdr[10:12])))
	if dxp <= 0 || dyp <= 0 {
		return nil, fmt.Errorf("zbmp: invalid dimensions %dx%d", dxp, dyp)
	}
	raw := make([]byte, dxp*dyp*2)
	if _, err := io.ReadFull(r, raw); err != nil {
		return nil, fmt.Errorf("zbmp: read pixels: %w", err)
	}
	pix := make([]uint16, dxp*dyp)
	for i := range pix {
		pix[i] = u16(raw[i*2:])
	}
	return &ZBMPImage{
		Pix:  pix,
		Rect: image.Rect(xpLeft, ypTop, xpLeft+dxp, ypTop+dyp),
	}, nil
}

// CameraAngle holds the decoded background image for one camera position.
type CameraAngle struct {
	Index int        // 0-based camera index (CHID of the CAM chunk)
	Img   *MBMPImage // palette-indexed background bitmap
	ZBuf  *ZBMPImage // z-buffer, or nil if no ZBMP child in this CAM
}

// BackgroundScene is a decoded BKGD chunk with all camera angles.
type BackgroundScene struct {
	Palette   Palette       // effective palette (global patched with GLCR entries)
	IndexBase int           // first palette slot overridden by this background's GLCR
	Angles    []CameraAngle // one per CAM child, in CHID order
}

// GrayscalePalette returns a 256-entry palette where entry i = RGBA{i,i,i,255}.
func GrayscalePalette() Palette {
	colors := make([]color.Color, 256)
	for i := range colors {
		v := uint8(i)
		colors[i] = color.RGBA{R: v, G: v, B: v, A: 0xFF}
	}
	return Palette{Colors: colors}
}

// FindGLCR searches cf for a top-level GLCR chunk, decodes it, and returns a
// full 256-entry palette. It tries CNO 0 first, then the first 256-entry
// GLCR, then any GLCR. Returns (palette, true, nil) on success or
// (Palette{}, false, nil) if no GLCR chunk is present.
func FindGLCR(cf *ChunkyFile, r io.ReaderAt) (Palette, bool, error) {
	// Collect all top-level GLCR chunks.
	var glcrChunks []Chunk
	for _, c := range cf.Chunks {
		if c.CTG == TagGLCR {
			glcrChunks = append(glcrChunks, c)
		}
	}
	if len(glcrChunks) == 0 {
		return Palette{}, false, nil
	}

	// Try CNO 0 first.
	candidate := glcrChunks[0]
	for _, c := range glcrChunks {
		if c.CNO == 0 {
			candidate = c
			break
		}
	}

	// If CNO 0 wasn't found, prefer a 256-entry chunk.
	if candidate.CNO != 0 {
		for _, c := range glcrChunks {
			data, err := ChunkData(r, c)
			if err != nil {
				continue
			}
			entries, err := readGLColors(data)
			if err != nil {
				continue
			}
			if len(entries) == 256 {
				candidate = c
				break
			}
		}
	}

	data, err := ChunkData(r, candidate)
	if err != nil {
		return Palette{}, false, fmt.Errorf("FindGLCR: reading chunk: %w", err)
	}
	entries, err := readGLColors(data)
	if err != nil {
		return Palette{}, false, fmt.Errorf("FindGLCR: parsing chunk: %w", err)
	}

	pal := GrayscalePalette()
	for i, e := range entries {
		if i >= len(pal.Colors) {
			break
		}
		pal.Colors[i] = e
	}
	return pal, true, nil
}

// LoadBackgroundScene reads a BKGD chunk and its children from cf, returning
// the decoded background with all camera angles. base is the 256-entry
// palette used as the starting point before the BKGD's own GLCR patch is applied.
//
// r must be the io.ReaderAt for the chunky file that cf was parsed from.
// bkgdCTG/bkgdCNO identify the BKGD chunk to load.
func LoadBackgroundScene(r io.ReaderAt, cf *ChunkyFile, bkgdCTG, bkgdCNO uint32, base Palette) (*BackgroundScene, error) {
	bkgdChunk, ok := cf.FindChunk(bkgdCTG, bkgdCNO)
	if !ok {
		return nil, fmt.Errorf("bkgd: chunk %s/0x%08X not found", CTGToString(bkgdCTG), bkgdCNO)
	}

	// Read and parse the BKGDF on-disk header.
	// Layout (8 bytes, always LE in practice):
	//   [0:2] int16  bo         — byte-order marker (0x0001 = LE)
	//   [2:4] int16  osk        — OS kind (ignored)
	//   [4]   byte   bIndexBase — first palette slot for the GLCR patch
	//   [5]   byte   bPad
	//   [6:8] int16  swPad
	raw, err := ChunkData(r, bkgdChunk)
	if err != nil {
		return nil, fmt.Errorf("bkgd: reading BKGD chunk: %w", err)
	}
	if len(raw) < 8 {
		return nil, fmt.Errorf("bkgd: BKGDF too short (%d bytes, need 8)", len(raw))
	}
	bo := int16(binary.LittleEndian.Uint16(raw[0:2]))
	if bo != 0x0001 {
		return nil, fmt.Errorf("bkgd: unsupported byte order 0x%04X (only LE files supported)", uint16(bo))
	}
	indexBase := int(raw[4])

	// Load the GLCR child (custom palette), if present.
	scenePalette := base
	if glcrChunk, ok := cf.FindChildByChidCTG(bkgdChunk, 0, TagGLCR); ok {
		glcrData, err := ChunkData(r, glcrChunk)
		if err != nil {
			return nil, fmt.Errorf("bkgd: reading GLCR: %w", err)
		}
		entries, err := readGLColors(glcrData)
		if err != nil {
			return nil, fmt.Errorf("bkgd: parsing GLCR: %w", err)
		}
		scenePalette = patchPalette(base, entries, indexBase)
	}

	// Load each CAM child (chid 0, 1, 2, ...) and its MBMP grandchild.
	var angles []CameraAngle
	for chid := uint32(0); ; chid++ {
		camChunk, ok := cf.FindChildByChidCTG(bkgdChunk, chid, TagCAM)
		if !ok {
			break
		}
		mbmpChunk, ok := cf.FindChildByChidCTG(camChunk, 0, TagMBMP)
		if !ok {
			return nil, fmt.Errorf("bkgd: CAM %d has no MBMP child", chid)
		}
		mbmpData, err := ChunkData(r, mbmpChunk)
		if err != nil {
			return nil, fmt.Errorf("bkgd: reading MBMP for CAM %d: %w", chid, err)
		}
		img, err := ReadMBMP(bytes.NewReader(mbmpData))
		if err != nil {
			return nil, fmt.Errorf("bkgd: decoding MBMP for CAM %d: %w", chid, err)
		}

		// Load the optional ZBMP z-buffer child (same CHID 0, different CTG).
		var zbuf *ZBMPImage
		if zbmpChunk, ok := cf.FindChildByChidCTG(camChunk, 0, TagZBMP); ok {
			zbmpData, err := ChunkData(r, zbmpChunk)
			if err != nil {
				return nil, fmt.Errorf("bkgd: reading ZBMP for CAM %d: %w", chid, err)
			}
			zbuf, err = ReadZBMP(bytes.NewReader(zbmpData))
			if err != nil {
				return nil, fmt.Errorf("bkgd: decoding ZBMP for CAM %d: %w", chid, err)
			}
		}

		angles = append(angles, CameraAngle{Index: int(chid), Img: img, ZBuf: zbuf})
	}

	return &BackgroundScene{
		Palette:   scenePalette,
		IndexBase: indexBase,
		Angles:    angles,
	}, nil
}

// readGLColors parses a GLCR chunk's raw bytes — a GL (growable list) of CLR
// entries — and returns them as RGBA colors.
//
// GL on-disk header (12 bytes):
//
//	[0:2]  int16  bo      — byte-order marker
//	[2:4]  int16  osk     — OS kind (ignored)
//	[4:8]  int32  cbEntry — bytes per entry (must be 4 for CLR)
//	[8:12] int32  ivMac   — number of entries
//
// Each CLR entry (4 bytes): bBlue, bGreen, bRed, bZero (Windows RGBQUAD order).
func readGLColors(data []byte) ([]color.RGBA, error) {
	const glHeaderSize = 12
	if len(data) < glHeaderSize {
		return nil, fmt.Errorf("GLCR: data too short (%d bytes, need at least %d)", len(data), glHeaderSize)
	}

	bo := int16(binary.LittleEndian.Uint16(data[0:2]))
	var cbEntry, ivMac int32
	switch bo {
	case 0x0001: // LE
		cbEntry = int32(binary.LittleEndian.Uint32(data[4:8]))
		ivMac = int32(binary.LittleEndian.Uint32(data[8:12]))
	case 0x0100: // BE
		cbEntry = int32(binary.BigEndian.Uint32(data[4:8]))
		ivMac = int32(binary.BigEndian.Uint32(data[8:12]))
	default:
		return nil, fmt.Errorf("GLCR: unknown byte order 0x%04X", uint16(bo))
	}

	if cbEntry != 4 {
		return nil, fmt.Errorf("GLCR: expected cbEntry=4 (CLR size), got %d", cbEntry)
	}
	if ivMac < 0 {
		return nil, fmt.Errorf("GLCR: negative entry count %d", ivMac)
	}
	need := glHeaderSize + int(ivMac)*4
	if len(data) != need {
		return nil, fmt.Errorf("GLCR: data length %d, expected %d (%d entries)", len(data), need, ivMac)
	}

	entries := make([]color.RGBA, ivMac)
	for i := range entries {
		off := glHeaderSize + i*4
		// CLR on disk is Windows RGBQUAD: Blue, Green, Red, Reserved.
		entries[i] = color.RGBA{B: data[off], G: data[off+1], R: data[off+2], A: 0xFF}
	}
	return entries, nil
}

// patchPalette returns a copy of base with entries[i] written into
// Colors[indexBase+i] for each i. Entries that would exceed 256 are dropped.
func patchPalette(base Palette, entries []color.RGBA, indexBase int) Palette {
	colors := make([]color.Color, len(base.Colors))
	copy(colors, base.Colors)
	for i, e := range entries {
		idx := indexBase + i
		if idx >= len(colors) {
			break
		}
		colors[idx] = e
	}
	return Palette{Colors: colors}
}
