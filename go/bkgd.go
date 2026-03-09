package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/color"
	"io"
)

// Chunk type tags for BKGD-related chunks.
const (
	ctgBKGD = uint32('B')<<24 | uint32('K')<<16 | uint32('G')<<8 | uint32('D')
	ctgCAM  = uint32('C')<<24 | uint32('A')<<16 | uint32('M')<<8 | uint32(' ')
	ctgMBMP = uint32('M')<<24 | uint32('B')<<16 | uint32('M')<<8 | uint32('P')
	ctgGLCR = uint32('G')<<24 | uint32('L')<<16 | uint32('C')<<8 | uint32('R')
)

// globalPalette is the application-wide 256-color base palette. Set this
// before calling LoadBackgroundScene if you have palette data.
var globalPalette Palette

// CameraAngle holds the decoded background image for one camera position.
type CameraAngle struct {
	Index int        // 0-based camera index (CHID of the CAM chunk)
	Img   *MBMPImage // palette-indexed background bitmap
}

// BackgroundScene is a decoded BKGD chunk with all camera angles.
type BackgroundScene struct {
	Palette   Palette       // effective palette (global patched with GLCR entries)
	IndexBase int           // first palette slot overridden by this background's GLCR
	Angles    []CameraAngle // one per CAM child, in CHID order
}

// LoadBackgroundScene reads a BKGD chunk and its children from cf, returning
// the decoded background with all camera angles.
//
// r must be the io.ReaderAt for the chunky file that cf was parsed from.
// bkgdCTG/bkgdCNO identify the BKGD chunk to load.
func LoadBackgroundScene(r io.ReaderAt, cf *ChunkyFile, bkgdCTG, bkgdCNO uint32) (*BackgroundScene, error) {
	bkgdChunk, ok := cf.FindChunk(bkgdCTG, bkgdCNO)
	if !ok {
		return nil, fmt.Errorf("bkgd: chunk %s/0x%08X not found", ctgToString(bkgdCTG), bkgdCNO)
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
	scenePalette := globalPalette
	if glcrChunk, ok := cf.FindChildByChidCTG(bkgdChunk, 0, ctgGLCR); ok {
		glcrData, err := ChunkData(r, glcrChunk)
		if err != nil {
			return nil, fmt.Errorf("bkgd: reading GLCR: %w", err)
		}
		entries, err := readGLColors(glcrData)
		if err != nil {
			return nil, fmt.Errorf("bkgd: parsing GLCR: %w", err)
		}
		scenePalette = patchPalette(globalPalette, entries, indexBase)
	}

	// Load each CAM child (chid 0, 1, 2, ...) and its MBMP grandchild.
	var angles []CameraAngle
	for chid := uint32(0); ; chid++ {
		camChunk, ok := cf.FindChildByChidCTG(bkgdChunk, chid, ctgCAM)
		if !ok {
			break
		}
		mbmpChunk, ok := cf.FindChildByChidCTG(camChunk, 0, ctgMBMP)
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
		angles = append(angles, CameraAngle{Index: int(chid), Img: img})
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
