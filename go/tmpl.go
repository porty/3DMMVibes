package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// Chunk type tags for template/action chunks.
const (
	ctgTMPL = uint32('T')<<24 | uint32('M')<<16 | uint32('P')<<8 | uint32('L')
	ctgACTN = uint32('A')<<24 | uint32('C')<<16 | uint32('T')<<8 | uint32('N')
	ctgBMDL = uint32('B')<<24 | uint32('M')<<16 | uint32('D')<<8 | uint32('L')
	ctgGGCL = uint32('G')<<24 | uint32('G')<<16 | uint32('C')<<8 | uint32('L')
	ctgGLXF = uint32('G')<<24 | uint32('L')<<16 | uint32('X')<<8 | uint32('F')
	ctgGLBS = uint32('G')<<24 | uint32('L')<<16 | uint32('B')<<8 | uint32('S')
)

// BMAT34 is a BRender 4×3 matrix (row-vector convention).
// Row 3 is the translation; rows 0–2 are the local axes.
type BMAT34 [4][3]float64

// CPS is one body-part entry in a cel's variable data.
type CPS struct {
	ChidModl int // CHID of the BMDL child of TMPL to use for this part
	IMat34   int // index into the GLXF transform list
}

// Cel is one animation frame from an ACTN's GGCL.
type Cel struct {
	Parts []CPS // one CPS per body part
}

// ActionData holds the parsed contents of one ACTN chunk.
type ActionData struct {
	Cels       []Cel
	Transforms []BMAT34 // indexed by CPS.IMat34
}

// LoadedTemplate is the result of loading a TMPL chunk and all its children.
type LoadedTemplate struct {
	CNO     uint32
	GrfTmpl uint32
	// Actions is indexed by the CHID of the ACTN child chunk (the action index).
	Actions map[uint32]*ActionData
	// Models maps CHID-of-BMDL to parsed model data.
	Models map[uint32]*BRModel
	// IBSets maps body-part index (0-based) to body-part-set index (ibset).
	IBSets []int
	// NumBodyParts is derived from GLBS length.
	NumBodyParts int
}

// LoadTemplate reads a TMPL chunk (by CNO) from cf and parses its full subtree.
func LoadTemplate(cf *ChunkyFile, r io.ReaderAt, cno uint32) (*LoadedTemplate, error) {
	tmplChunk, ok := cf.FindChunk(ctgTMPL, cno)
	if !ok {
		return nil, fmt.Errorf("TMPL/0x%08X not found", cno)
	}

	tmplData, err := ChunkData(r, tmplChunk)
	if err != nil {
		return nil, fmt.Errorf("reading TMPL/0x%08X: %w", cno, err)
	}
	if len(tmplData) < 16 {
		return nil, fmt.Errorf("TMPL/0x%08X: header too short (%d bytes)", cno, len(tmplData))
	}
	grfTmpl := binary.LittleEndian.Uint32(tmplData[12:16])

	lt := &LoadedTemplate{
		CNO:     cno,
		GrfTmpl: grfTmpl,
		Actions: make(map[uint32]*ActionData),
		Models:  make(map[uint32]*BRModel),
	}

	// Load GLBS for body-part-set mapping.
	for _, kid := range tmplChunk.Kids {
		if kid.CTG != ctgGLBS {
			continue
		}
		glbsChunk, ok := cf.FindChunk(kid.CTG, kid.CNO)
		if !ok {
			continue
		}
		data, err := ChunkData(r, glbsChunk)
		if err != nil {
			return nil, fmt.Errorf("reading GLBS: %w", err)
		}
		ibsets, err := parseGLInt16(data)
		if err != nil {
			return nil, fmt.Errorf("parsing GLBS: %w", err)
		}
		lt.IBSets = ibsets
		lt.NumBodyParts = len(ibsets)
		break
	}

	// Load BMDL children.
	for _, kid := range tmplChunk.Kids {
		if kid.CTG != ctgBMDL {
			continue
		}
		bmdlChunk, ok := cf.FindChunk(kid.CTG, kid.CNO)
		if !ok {
			continue
		}
		data, err := ChunkData(r, bmdlChunk)
		if err != nil {
			return nil, fmt.Errorf("reading BMDL/0x%08X: %w", kid.CNO, err)
		}
		model, err := ParseBMDL(data)
		if err != nil {
			// Non-fatal: skip degenerate models.
			continue
		}
		lt.Models[kid.CHID] = model
	}

	// Collect and sort ACTN children by CHID so Actions are in anid order.
	type actnKid struct {
		chid uint32
		cno  uint32
	}
	var actnKids []actnKid
	for _, kid := range tmplChunk.Kids {
		if kid.CTG == ctgACTN {
			actnKids = append(actnKids, actnKid{kid.CHID, kid.CNO})
		}
	}
	sort.Slice(actnKids, func(i, j int) bool { return actnKids[i].chid < actnKids[j].chid })

	for _, ak := range actnKids {
		actnChunk, ok := cf.FindChunk(ctgACTN, ak.cno)
		if !ok {
			continue
		}
		ad, err := loadAction(cf, r, actnChunk, lt.NumBodyParts)
		if err != nil {
			// Non-fatal: skip broken actions.
			continue
		}
		lt.Actions[ak.chid] = ad
	}

	return lt, nil
}

// loadAction parses one ACTN chunk and its GGCL and GLXF children.
func loadAction(cf *ChunkyFile, r io.ReaderAt, actnChunk Chunk, numParts int) (*ActionData, error) {
	var ggclChunk, glxfChunk Chunk
	var hasGGCL, hasGLXF bool
	for _, kid := range actnChunk.Kids {
		switch kid.CTG {
		case ctgGGCL:
			ggclChunk, hasGGCL = cf.FindChunk(kid.CTG, kid.CNO)
		case ctgGLXF:
			glxfChunk, hasGLXF = cf.FindChunk(kid.CTG, kid.CNO)
		}
	}
	if !hasGGCL {
		return nil, fmt.Errorf("ACTN/0x%08X: no GGCL child", actnChunk.CNO)
	}

	ggclData, err := ChunkData(r, ggclChunk)
	if err != nil {
		return nil, fmt.Errorf("reading GGCL: %w", err)
	}
	cels, err := parseGGCL(ggclData, numParts)
	if err != nil {
		return nil, fmt.Errorf("parsing GGCL: %w", err)
	}

	var transforms []BMAT34
	if hasGLXF {
		glxfData, err := ChunkData(r, glxfChunk)
		if err != nil {
			return nil, fmt.Errorf("reading GLXF: %w", err)
		}
		transforms, err = parseGLXF(glxfData)
		if err != nil {
			return nil, fmt.Errorf("parsing GLXF: %w", err)
		}
	}

	return &ActionData{Cels: cels, Transforms: transforms}, nil
}

// parseGGCL parses a GGCL blob into Cel records.
// Fixed part per entry: 8 bytes (CEL: chidSnd uint32 + dwr int32).
// Variable part per entry: numParts × 4 bytes (CPS: chidModl int16 + imat34 int16).
func parseGGCL(data []byte, numParts int) ([]Cel, error) {
	_, entries, err := ParseGG(data)
	if err != nil {
		return nil, err
	}

	cels := make([]Cel, len(entries))
	for i, e := range entries {
		// Fixed part (8 bytes): [0:4] chidSnd, [4:8] dwr — both ignored for rendering.
		// Variable part: numParts × CPS (4 bytes each).
		varData := e.Var
		parts := make([]CPS, numParts)
		for p := range numParts {
			off := p * 4
			if off+4 > len(varData) {
				break
			}
			chidModl := int(int16(binary.LittleEndian.Uint16(varData[off : off+2])))
			imat34 := int(int16(binary.LittleEndian.Uint16(varData[off+2 : off+4])))
			parts[p] = CPS{ChidModl: chidModl, IMat34: imat34}
		}
		cels[i] = Cel{Parts: parts}
	}
	return cels, nil
}

// parseGLXF parses a GLXF blob (GL of BMAT34, each 48 bytes) into a slice of matrices.
// GL header: bo(2) osk(2) cbEntry(4) ivMac(4) — 12 bytes total.
func parseGLXF(data []byte) ([]BMAT34, error) {
	const glHdrSize = 12
	if len(data) < glHdrSize {
		return nil, fmt.Errorf("GLXF: too short (%d bytes)", len(data))
	}
	// Skip bo(2), osk(2).
	cbEntry := int(binary.LittleEndian.Uint32(data[4:8]))
	ivMac := int(binary.LittleEndian.Uint32(data[8:12]))
	if cbEntry != 48 {
		return nil, fmt.Errorf("GLXF: unexpected cbEntry %d (expected 48)", cbEntry)
	}
	need := glHdrSize + ivMac*48
	if len(data) < need {
		return nil, fmt.Errorf("GLXF: data too short (%d bytes, need %d)", len(data), need)
	}

	mats := make([]BMAT34, ivMac)
	off := glHdrSize
	for i := range mats {
		// Each BMAT34 is 4 rows × 3 int32 BRS values (row-major, row-vector convention).
		for row := range 4 {
			for col := range 3 {
				v := int32(binary.LittleEndian.Uint32(data[off : off+4]))
				mats[i][row][col] = brsToFloat64(v)
				off += 4
			}
		}
	}
	return mats, nil
}

// parseGLInt16 parses a GL of int16 entries. Used for GLBS.
// GL header: bo(2) osk(2) cbEntry(4) ivMac(4) — 12 bytes.
func parseGLInt16(data []byte) ([]int, error) {
	const glHdrSize = 12
	if len(data) < glHdrSize {
		return nil, fmt.Errorf("GL: too short (%d bytes)", len(data))
	}
	cbEntry := int(binary.LittleEndian.Uint32(data[4:8]))
	ivMac := int(binary.LittleEndian.Uint32(data[8:12]))
	if cbEntry != 2 {
		return nil, fmt.Errorf("GL int16: unexpected cbEntry %d (expected 2)", cbEntry)
	}
	need := glHdrSize + ivMac*2
	if len(data) < need {
		return nil, fmt.Errorf("GL int16: data too short (%d bytes, need %d)", len(data), need)
	}
	out := make([]int, ivMac)
	off := glHdrSize
	for i := range out {
		out[i] = int(int16(binary.LittleEndian.Uint16(data[off : off+2])))
		off += 2
	}
	return out, nil
}

// applyBMAT34 transforms a Vec3 by a BMAT34 matrix (row-vector convention).
// v' = v · M:  v'[j] = sum_i(v[i]*M[i][j]) + M[3][j]
func applyBMAT34(v Vec3, m BMAT34) Vec3 {
	return Vec3{
		X: v.X*m[0][0] + v.Y*m[1][0] + v.Z*m[2][0] + m[3][0],
		Y: v.X*m[0][1] + v.Y*m[1][1] + v.Z*m[2][1] + m[3][1],
		Z: v.X*m[0][2] + v.Y*m[1][2] + v.Z*m[2][2] + m[3][2],
	}
}
