package mm

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ManifestKID is a child-chunk reference in the manifest.
type ManifestKID struct {
	CTG  string `json:"ctg"`
	CNO  string `json:"cno"`
	CHID uint32 `json:"chid"`
}

// ManifestChunk describes a single chunk in the manifest.
type ManifestChunk struct {
	Order        int           `json:"order"`
	CTG          string        `json:"ctg"`
	CNO          string        `json:"cno"`
	File         *string       `json:"file"`
	SizeRaw      int32         `json:"size_raw"`
	SizeUnpacked *int32        `json:"size_unpacked"`
	Flags        uint32        `json:"flags"`
	FlagsDecoded []string      `json:"flags_decoded"`
	Compressed   *bool         `json:"compressed"`
	Skipped      bool          `json:"skipped"`
	Name         *string       `json:"name,omitempty"`
	Children     []ManifestKID `json:"children"`
}

// Manifest is the top-level structure written to manifest.json.
type Manifest struct {
	SourceFile   string          `json:"source_file"`
	Creator      string          `json:"creator"`
	VerCur       int16           `json:"ver_cur"`
	VerBack      int16           `json:"ver_back"`
	CRPFormat    string          `json:"crp_format"`
	ExtractedRaw bool            `json:"extracted_raw"`
	Chunks       []ManifestChunk `json:"chunks"`
}

// WriteManifest encodes m as indented JSON and writes it to <outDir>/manifest.json.
func WriteManifest(outDir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}
	dest := filepath.Join(outDir, "manifest.json")
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return fmt.Errorf("writing manifest: %w", err)
	}
	return nil
}

// BuildManifestChunk builds a ManifestChunk from a Chunk and extraction metadata.
func BuildManifestChunk(c Chunk, file *string, compressed *bool, sizeUnpacked *int32) ManifestChunk {
	kids := make([]ManifestKID, len(c.Kids))
	for i, k := range c.Kids {
		kids[i] = ManifestKID{
			CTG:  CTGToString(k.CTG),
			CNO:  fmt.Sprintf("0x%08X", k.CNO),
			CHID: k.CHID,
		}
	}

	var namePtr *string
	if c.Name != "" {
		s := c.Name
		namePtr = &s
	}

	return ManifestChunk{
		Order:        c.Order,
		CTG:          CTGToString(c.CTG),
		CNO:          fmt.Sprintf("0x%08X", c.CNO),
		File:         file,
		SizeRaw:      c.Size,
		SizeUnpacked: sizeUnpacked,
		Flags:        c.Flags,
		FlagsDecoded: decodeFlagsStrings(c.Flags),
		Compressed:   compressed,
		Skipped:      file == nil,
		Name:         namePtr,
		Children:     kids,
	}
}

// decodeFlagsStrings returns human-readable flag names for a chunk's grfcrp bitmask.
func decodeFlagsStrings(flags uint32) []string {
	var out []string
	if flags&FcrpOnExtra != 0 {
		out = append(out, "on_extra")
	}
	if flags&FcrpLoner != 0 {
		out = append(out, "loner")
	}
	if flags&FcrpPacked != 0 {
		out = append(out, "packed")
	}
	if flags&FcrpForest != 0 {
		out = append(out, "forest")
	}
	if out == nil {
		out = []string{}
	}
	return out
}

// CRPFormatString returns the human-readable CRP format name for cbFixed.
func CRPFormatString(cbFixed int32) string {
	if cbFixed == crpbgFixedSize {
		return "CRPBG"
	}
	return "CRPSM"
}

// PeekUnpackedSize reads cbDst from bytes [4:8] of a CODM-wrapped payload
// (big-endian uint32) without decompressing. Returns 0 on error.
func PeekUnpackedSize(raw []byte) int32 {
	if len(raw) < 8 {
		return 0
	}
	return int32(binary.BigEndian.Uint32(raw[4:8]))
}
