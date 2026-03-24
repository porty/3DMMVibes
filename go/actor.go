package mm

import (
	"encoding/binary"
	"fmt"
	"io"
)

// Actor event types (AET enum from actor.h).
const (
	aetAdd    = int32(0)
	aetActn   = int32(1)
	aetCost   = int32(2)
	aetRotF   = int32(3)
	aetPull   = int32(4)
	aetSize   = int32(5)
	aetSnd    = int32(6)
	aetMove   = int32(7)
	aetFreeze = int32(8)
	aetTweak  = int32(9)
	aetStep   = int32(10)
	aetRem    = int32(11)
	aetRotH   = int32(12)
)

// brsToFloat64 converts a BRS (16.16 fixed-point int32) to float64.
func brsToFloat64(v int32) float64 { return float64(v) / 65536.0 }

// ActorDef holds the parsed ACTF header for one ACTR chunk.
type ActorDef struct {
	FullRouteOffset [3]float64 // dxyzFullRte: whole-route translation (X, Y, Z)
	ARID            int32      // unique actor ID
	NfrmFirst       int32      // first frame actor lives (may be knfrmInvalid)
	NfrmLast        int32      // last frame actor lives (may be knfrmInvalid)
	TagTmplCTG      uint32     // template chunk CTG
	TagTmplCNO      uint32     // template chunk CNO
}

// RoutePoint is one entry from a PATH chunk (RPT on disk).
type RoutePoint struct {
	X, Y, Z float64 // world-space position
	Dwr     float64 // distance to next point; <0 means use template default
}

// ActorEvent is one entry from a GGAE chunk (AEV on disk).
type ActorEvent struct {
	AET       int32
	Nfrm      int32
	Irpt      int32   // route-point index
	DwrOffset float64 // distance into segment [Irpt, Irpt+1]
	Dnfrm     int32
	VarData   []byte
}

// ParseActorDef parses the ACTF header from raw ACTR chunk data.
//
// ACTF layout (44 bytes):
//
//	[0:2]   int16  bo
//	[2:4]   int16  osk
//	[4:8]   BRS    dxr   — FullRouteOffset X
//	[8:12]  BRS    dyr   — FullRouteOffset Y
//	[12:16] BRS    dzr   — FullRouteOffset Z
//	[16:20] int32  arid
//	[20:24] int32  nfrmFirst
//	[24:28] int32  nfrmLast
//	[28:32] int32  tagTmpl.sid   (ignored)
//	[32:36] int32  tagTmpl.pcrf  (ignored)
//	[36:40] uint32 tagTmpl.ctg
//	[40:44] uint32 tagTmpl.cno
func ParseActorDef(data []byte) (*ActorDef, error) {
	if len(data) < 44 {
		return nil, fmt.Errorf("ACTF: data too short (%d bytes, need 44)", len(data))
	}
	bo := int16(binary.LittleEndian.Uint16(data[0:2]))
	if bo != kboCur {
		return nil, fmt.Errorf("ACTF: unsupported byte order 0x%04X", uint16(bo))
	}
	return &ActorDef{
		FullRouteOffset: [3]float64{
			brsToFloat64(int32(binary.LittleEndian.Uint32(data[4:8]))),
			brsToFloat64(int32(binary.LittleEndian.Uint32(data[8:12]))),
			brsToFloat64(int32(binary.LittleEndian.Uint32(data[12:16]))),
		},
		ARID:       int32(binary.LittleEndian.Uint32(data[16:20])),
		NfrmFirst:  int32(binary.LittleEndian.Uint32(data[20:24])),
		NfrmLast:   int32(binary.LittleEndian.Uint32(data[24:28])),
		TagTmplCTG: binary.LittleEndian.Uint32(data[36:40]),
		TagTmplCNO: binary.LittleEndian.Uint32(data[40:44]),
	}, nil
}

// ParsePath parses a PATH chunk into a slice of RoutePoints.
//
// PATH is a GL (General List) of RPT structs. GL header (12 bytes):
//
//	[0:2]  int16 bo
//	[2:4]  int16 osk
//	[4:8]  int32 cbEntry  (must be 16 for RPT)
//	[8:12] int32 ivMac    (number of entries)
//
// Each RPT (16 bytes): dxr(4), dyr(4), dzr(4), dwr(4) — all int32 BRS.
func ParsePath(data []byte) ([]RoutePoint, error) {
	const hdrSize = 12
	if len(data) < hdrSize {
		return nil, fmt.Errorf("PATH: data too short (%d bytes)", len(data))
	}
	bo := int16(binary.LittleEndian.Uint16(data[0:2]))
	if bo != kboCur {
		return nil, fmt.Errorf("PATH: unsupported byte order 0x%04X", uint16(bo))
	}
	cbEntry := int32(binary.LittleEndian.Uint32(data[4:8]))
	ivMac := int32(binary.LittleEndian.Uint32(data[8:12]))
	if cbEntry != 16 {
		return nil, fmt.Errorf("PATH: unexpected cbEntry %d (expected 16 for RPT)", cbEntry)
	}
	if ivMac < 0 {
		return nil, fmt.Errorf("PATH: negative entry count %d", ivMac)
	}
	need := hdrSize + int(ivMac)*16
	if len(data) < need {
		return nil, fmt.Errorf("PATH: data too short for %d entries: have %d, need %d", ivMac, len(data), need)
	}
	pts := make([]RoutePoint, ivMac)
	for i := range pts {
		off := hdrSize + i*16
		pts[i] = RoutePoint{
			X:   brsToFloat64(int32(binary.LittleEndian.Uint32(data[off : off+4]))),
			Y:   brsToFloat64(int32(binary.LittleEndian.Uint32(data[off+4 : off+8]))),
			Z:   brsToFloat64(int32(binary.LittleEndian.Uint32(data[off+8 : off+12]))),
			Dwr: brsToFloat64(int32(binary.LittleEndian.Uint32(data[off+12 : off+16]))),
		}
	}
	return pts, nil
}

// ParseActorEvents parses a GGAE chunk into a slice of ActorEvents.
//
// GGAE is a GG whose fixed part is an AEV (20 bytes):
//
//	[0:4]   int32 aet
//	[4:8]   int32 nfrm
//	[8:12]  int32 irpt
//	[12:16] BRS   dwrOffset
//	[16:20] int32 dnfrm
func ParseActorEvents(data []byte) ([]ActorEvent, error) {
	_, entries, err := ParseGG(data)
	if err != nil {
		return nil, fmt.Errorf("GGAE: %w", err)
	}
	out := make([]ActorEvent, 0, len(entries))
	for _, e := range entries {
		if len(e.Fixed) < 20 {
			continue
		}
		out = append(out, ActorEvent{
			AET:       int32(binary.LittleEndian.Uint32(e.Fixed[0:4])),
			Nfrm:      int32(binary.LittleEndian.Uint32(e.Fixed[4:8])),
			Irpt:      int32(binary.LittleEndian.Uint32(e.Fixed[8:12])),
			DwrOffset: brsToFloat64(int32(binary.LittleEndian.Uint32(e.Fixed[12:16]))),
			Dnfrm:     int32(binary.LittleEndian.Uint32(e.Fixed[16:20])),
			VarData:   e.Var,
		})
	}
	return out, nil
}

// ActorWorldPos computes the world-space position of an actor at frame nfrm.
// Returns (x, y, z, onStage). If the actor is not on stage at nfrm, onStage=false.
//
// Algorithm:
//  1. Walk all events with Nfrm <= nfrm in order.
//  2. aetAdd marks the start of a subroute and sets the subroute translation.
//  3. aetRem removes the actor from the stage.
//  4. aetMove accumulates an extra path translation.
//  5. The final world position is: routeInterp(path, rtel) + subrouteOffset + FullRouteOffset.
func ActorWorldPos(def *ActorDef, path []RoutePoint, events []ActorEvent, nfrm int32) (x, y, z float64, onStage bool) {
	var subX, subY, subZ float64 // subroute translation (set by aetAdd, accumulated by aetMove)
	var rtelIrpt int32
	var rtelDwrOffset float64

	for i := range events {
		ev := &events[i]
		if ev.Nfrm > nfrm {
			break
		}
		switch ev.AET {
		case aetAdd:
			// AEVADD variable data: dxr(4), dyr(4), dzr(4), xa(2), ya(2), za(2)
			// The RTEL in the fixed part gives the starting route location.
			rtelIrpt = ev.Irpt
			rtelDwrOffset = ev.DwrOffset
			onStage = true
			subX, subY, subZ = 0, 0, 0 // reset subroute translation
			if len(ev.VarData) >= 12 {
				subX = brsToFloat64(int32(binary.LittleEndian.Uint32(ev.VarData[0:4])))
				subY = brsToFloat64(int32(binary.LittleEndian.Uint32(ev.VarData[4:8])))
				subZ = brsToFloat64(int32(binary.LittleEndian.Uint32(ev.VarData[8:12])))
			}
		case aetRem:
			onStage = false
		case aetMove:
			// XYZ translation delta applied to the path (12 bytes: dxr, dyr, dzr).
			if len(ev.VarData) >= 12 {
				subX += brsToFloat64(int32(binary.LittleEndian.Uint32(ev.VarData[0:4])))
				subY += brsToFloat64(int32(binary.LittleEndian.Uint32(ev.VarData[4:8])))
				subZ += brsToFloat64(int32(binary.LittleEndian.Uint32(ev.VarData[8:12])))
			}
		}
	}

	if !onStage || len(path) == 0 {
		return 0, 0, 0, onStage
	}

	// Interpolate position along the route from the last aetAdd RTEL.
	px, py, pz := interpolateRoute(path, rtelIrpt, rtelDwrOffset)

	x = px + subX + def.FullRouteOffset[0]
	y = py + subY + def.FullRouteOffset[1]
	z = pz + subZ + def.FullRouteOffset[2]
	return x, y, z, true
}

// interpolateRoute returns the world-space position for a given RTEL within path.
func interpolateRoute(path []RoutePoint, irpt int32, dwrOffset float64) (x, y, z float64) {
	n := int32(len(path))
	if n == 0 {
		return 0, 0, 0
	}
	i := irpt
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	p0 := path[i]
	if i >= n-1 || p0.Dwr <= 0 {
		return p0.X, p0.Y, p0.Z
	}
	p1 := path[i+1]
	t := dwrOffset / p0.Dwr
	if t < 0 {
		t = 0
	}
	if t > 1 {
		t = 1
	}
	return p0.X + (p1.X-p0.X)*t,
		p0.Y + (p1.Y-p0.Y)*t,
		p0.Z + (p1.Z-p0.Z)*t
}

// AEVSND is the variable-data payload for an aetSnd actor event (44 bytes).
//
// Layout (all little-endian):
//
//	[0:4]   FLoop    — tribool (0=no, 1=yes, 2=maybe)
//	[4:8]   FQueue   — tribool; if true, multiple events coexist in one frame
//	[8:12]  Vlm      — volume
//	[12:16] Celn     — cel number for motion-match; -1 (ivNil) if not motion-match
//	[16:20] Sty      — sound type: 2=SFX, 3=Speech, 4=MIDI
//	[20:24] FNoSound — tribool mute flag
//	[24:28] CHID     — child chunk ID for user sounds
//	[28:32] TagSID   — tag.sid
//	[32:36] TagPCRF  — tag.pcrf (runtime pointer; skip)
//	[36:40] TagCTG   — tag.ctg
//	[40:44] TagCNO   — tag.cno
type AEVSND struct {
	FLoop    int32
	FQueue   int32
	Vlm      int32
	Celn     int32 // -1 means not a motion-match sound
	Sty      int32
	FNoSound int32
	CHID     uint32
	TagSID   int32
	TagPCRF  uint32 // runtime pointer — ignored
	TagCTG   uint32
	TagCNO   uint32
}

// ParseAEVSND parses a 44-byte aetSnd VarData payload.
func ParseAEVSND(data []byte) (*AEVSND, error) {
	if len(data) < 44 {
		return nil, fmt.Errorf("AEVSND: data too short (%d bytes, need 44)", len(data))
	}
	return &AEVSND{
		FLoop:    int32(binary.LittleEndian.Uint32(data[0:4])),
		FQueue:   int32(binary.LittleEndian.Uint32(data[4:8])),
		Vlm:      int32(binary.LittleEndian.Uint32(data[8:12])),
		Celn:     int32(binary.LittleEndian.Uint32(data[12:16])),
		Sty:      int32(binary.LittleEndian.Uint32(data[16:20])),
		FNoSound: int32(binary.LittleEndian.Uint32(data[20:24])),
		CHID:     binary.LittleEndian.Uint32(data[24:28]),
		TagSID:   int32(binary.LittleEndian.Uint32(data[28:32])),
		TagPCRF:  binary.LittleEndian.Uint32(data[32:36]),
		TagCTG:   binary.LittleEndian.Uint32(data[36:40]),
		TagCNO:   binary.LittleEndian.Uint32(data[40:44]),
	}, nil
}

// StyLabel returns a short human-readable label for a sound type value.
func StyLabel(sty int32) string {
	switch sty {
	case 2:
		return "SFX"
	case 3:
		return "Speech"
	case 4:
		return "MIDI"
	default:
		return "Sound"
	}
}

// LoadActor reads the ACTF, PATH and GGAE sub-chunks for one ACTR chunk.
type Actor struct {
	Def    *ActorDef
	Path   []RoutePoint
	Events []ActorEvent
}

func LoadActor(cf *ChunkyFile, r io.ReaderAt, actrCNO uint32) (*Actor, error) {
	actrChunk, ok := cf.FindChunk(ctgACTR, actrCNO)
	if !ok {
		return nil, fmt.Errorf("actor 0x%08X: chunk not found", actrCNO)
	}

	// Parse ACTF header from the ACTR chunk data.
	actrData, err := ChunkData(r, actrChunk)
	if err != nil {
		return nil, fmt.Errorf("actor 0x%08X: reading chunk: %w", actrCNO, err)
	}
	def, err := ParseActorDef(actrData)
	if err != nil {
		return nil, fmt.Errorf("actor 0x%08X: parsing ACTF: %w", actrCNO, err)
	}

	// Parse PATH child (chid=0).
	var path []RoutePoint
	if pathChunk, ok := cf.FindChildByChidCTG(actrChunk, 0, ctgPATH); ok {
		pathData, err := ChunkData(r, pathChunk)
		if err != nil {
			return nil, fmt.Errorf("actor 0x%08X: reading PATH: %w", actrCNO, err)
		}
		path, err = ParsePath(pathData)
		if err != nil {
			return nil, fmt.Errorf("actor 0x%08X: parsing PATH: %w", actrCNO, err)
		}
	}

	// Parse GGAE child (chid=0).
	var events []ActorEvent
	if ggaeChunk, ok := cf.FindChildByChidCTG(actrChunk, 0, ctgGGAE); ok {
		ggaeData, err := ChunkData(r, ggaeChunk)
		if err != nil {
			return nil, fmt.Errorf("actor 0x%08X: reading GGAE: %w", actrCNO, err)
		}
		events, err = ParseActorEvents(ggaeData)
		if err != nil {
			return nil, fmt.Errorf("actor 0x%08X: parsing GGAE: %w", actrCNO, err)
		}
	}

	return &Actor{Def: def, Path: path, Events: events}, nil
}
