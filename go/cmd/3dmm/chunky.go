package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	mm "github.com/porty/3dmm-go"
	"github.com/urfave/cli/v2"
)

// chunkyCommand returns the `chunky` command with list/extract subcommands.
func chunkyCommand() *cli.Command {
	return &cli.Command{
		Name:  "chunky",
		Usage: "List or extract chunks from a chunky file (.chk)",
		Subcommands: []*cli.Command{
			{
				Name:      "list",
				Usage:     "List all chunks in a chunky file",
				ArgsUsage: "<file.chk>",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "ctg", Usage: `Filter by chunk type (4 chars, e.g. "MVIE")`},
					&cli.IntFlag{Name: "cno", Value: -1, Usage: "Filter by chunk number (-1 = all chunks)"},
					&cli.BoolFlag{Name: "kids", Usage: "Show child chunk types for each chunk"},
				},
				Action: chunkyListAction,
			},
			{
				Name:      "extract",
				Usage:     "Extract chunks to individual files",
				ArgsUsage: "<file.chk>",
				Description: "Writes each chunk to a file named <CTG>_<CNO>.bin in --outdir, plus a manifest.json.\n" +
					"Use --raw to store compressed bytes verbatim (enables byte-for-byte reconstruction).\n\n" +
					"Flags legend: P=packed(compressed)  F=forest(nested chunky)  X=on-extra-file",
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "outdir", Value: ".", Usage: "Output directory for extracted chunks"},
					&cli.StringFlag{Name: "ctg", Usage: `Filter by chunk type (4 chars, e.g. "MVIE")`},
					&cli.IntFlag{Name: "cno", Value: -1, Usage: "Filter by chunk number (-1 = all chunks)"},
					&cli.BoolFlag{Name: "verbose", Aliases: []string{"v"}, Usage: "Print each file written during extraction"},
					&cli.BoolFlag{Name: "raw", Usage: "Keep raw (possibly compressed) chunk bytes for exact reconstruction; writes manifest.json"},
				},
				Action: chunkyExtractAction,
			},
		},
	}
}

func chunkyListAction(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}
	path := c.Args().First()
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	cf, err := mm.ParseChunkyFile(f)
	if err != nil {
		return err
	}

	chunks := applyFilters(cf.Chunks, c.String("ctg"), c.Int("cno"))
	listChunks(cf, chunks, c.Bool("kids"))
	return nil
}

func chunkyExtractAction(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}
	path := c.Args().First()
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	cf, err := mm.ParseChunkyFile(f)
	if err != nil {
		return err
	}

	chunks := applyFilters(cf.Chunks, c.String("ctg"), c.Int("cno"))
	return extractChunks(f, cf, path, chunks, c.String("outdir"), c.Bool("verbose"), c.Bool("raw"))
}

// applyFilters narrows the chunk list by CTG and/or CNO.
func applyFilters(chunks []mm.Chunk, ctgStr string, cnoVal int) []mm.Chunk {
	if ctgStr == "" && cnoVal < 0 {
		return chunks
	}
	var out []mm.Chunk
	for _, c := range chunks {
		if ctgStr != "" && c.CTG != parseCTGString(ctgStr) {
			continue
		}
		if cnoVal >= 0 && c.CNO != uint32(cnoVal) {
			continue
		}
		out = append(out, c)
	}
	return out
}

// listChunks prints a summary table to stdout.
func listChunks(cf *mm.ChunkyFile, chunks []mm.Chunk, showKids bool) {
	fmt.Printf("creator: %-4s  version: %d/%d  total chunks: %d\n\n",
		mm.CTGToString(cf.Creator), cf.VerCur, cf.VerBack, len(cf.Chunks))
	fmt.Printf("%-4s  %-10s  %-12s  %-10s  %-5s  %-8s  %s\n",
		"CTG", "CNO", "Offset", "Size", "Flags", "Children", "Name")
	fmt.Println(strings.Repeat("-", 72))
	for _, c := range chunks {
		kids := fmt.Sprintf("%d", c.CKid)
		if showKids && c.CKid > 0 {
			kids = kidsString(c.Kids)
		}
		fmt.Printf("%-4s  0x%08X  0x%08X    %-10d  %-5s  %-8s  %s\n",
			mm.CTGToString(c.CTG), c.CNO, c.Offset, c.Size,
			flagsString(c), kids, c.Name)
	}
	if len(cf.Chunks) != len(chunks) {
		fmt.Printf("\n(showing %d of %d chunks after filter)\n", len(chunks), len(cf.Chunks))
	}
}

// kidsString returns a compact summary of child chunk types, e.g. "BMDL×4 MTRL×8".
func kidsString(kids []mm.KID) string {
	counts := make(map[string]int)
	order := []string{}
	for _, k := range kids {
		tag := mm.CTGToString(k.CTG)
		if counts[tag] == 0 {
			order = append(order, tag)
		}
		counts[tag]++
	}
	var b strings.Builder
	for i, tag := range order {
		if i > 0 {
			b.WriteByte(' ')
		}
		if counts[tag] == 1 {
			b.WriteString(tag)
		} else {
			fmt.Fprintf(&b, "%s×%d", tag, counts[tag])
		}
	}
	return b.String()
}

// flagsString returns the compact flag characters for a chunk.
func flagsString(c mm.Chunk) string {
	var b strings.Builder
	if c.IsPacked() {
		b.WriteByte('P')
	}
	if c.IsForest() {
		b.WriteByte('F')
	}
	if c.IsOnExtra() {
		b.WriteByte('X')
	}
	return b.String()
}

// extractChunks writes each chunk's data to outDir/<CTG>_<CNO>.bin and writes
// a manifest.json summarising all chunks. Packed chunks are decompressed unless
// raw is true. Chunks with FcrpOnExtra are skipped.
func extractChunks(r io.ReaderAt, cf *mm.ChunkyFile, sourcePath string, chunks []mm.Chunk, outDir string, verbose, raw bool) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", outDir, err)
	}

	var extracted, skipped int
	var manifestChunks []mm.ManifestChunk

	for _, c := range chunks {
		tag := mm.CTGToString(c.CTG)
		if c.IsOnExtra() {
			fmt.Fprintf(os.Stderr, "skip %s/0x%08X: data is on companion file (fcrpOnExtra)\n", tag, c.CNO)
			skipped++
			manifestChunks = append(manifestChunks, mm.BuildManifestChunk(c, nil, nil, nil))
			continue
		}

		name := fmt.Sprintf("%s_%08X.bin", tag, c.CNO)
		dest := filepath.Join(outDir, name)

		var data []byte
		var compressed bool
		var sizeUnpacked int32

		if c.Size > 0 {
			rawBytes := make([]byte, c.Size)
			if _, err := r.ReadAt(rawBytes, int64(c.Offset)); err != nil {
				return fmt.Errorf("reading %s/0x%08X at 0x%X: %w", tag, c.CNO, c.Offset, err)
			}
			if raw {
				data = rawBytes
				compressed = c.IsPacked()
				if c.IsPacked() {
					sizeUnpacked = mm.PeekUnpackedSize(rawBytes)
				} else {
					sizeUnpacked = c.Size
				}
			} else {
				if c.IsPacked() {
					var err error
					data, err = mm.DecodeKauaiChunk(rawBytes)
					if err != nil {
						return fmt.Errorf("decompressing %s/0x%08X: %w", tag, c.CNO, err)
					}
					sizeUnpacked = int32(len(data))
				} else {
					data = rawBytes
					sizeUnpacked = c.Size
				}
				compressed = false
			}
		} else {
			sizeUnpacked = 0
		}

		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", dest, err)
		}

		if verbose {
			extra := ""
			if c.IsPacked() {
				if raw {
					extra = " [raw/compressed]"
				} else {
					extra = " [decompressed]"
				}
			}
			if c.IsForest() {
				extra += " [forest]"
			}
			fmt.Printf("wrote %s (%d bytes)%s\n", name, len(data), extra)
		}
		extracted++

		su := sizeUnpacked
		co := compressed
		manifestChunks = append(manifestChunks, mm.BuildManifestChunk(c, &name, &co, &su))
	}

	fmt.Printf("extracted %d chunks to %s", extracted, outDir)
	if skipped > 0 {
		fmt.Printf(" (%d skipped, on companion file)", skipped)
	}
	fmt.Println()

	m := &mm.Manifest{
		SourceFile:   filepath.Base(sourcePath),
		Creator:      mm.CTGToString(cf.Creator),
		VerCur:       cf.VerCur,
		VerBack:      cf.VerBack,
		CRPFormat:    mm.CRPFormatString(cf.CRPFormat),
		ExtractedRaw: raw,
		Chunks:       manifestChunks,
	}
	if err := mm.WriteManifest(outDir, m); err != nil {
		return err
	}
	fmt.Printf("wrote manifest.json to %s\n", outDir)
	return nil
}

// parseCTGString converts a user-supplied 4-char string (e.g. "MVIE") to the
// uint32 value that mm.ParseChunkyFile stores in Chunk.CTG.
func parseCTGString(s string) uint32 {
	b := [4]byte{' ', ' ', ' ', ' '}
	for i := 0; i < 4 && i < len(s); i++ {
		b[i] = s[i]
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}
