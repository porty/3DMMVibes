package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"sort"
)

// Chunk type tags for movie/scene chunks.
const (
	ctgMVIE = uint32('M')<<24 | uint32('V')<<16 | uint32('I')<<8 | uint32('E')
	ctgSCEN = uint32('S')<<24 | uint32('C')<<16 | uint32('E')<<8 | uint32('N')
	ctgGGFR = uint32('G')<<24 | uint32('G')<<16 | uint32('F')<<8 | uint32('R')
	ctgGGST = uint32('G')<<24 | uint32('G')<<16 | uint32('S')<<8 | uint32('T')
	ctgACTR = uint32('A')<<24 | uint32('C')<<16 | uint32('T')<<8 | uint32('R')
	ctgGGAE = uint32('G')<<24 | uint32('G')<<16 | uint32('A')<<8 | uint32('E')
	ctgPATH = uint32('P')<<24 | uint32('A')<<16 | uint32('T')<<8 | uint32('H')
)

// Scene event types (SEVT enum).
const (
	sevtAddActr    = int32(0)
	sevtPlaySnd    = int32(1)
	sevtAddTbox    = int32(2)
	sevtChngCamera = int32(3)
	sevtSetBkgd    = int32(4)
	sevtPause      = int32(5)
)

// GGEntry is one non-free entry parsed from a GG (General Group) blob.
type GGEntry struct {
	Fixed []byte // cbFixed bytes from the entry
	Var   []byte // variable bytes following the fixed part
}

// ParseGG parses a raw GG blob into its entries, skipping free (deleted) slots.
// It returns the fixed-part size for reference. The GG format is:
//
//	[GGF header: 20 bytes]
//	[variable-data blob: bvMac bytes]
//	[LOC array: ilocMac × 8 bytes]
func ParseGG(data []byte) (cbFixed int, entries []GGEntry, err error) {
	const hdrSize = 20
	if len(data) < hdrSize {
		return 0, nil, fmt.Errorf("GG: data too short (%d bytes, need %d)", len(data), hdrSize)
	}

	var hdr ggfDisk
	if err := binary.Read(bytes.NewReader(data[:hdrSize]), binary.LittleEndian, &hdr); err != nil {
		return 0, nil, fmt.Errorf("GG: reading header: %w", err)
	}
	if hdr.ByteOrder != kboCur {
		return 0, nil, fmt.Errorf("GG: unsupported byte order 0x%04X", uint16(hdr.ByteOrder))
	}
	if hdr.IlocMac < 0 || hdr.BvMac < 0 || hdr.CbFixed < 0 {
		return 0, nil, fmt.Errorf("GG: invalid header: ilocMac=%d bvMac=%d cbFixed=%d",
			hdr.IlocMac, hdr.BvMac, hdr.CbFixed)
	}

	locArraySize := int(hdr.IlocMac) * 8
	need := hdrSize + int(hdr.BvMac) + locArraySize
	if len(data) < need {
		return 0, nil, fmt.Errorf("GG: data too short: have %d bytes, need %d", len(data), need)
	}

	varBlob := data[hdrSize : hdrSize+int(hdr.BvMac)]
	locsStart := hdrSize + int(hdr.BvMac)
	cf := int(hdr.CbFixed)

	var out []GGEntry
	for i := range int(hdr.IlocMac) {
		off := locsStart + i*8
		var loc locDisk
		if err := binary.Read(bytes.NewReader(data[off:off+8]), binary.LittleEndian, &loc); err != nil {
			return 0, nil, fmt.Errorf("GG: reading LOC[%d]: %w", i, err)
		}
		if loc.Bv == bvNilValue {
			continue // free/deleted slot
		}
		bv, cb := int(loc.Bv), int(loc.Cb)
		if bv < 0 || bv+cb > len(varBlob) {
			return 0, nil, fmt.Errorf("GG: LOC[%d] out of bounds: bv=%d cb=%d (blob len=%d)",
				i, bv, cb, len(varBlob))
		}
		entry := varBlob[bv : bv+cb]
		fixedLen := cf
		if fixedLen > len(entry) {
			fixedLen = len(entry)
		}
		out = append(out, GGEntry{
			Fixed: entry[:fixedLen],
			Var:   entry[fixedLen:],
		})
	}
	return cf, out, nil
}

// SceneEvent is one SEV record from a GGST or GGFR chunk.
type SceneEvent struct {
	Nfrm    int32
	Sevt    int32
	VarData []byte
}

// ChunkTAG is the meaningful part of a TAG value: the chunk type and number.
// On disk a TAG is 16 bytes: sid(4), pcrf(4), ctg(4), cno(4).
type ChunkTAG struct {
	CTG uint32
	CNO uint32
}

// SceneData holds the parsed contents of one SCEN chunk.
type SceneData struct {
	NfrmFirst   int32
	NfrmLast    int32
	Trans       int32
	StartEvents []SceneEvent // from GGST — fire once at scene start
	FrameEvents []SceneEvent // from GGFR — sorted by Nfrm
	ActorCNOs   []uint32     // CNOs of all ACTR children of this SCEN
}

// ParseScene reads and parses one SCEN chunk along with its GGST and GGFR children.
func ParseScene(cf *ChunkyFile, r io.ReaderAt, scenChunk Chunk) (*SceneData, error) {
	raw, err := ChunkData(r, scenChunk)
	if err != nil {
		return nil, fmt.Errorf("scene 0x%08X: reading chunk: %w", scenChunk.CNO, err)
	}
	// SCENH: bo(2), osk(2), nfrmLast(4), nfrmFirst(4), trans(4) = 16 bytes
	if len(raw) < 16 {
		return nil, fmt.Errorf("scene 0x%08X: SCENH too short (%d bytes, need 16)", scenChunk.CNO, len(raw))
	}
	bo := int16(binary.LittleEndian.Uint16(raw[0:2]))
	if bo != kboCur {
		return nil, fmt.Errorf("scene 0x%08X: unsupported byte order 0x%04X", scenChunk.CNO, uint16(bo))
	}
	sd := &SceneData{
		NfrmLast:  int32(binary.LittleEndian.Uint32(raw[4:8])),
		NfrmFirst: int32(binary.LittleEndian.Uint32(raw[8:12])),
		Trans:     int32(binary.LittleEndian.Uint32(raw[12:16])),
	}

	// Parse GGST — scene-start events.
	if ggstChunk, ok := findChildByCTG(cf, scenChunk, ctgGGST); ok {
		ggstData, err := ChunkData(r, ggstChunk)
		if err != nil {
			return nil, fmt.Errorf("scene 0x%08X: reading GGST: %w", scenChunk.CNO, err)
		}
		sd.StartEvents, err = parseSceneGG(ggstData)
		if err != nil {
			return nil, fmt.Errorf("scene 0x%08X: parsing GGST: %w", scenChunk.CNO, err)
		}
	}

	// Parse GGFR — per-frame events.
	if ggfrChunk, ok := findChildByCTG(cf, scenChunk, ctgGGFR); ok {
		ggfrData, err := ChunkData(r, ggfrChunk)
		if err != nil {
			return nil, fmt.Errorf("scene 0x%08X: reading GGFR: %w", scenChunk.CNO, err)
		}
		sd.FrameEvents, err = parseSceneGG(ggfrData)
		if err != nil {
			return nil, fmt.Errorf("scene 0x%08X: parsing GGFR: %w", scenChunk.CNO, err)
		}
		sort.Slice(sd.FrameEvents, func(i, j int) bool {
			return sd.FrameEvents[i].Nfrm < sd.FrameEvents[j].Nfrm
		})
	}

	// Collect all ACTR children of this SCEN, in CHID order.
	type actrRef struct {
		chid uint32
		cno  uint32
	}
	var actrs []actrRef
	for _, kid := range scenChunk.Kids {
		if kid.CTG == ctgACTR {
			actrs = append(actrs, actrRef{kid.CHID, kid.CNO})
		}
	}
	sort.Slice(actrs, func(i, j int) bool { return actrs[i].chid < actrs[j].chid })
	for _, a := range actrs {
		sd.ActorCNOs = append(sd.ActorCNOs, a.cno)
	}

	return sd, nil
}

// parseSceneGG parses a GG whose fixed part is an SEV (8 bytes: nfrm + sevt).
func parseSceneGG(data []byte) ([]SceneEvent, error) {
	_, entries, err := ParseGG(data)
	if err != nil {
		return nil, err
	}
	out := make([]SceneEvent, 0, len(entries))
	for _, e := range entries {
		if len(e.Fixed) < 8 {
			continue
		}
		out = append(out, SceneEvent{
			Nfrm:    int32(binary.LittleEndian.Uint32(e.Fixed[0:4])),
			Sevt:    int32(binary.LittleEndian.Uint32(e.Fixed[4:8])),
			VarData: e.Var,
		})
	}
	return out, nil
}

// findChildByCTG returns the first child of parent whose CTG matches ctg.
func findChildByCTG(cf *ChunkyFile, parent Chunk, ctg uint32) (Chunk, bool) {
	for _, kid := range parent.Kids {
		if kid.CTG == ctg {
			return cf.FindChunk(kid.CTG, kid.CNO)
		}
	}
	return Chunk{}, false
}

// ParseSEVBkgdTag parses the variable data of a sevtSetBkgd event.
// TAG on disk: sid(4), pcrf(4), ctg(4), cno(4) — only ctg and cno are meaningful.
func ParseSEVBkgdTag(varData []byte) (ChunkTAG, error) {
	if len(varData) < 16 {
		return ChunkTAG{}, fmt.Errorf("sevtSetBkgd: varData too short (%d bytes, need 16)", len(varData))
	}
	return ChunkTAG{
		CTG: binary.LittleEndian.Uint32(varData[8:12]),
		CNO: binary.LittleEndian.Uint32(varData[12:16]),
	}, nil
}

// ParseSEVCamera parses the variable data of a sevtChngCamera event (4-byte icam).
func ParseSEVCamera(varData []byte) (int32, error) {
	if len(varData) < 4 {
		return 0, fmt.Errorf("sevtChngCamera: varData too short (%d bytes, need 4)", len(varData))
	}
	return int32(binary.LittleEndian.Uint32(varData[0:4])), nil
}
