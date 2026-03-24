package mm

import (
	"encoding/binary"
	"fmt"
)

// BMDL / BRender model constants.
const (
	// brVertexSize is the on-disk size of a br_vertex struct (including padding).
	// Layout: p(12) + map(8) + index(1) + R(1) + G(1) + B(1) + r(2) + n(6) = 32 bytes
	brVertexSize = 32

	// brFaceSize is the on-disk size of a br_face struct (including 2-byte tail padding).
	// Layout: vertices(6) + edges(6) + material*(4) + smoothing(2) + flags(1) + pad(1) + n(6) + d(4) + pad2(2) = 32 bytes
	brFaceSize = 32

	// modlfHeaderSize is the size of the MODLF on-disk header.
	modlfHeaderSize = 48
)

// Vec3 is a 3D point or vector in world space.
type Vec3 struct{ X, Y, Z float64 }

// BRVertex is one vertex from a BRender model.
type BRVertex struct {
	Pos     Vec3
	R, G, B uint8
}

// BRFace is one triangle face, referencing vertex indices in BRModel.Verts.
type BRFace struct {
	V [3]int
}

// BRModel is the result of parsing a BMDL chunk.
type BRModel struct {
	Verts []BRVertex
	Faces []BRFace
}

// ParseBMDL parses the raw (decompressed) bytes of a BMDL chunk into a BRModel.
//
// On-disk layout:
//
//	MODLF header (48 bytes): bo(2) osk(2) cver(2) cfac(2) rRadius(4) brb(24) bvec3Pivot(12)
//	br_vertex array: cver × 32 bytes
//	br_face array:   cfac × 32 bytes
func ParseBMDL(data []byte) (*BRModel, error) {
	if len(data) < modlfHeaderSize {
		return nil, fmt.Errorf("BMDL: data too short (%d bytes, need at least %d)", len(data), modlfHeaderSize)
	}

	cver := int(binary.LittleEndian.Uint16(data[4:6]))
	cfac := int(binary.LittleEndian.Uint16(data[6:8]))

	need := modlfHeaderSize + cver*brVertexSize + cfac*brFaceSize
	if len(data) < need {
		return nil, fmt.Errorf("BMDL: data too short (%d bytes, need %d for %d verts + %d faces)",
			len(data), need, cver, cfac)
	}

	verts := make([]BRVertex, cver)
	off := modlfHeaderSize
	for i := range verts {
		// p: 3 × int32 BRS (16.16 fixed-point)
		x := int32(binary.LittleEndian.Uint32(data[off : off+4]))
		y := int32(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		z := int32(binary.LittleEndian.Uint32(data[off+8 : off+12]))
		// UV map: [12:20] — ignored
		// index, R, G, B at [20:24]
		r := data[off+21]
		g := data[off+22]
		b := data[off+23]
		verts[i] = BRVertex{
			Pos: Vec3{
				X: brsToFloat64(x),
				Y: brsToFloat64(y),
				Z: brsToFloat64(z),
			},
			R: r, G: g, B: b,
		}
		off += brVertexSize
	}

	faces := make([]BRFace, cfac)
	for i := range faces {
		v0 := int(binary.LittleEndian.Uint16(data[off : off+2]))
		v1 := int(binary.LittleEndian.Uint16(data[off+2 : off+4]))
		v2 := int(binary.LittleEndian.Uint16(data[off+4 : off+6]))
		faces[i] = BRFace{V: [3]int{v0, v1, v2}}
		off += brFaceSize
	}

	return &BRModel{Verts: verts, Faces: faces}, nil
}
