// Package main implements a chunky file parser.
//
// Chunky files (.chk) are the binary data format used by Kauai and 3D Movie
// Maker. Each file contains a heap of typed data "chunks" identified by a
// (CTG, CNO) pair, plus an index implemented as a General Group (GG) stored
// at the end of the file.
//
// On-disk layout:
//
//	[CFP header: 128 bytes]
//	[chunk data heap: arbitrary order]
//	[GG index: cbIndex bytes at fpIndex]
//	[free map: cbMap bytes at fpMap]
//
// GG index layout:
//
//	[GGF header: 20 bytes]
//	[variable data blob: bvMac bytes]  ← fixed+variable parts of each CRP
//	[LOC array: ilocMac×8 bytes]       ← (bv, cb) offsets into variable data
//
// Each CRP (Chunk Representation) fixed part is either:
//   - CRPSM (20 bytes): default small index (cb packed into high 24 bits)
//   - CRPBG (32 bytes): big index (separate cb field, rti field)
//
// The variable part of each CRP entry contains:
//   - ckid × 12 bytes: KID child-chunk references
//   - remaining bytes: STN chunk name (may be empty)

package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"unicode/utf16"
)

// Chunky file format constants.
const (
	// magicLE is what binary.LittleEndian.Uint32 returns when reading the
	// 4 bytes "CHN2" from disk. BigLittle('CHN2','2NHC') == '2NHC' on x86,
	// and '2NHC' stored as little-endian == bytes [C,H,N,2] on disk.
	magicLE = uint32(0x324E4843)

	kboCur = int16(0x0001)

	// kcvnMinGrfcrp is the first chunky file version that stores grfcrp as a
	// bitmask. Older files store it as four individual flag bytes.
	kcvnMinGrfcrp = int16(4)

	// kcbitGrfcrp is the bit-shift used in the CRPSM luGrfcrpCb field.
	// High 24 bits = cb, low 8 bits = grfcrp flags.
	kcbitGrfcrp = 8

	bvNilValue = int32(-1) // LOC.Bv value indicating a free (deleted) slot

	// Chunk flags (grfcrp bits).
	FcrpOnExtra = uint32(0x01) // data lives on a companion file, not the main .chk
	FcrpLoner   = uint32(0x02) // chunk can exist without a parent
	FcrpPacked  = uint32(0x04) // data is compressed with the Kauai codec
	FcrpForest  = uint32(0x10) // data is an embedded chunk forest (nested chunky data)

	crpsmFixedSize = 20 // sizeof(CRPSM)
	crpbgFixedSize = 32 // sizeof(CRPBG)
)

// KID is a child-chunk reference stored in each CRP variable-data block.
// On disk: CTG uint32 LE, CNO uint32 LE, CHID uint32 LE (12 bytes total).
type KID struct {
	CTG  uint32 // child chunk type tag
	CNO  uint32 // child chunk number
	CHID uint32 // child ID (used to distinguish multiple same-type children)
}

// ChunkyFile is the result of parsing a chunky file header and index.
type ChunkyFile struct {
	Creator   uint32 // CTG of the program that wrote this file
	VerCur    int16  // current format version
	VerBack   int16  // oldest version that can read this file
	CRPFormat int32  // fixed-part size: crpsmFixedSize (20) or crpbgFixedSize (32)
	Chunks    []Chunk
}

// Chunk is a single entry from the chunky file index.
type Chunk struct {
	CTG    uint32 // 4-char type tag (e.g. 0x4D564945 == 'MVIE')
	CNO    uint32 // chunk number (unique within a CTG)
	Offset int32  // byte offset of raw chunk data in the .chk file
	Size   int32  // byte size of raw chunk data (may be compressed)
	Flags  uint32 // fcrp* bitmask flags
	CKid   int    // number of child-chunk references
	Kids   []KID  // child-chunk references, length == CKid
	Name   string // optional chunk name (STN, may be empty)
	Order  int    // 0-based position in the original GG LOC array (non-deleted slots only)
}

// IsPacked reports whether the chunk data is compressed.
func (c Chunk) IsPacked() bool { return c.Flags&FcrpPacked != 0 }

// IsOnExtra reports whether the data lives on a companion file (not in the .chk
// itself). Chunks with this flag cannot be extracted from the main file alone.
func (c Chunk) IsOnExtra() bool { return c.Flags&FcrpOnExtra != 0 }

// IsForest reports whether the chunk data contains an embedded chunk forest.
func (c Chunk) IsForest() bool { return c.Flags&FcrpForest != 0 }

// -------------------------------------------------------------------
// On-disk structs — all fields little-endian, no implicit padding.
// -------------------------------------------------------------------

// cfpDisk is the Chunky File Prefix at offset 0 (128 bytes total).
type cfpDisk struct {
	Magic     uint32    // "CHN2" as LE uint32
	Creator   uint32    // CTG of creating application
	VerCur    int16     // DVER._swCur
	VerBack   int16     // DVER._swBack
	ByteOrder int16     // kboCur (0x0001) or kboOther (0x0100)
	OSKind    int16     // OS identifier
	FpMac     int32     // logical end-of-file
	FpIndex   int32     // file offset of GG index
	CbIndex   int32     // byte size of GG index
	FpMap     int32     // file offset of free-space map
	CbMap     int32     // byte size of free-space map (0 = none)
	Reserved  [23]int32 // reserved, should be zero
} // 4+4+2+2+2+2+4+4+4+4+4+92 = 128 bytes

// ggfDisk is the General Group on-file header (20 bytes).
type ggfDisk struct {
	ByteOrder int16 // kboCur or kboOther
	OSKind    int16
	IlocMac   int32 // number of entries (including free slots)
	BvMac     int32 // total byte size of variable-data blob
	ClocFree  int32 // free-list head (or -1)
	CbFixed   int32 // fixed-part size per entry (crpsmFixedSize or crpbgFixedSize)
} // 2+2+4+4+4+4 = 20 bytes

// locDisk is one entry in the LOC array (8 bytes).
type locDisk struct {
	Bv int32 // byte offset into variable-data blob (-1 = free/deleted slot)
	Cb int32 // total entry size (cbFixed + variable size)
}

// crpsmDisk is a small Chunk Representation (20 bytes, default for 3DMMForever).
type crpsmDisk struct {
	CTG        uint32 // chunk type tag
	CNO        uint32 // chunk number
	FP         int32  // file offset of chunk data
	LuGrfcrpCb uint32 // high 24 bits = cb (data size), low 8 bits = grfcrp flags
	CKid       uint16 // number of child-chunk references
	CCrpRef    uint16 // number of parent-chunk references
}

// crpbgDisk is a big Chunk Representation (32 bytes), used by original 3DMM.
type crpbgDisk struct {
	CTG     uint32 // chunk type tag
	CNO     uint32 // chunk number
	FP      int32  // file offset of chunk data
	Cb      int32  // data size in bytes
	CKid    int32  // number of child-chunk references
	CCrpRef int32  // number of parent-chunk references
	RTI     int32  // run-time identifier (not meaningful on disk)
	Grfcrp  uint32 // grfcrp bitmask (v≥4) or four flag bytes (v<4)
}

// -------------------------------------------------------------------
// Public API
// -------------------------------------------------------------------

// ParseChunkyFile reads the header and index from a chunky file and returns
// the list of chunks. Chunk data is not read; use Chunk.Offset and Chunk.Size
// to locate it in the original reader.
func ParseChunkyFile(rs io.ReadSeeker) (*ChunkyFile, error) {
	var hdr cfpDisk
	if err := binary.Read(rs, binary.LittleEndian, &hdr); err != nil {
		return nil, fmt.Errorf("reading file header: %w", err)
	}
	if hdr.Magic != magicLE {
		return nil, fmt.Errorf("not a chunky file: bad magic 0x%08X (expected 0x%08X for \"CHN2\")",
			hdr.Magic, magicLE)
	}
	if hdr.ByteOrder != kboCur {
		return nil, fmt.Errorf("unsupported byte order 0x%04X: only little-endian files are supported",
			uint16(hdr.ByteOrder))
	}
	if hdr.FpIndex <= 0 || hdr.CbIndex <= 0 {
		return nil, fmt.Errorf("invalid index location: fpIndex=%d cbIndex=%d", hdr.FpIndex, hdr.CbIndex)
	}

	if _, err := rs.Seek(int64(hdr.FpIndex), io.SeekStart); err != nil {
		return nil, fmt.Errorf("seeking to index at 0x%X: %w", hdr.FpIndex, err)
	}
	indexData := make([]byte, hdr.CbIndex)
	if _, err := io.ReadFull(rs, indexData); err != nil {
		return nil, fmt.Errorf("reading index (%d bytes at 0x%X): %w", hdr.CbIndex, hdr.FpIndex, err)
	}

	chunks, cbFixed, err := parseGGIndex(indexData, hdr.VerCur)
	if err != nil {
		return nil, fmt.Errorf("parsing index: %w", err)
	}
	return &ChunkyFile{
		Creator:   hdr.Creator,
		VerCur:    hdr.VerCur,
		VerBack:   hdr.VerBack,
		CRPFormat: cbFixed,
		Chunks:    chunks,
	}, nil
}

// -------------------------------------------------------------------
// Internal parsing
// -------------------------------------------------------------------

func parseGGIndex(data []byte, fileVer int16) ([]Chunk, int32, error) {
	const ggfSize = 20
	if len(data) < ggfSize {
		return nil, 0, fmt.Errorf("index too small (%d bytes, need at least %d)", len(data), ggfSize)
	}

	var hdr ggfDisk
	if err := binary.Read(bytes.NewReader(data[:ggfSize]), binary.LittleEndian, &hdr); err != nil {
		return nil, 0, fmt.Errorf("reading GGF header: %w", err)
	}
	if hdr.ByteOrder != kboCur {
		return nil, 0, fmt.Errorf("index has unsupported byte order 0x%04X", uint16(hdr.ByteOrder))
	}
	if hdr.IlocMac < 0 || hdr.BvMac < 0 || hdr.CbFixed <= 0 {
		return nil, 0, fmt.Errorf("malformed GGF: ilocMac=%d bvMac=%d cbFixed=%d",
			hdr.IlocMac, hdr.BvMac, hdr.CbFixed)
	}

	// Verify we have enough bytes for variable data + LOC array.
	locArraySize := int(hdr.IlocMac) * 8
	need := ggfSize + int(hdr.BvMac) + locArraySize
	if len(data) < need {
		return nil, 0, fmt.Errorf("index too short: have %d bytes, need %d (ggf=%d vardata=%d locs=%d)",
			len(data), need, ggfSize, hdr.BvMac, locArraySize)
	}

	varData := data[ggfSize : ggfSize+int(hdr.BvMac)]
	locsStart := ggfSize + int(hdr.BvMac)
	oldIndex := fileVer > 0 && fileVer < kcvnMinGrfcrp

	var chunks []Chunk
	order := 0
	for i := range int(hdr.IlocMac) {
		off := locsStart + i*8
		var l locDisk
		if err := binary.Read(bytes.NewReader(data[off:off+8]), binary.LittleEndian, &l); err != nil {
			return nil, 0, fmt.Errorf("reading LOC[%d]: %w", i, err)
		}
		if l.Bv == bvNilValue {
			continue // free/deleted slot
		}
		bv, cb := int(l.Bv), int(l.Cb)
		if bv < 0 || bv+cb > len(varData) {
			return nil, 0, fmt.Errorf("LOC[%d] out of bounds: bv=%d cb=%d (vardata len=%d)",
				i, l.Bv, l.Cb, len(varData))
		}
		c, err := parseCRP(varData[bv:bv+cb], hdr.CbFixed, oldIndex)
		if err != nil {
			return nil, 0, fmt.Errorf("parsing CRP[%d]: %w", i, err)
		}
		c.Order = order
		order++
		chunks = append(chunks, c)
	}
	return chunks, hdr.CbFixed, nil
}

// parseCRP interprets an entry from the variable-data blob as either a CRPSM
// or CRPBG depending on cbFixed.
func parseCRP(data []byte, cbFixed int32, oldIndex bool) (Chunk, error) {
	switch cbFixed {
	case crpsmFixedSize:
		if len(data) < crpsmFixedSize {
			return Chunk{}, fmt.Errorf("CRPSM entry too short (%d bytes, need %d)", len(data), crpsmFixedSize)
		}
		var crp crpsmDisk
		if err := binary.Read(bytes.NewReader(data[:crpsmFixedSize]), binary.LittleEndian, &crp); err != nil {
			return Chunk{}, fmt.Errorf("reading CRPSM: %w", err)
		}
		ckid := int(crp.CKid)
		kids, err := parseKIDs(data[crpsmFixedSize:], ckid)
		if err != nil {
			return Chunk{}, fmt.Errorf("parsing CRPSM kids: %w", err)
		}
		stnOff := crpsmFixedSize + ckid*12
		return Chunk{
			CTG:    crp.CTG,
			CNO:    crp.CNO,
			Offset: crp.FP,
			Size:   int32(crp.LuGrfcrpCb >> kcbitGrfcrp),
			Flags:  crp.LuGrfcrpCb & 0xFF,
			CKid:   ckid,
			Kids:   kids,
			Name:   parseSTN(data[stnOff:]),
		}, nil

	case crpbgFixedSize:
		if len(data) < crpbgFixedSize {
			return Chunk{}, fmt.Errorf("CRPBG entry too short (%d bytes, need %d)", len(data), crpbgFixedSize)
		}
		var crp crpbgDisk
		if err := binary.Read(bytes.NewReader(data[:crpbgFixedSize]), binary.LittleEndian, &crp); err != nil {
			return Chunk{}, fmt.Errorf("reading CRPBG: %w", err)
		}
		grfcrp := crp.Grfcrp
		if oldIndex {
			// Pre-v4 files encode flags as 4 individual bytes in the union
			// rather than a packed bitmask. LE layout: [fOnExtra, fLoner, fPacked, bT].
			grfcrp = 0
			if (crp.Grfcrp>>8)&0xFF != 0 { // byte[1] = fLoner
				grfcrp |= FcrpLoner
			}
			if (crp.Grfcrp>>16)&0xFF != 0 { // byte[2] = fPacked
				grfcrp |= FcrpPacked
			}
		}
		ckid := int(crp.CKid)
		kids, err := parseKIDs(data[crpbgFixedSize:], ckid)
		if err != nil {
			return Chunk{}, fmt.Errorf("parsing CRPBG kids: %w", err)
		}
		stnOff := crpbgFixedSize + ckid*12
		return Chunk{
			CTG:    crp.CTG,
			CNO:    crp.CNO,
			Offset: crp.FP,
			Size:   crp.Cb,
			Flags:  grfcrp,
			CKid:   ckid,
			Kids:   kids,
			Name:   parseSTN(data[stnOff:]),
		}, nil

	default:
		return Chunk{}, fmt.Errorf("unknown CRP fixed size %d (expected %d for CRPSM or %d for CRPBG)",
			cbFixed, crpsmFixedSize, crpbgFixedSize)
	}
}

// parseSTN reads a Kauai STN string from the start of data.
//
// On-disk layout (written by STN::GetData / STN::FRead):
//
//	[osk: 2 bytes LE]  OS/encoding kind (0x0303 = koskSbWin, single-byte Windows)
//	[cch: 1 byte]      string length
//	[chars: cch bytes] string content
//	[NUL: 1 byte]      null terminator
//
// Returns "" if data is too short, the osk is unrecognised, or cch is zero.
func parseSTN(data []byte) string {
	const (
		koskSbWin  = 0x0303 // single-byte Windows (used by 3DMMForever files)
		koskSbMac  = 0x0101
		koskUniWin = 0x0505
		koskUniMac = 0x0404
	)
	if len(data) < 4 { // need at least osk(2) + cch(1) + NUL(1)
		return ""
	}
	osk := binary.LittleEndian.Uint16(data[:2])
	switch osk {
	case koskSbWin, koskSbMac:
		n := int(data[2])
		if n == 0 || len(data) < 3+n {
			return ""
		}
		return string(data[3 : 3+n])
	case koskUniWin, koskUniMac:
		// 2-byte LE length, followed by n UTF-16 code units + null
		if len(data) < 6 {
			return ""
		}
		n := int(binary.LittleEndian.Uint16(data[2:4]))
		if n == 0 || len(data) < 4+n*2 {
			return ""
		}
		u16 := make([]uint16, n)
		for i := range u16 {
			u16[i] = binary.LittleEndian.Uint16(data[4+i*2 : 6+i*2])
		}
		return string(utf16.Decode(u16))
	default:
		return ""
	}
}

// parseKIDs reads ckid KID records from the start of varData.
// Each KID is 12 bytes: CTG uint32 LE, CNO uint32 LE, CHID uint32 LE.
func parseKIDs(varData []byte, ckid int) ([]KID, error) {
	if ckid == 0 {
		return nil, nil
	}
	need := ckid * 12
	if len(varData) < need {
		return nil, fmt.Errorf("variable data too short for %d KIDs: have %d bytes, need %d", ckid, len(varData), need)
	}
	kids := make([]KID, ckid)
	for i := range kids {
		off := i * 12
		kids[i] = KID{
			CTG:  binary.LittleEndian.Uint32(varData[off : off+4]),
			CNO:  binary.LittleEndian.Uint32(varData[off+4 : off+8]),
			CHID: binary.LittleEndian.Uint32(varData[off+8 : off+12]),
		}
	}
	return kids, nil
}

// FindChunk returns the first chunk with the given CTG and CNO, or false.
func (cf *ChunkyFile) FindChunk(ctg, cno uint32) (Chunk, bool) {
	for _, c := range cf.Chunks {
		if c.CTG == ctg && c.CNO == cno {
			return c, true
		}
	}
	return Chunk{}, false
}

// FindChildByChidCTG finds a child of parent with the given CHID and child CTG,
// then resolves and returns the full Chunk record from cf.Chunks.
func (cf *ChunkyFile) FindChildByChidCTG(parent Chunk, chid, ctg uint32) (Chunk, bool) {
	for _, kid := range parent.Kids {
		if kid.CHID == chid && kid.CTG == ctg {
			return cf.FindChunk(kid.CTG, kid.CNO)
		}
	}
	return Chunk{}, false
}

// ChunkData reads and decompresses (if packed) the raw bytes of a chunk.
func ChunkData(r io.ReaderAt, c Chunk) ([]byte, error) {
	if c.Size <= 0 {
		return nil, nil
	}
	raw := make([]byte, c.Size)
	if _, err := r.ReadAt(raw, int64(c.Offset)); err != nil {
		return nil, fmt.Errorf("reading chunk %s/0x%08X at 0x%X: %w", ctgToString(c.CTG), c.CNO, c.Offset, err)
	}
	if c.IsPacked() {
		data, err := DecodeKauaiChunk(raw)
		if err != nil {
			return nil, fmt.Errorf("decompressing chunk %s/0x%08X: %w", ctgToString(c.CTG), c.CNO, err)
		}
		return data, nil
	}
	return raw, nil
}

// ctgToString converts a CTG uint32 (read as little-endian from disk) to its
// human-readable 4-char form. Non-printable bytes are replaced with '.'.
//
// Example: 0x4D564945 (LE read of "MVIE" bytes) → "MVIE"
func ctgToString(ctg uint32) string {
	b := [4]byte{byte(ctg >> 24), byte(ctg >> 16), byte(ctg >> 8), byte(ctg)}
	for i, c := range b {
		if c < 0x20 || c == 0x7F {
			b[i] = '.'
		}
	}
	return string(b[:])
}
