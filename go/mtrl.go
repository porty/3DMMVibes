package mm

import (
	"encoding/binary"
	"fmt"
)

// TexMap holds the decoded pixel data from a TMAP chunk.
// Pixels are 8-bit palette indices into the GLCR palette.
type TexMap struct {
	Width    int
	Height   int
	RowBytes int
	Pixels   []byte // [y*RowBytes + x] = palette index 0–255
}

// Material describes the surface appearance of one body part.
// Either HasTexture is true (Tex is populated) or the solid R/G/B color is used.
type Material struct {
	R, G, B    uint8
	HasTexture bool
	Tex        *TexMap
}

// LoadedCMTL holds the parsed contents of one CMTL chunk (a costume for one body-part-set).
// Parts[i] is the Material for the i-th body part within the ibset; a nil entry means
// no MTRL was found for that slot (renderer falls back to ibsetColors).
type LoadedCMTL struct {
	IBSet int
	Parts []*Material
}

// parseMTRLF decodes a 20-byte MTRLF payload into a Material.
// tmapData is the raw bytes of the TMAP child chunk, or nil for a solid-color material.
func parseMTRLF(data []byte, tmapData []byte) (*Material, error) {
	const mtrlSize = 20
	if len(data) < mtrlSize {
		return nil, fmt.Errorf("MTRLF: too short (%d bytes, need %d)", len(data), mtrlSize)
	}
	// bo at [0:2], osk at [2:4] — skip (always little-endian in practice)
	brc := binary.LittleEndian.Uint32(data[4:8])
	r := uint8(brc >> 16)
	g := uint8(brc >> 8)
	b := uint8(brc)

	if tmapData != nil {
		tex, err := parseTMAPF(tmapData)
		if err != nil {
			return nil, fmt.Errorf("TMAP: %w", err)
		}
		return &Material{R: r, G: g, B: b, HasTexture: true, Tex: tex}, nil
	}
	return &Material{R: r, G: g, B: b}, nil
}

// parseCMTLF decodes an 8-byte CMTLF payload and returns the ibset value.
func parseCMTLF(data []byte) (ibset int, err error) {
	const cmtlSize = 8
	if len(data) < cmtlSize {
		return 0, fmt.Errorf("CMTLF: too short (%d bytes, need %d)", len(data), cmtlSize)
	}
	// bo at [0:2], osk at [2:4] — skip
	ibset = int(int32(binary.LittleEndian.Uint32(data[4:8])))
	return ibset, nil
}

// parseTMAPF decodes a TMAP chunk (20-byte header + pixel data) into a TexMap.
func parseTMAPF(data []byte) (*TexMap, error) {
	const tmapHdrSize = 20
	if len(data) < tmapHdrSize {
		return nil, fmt.Errorf("TMAPF: too short (%d bytes, need %d)", len(data), tmapHdrSize)
	}
	// bo at [0:2], osk at [2:4] — skip
	cbRow := int(int16(binary.LittleEndian.Uint16(data[4:6])))
	dxp := int(int16(binary.LittleEndian.Uint16(data[12:14])))
	dyp := int(int16(binary.LittleEndian.Uint16(data[14:16])))

	if cbRow <= 0 || dxp <= 0 || dyp <= 0 {
		return nil, fmt.Errorf("TMAPF: invalid dimensions cbRow=%d dxp=%d dyp=%d", cbRow, dxp, dyp)
	}
	if cbRow < dxp {
		return nil, fmt.Errorf("TMAPF: cbRow %d < dxp %d", cbRow, dxp)
	}
	pixelLen := cbRow * dyp
	if len(data) < tmapHdrSize+pixelLen {
		return nil, fmt.Errorf("TMAPF: pixel data too short (%d bytes, need %d)", len(data)-tmapHdrSize, pixelLen)
	}
	pixels := make([]byte, pixelLen)
	copy(pixels, data[tmapHdrSize:tmapHdrSize+pixelLen])
	return &TexMap{Width: dxp, Height: dyp, RowBytes: cbRow, Pixels: pixels}, nil
}
