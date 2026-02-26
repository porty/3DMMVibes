package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: 3dmm-go <command> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "  chunky   List or extract chunks from a chunky file (.chk)")
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "chunky":
		chunkyMain(flag.Args()[1:])
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n", flag.Arg(0))
		flag.Usage()
		os.Exit(1)
	}
}

func chunkyMain(args []string) {
	fs := flag.NewFlagSet("chunky", flag.ExitOnError)
	doExtract := fs.Bool("extract", false, "Extract chunks to individual files")
	outDir    := fs.String("outdir", ".", "Output directory for extracted chunks")
	ctgStr    := fs.String("ctg", "", `Filter by chunk type (4 chars, e.g. "MVIE")`)
	cnoVal    := fs.Int("cno", -1, "Filter by chunk number (-1 = all chunks)")
	verbose   := fs.Bool("v", false, "Print each file written during extraction")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: 3dmm-go chunky [flags] <file.chk>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Without -extract, lists all chunks. With -extract, writes each chunk")
		fmt.Fprintln(os.Stderr, "to a file named <CTG>_<CNO>.bin in -outdir.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Flags legend: P=packed(compressed)  F=forest(nested chunky)  X=on-extra-file")
		fmt.Fprintln(os.Stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}

	if fs.NArg() < 1 {
		fs.Usage()
		os.Exit(1)
	}

	path := fs.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		fatalf("%v", err)
	}

	chunks := applyFilters(cf.Chunks, *ctgStr, *cnoVal)

	if *doExtract {
		if err := extractChunks(f, chunks, *outDir, *verbose); err != nil {
			fatalf("%v", err)
		}
	} else {
		listChunks(cf, chunks)
	}
}

// applyFilters narrows the chunk list by CTG and/or CNO.
func applyFilters(chunks []Chunk, ctgStr string, cnoVal int) []Chunk {
	if ctgStr == "" && cnoVal < 0 {
		return chunks
	}
	var out []Chunk
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
func listChunks(cf *ChunkyFile, chunks []Chunk) {
	fmt.Printf("creator: %-4s  version: %d/%d  total chunks: %d\n\n",
		ctgToString(cf.Creator), cf.VerCur, cf.VerBack, len(cf.Chunks))
	fmt.Printf("%-4s  %-10s  %-12s  %-10s  %-5s  %s\n",
		"CTG", "CNO", "Offset", "Size", "Flags", "Children")
	fmt.Println(strings.Repeat("-", 58))
	for _, c := range chunks {
		fmt.Printf("%-4s  0x%08X  0x%08X    %-10d  %-5s  %d\n",
			ctgToString(c.CTG), c.CNO, c.Offset, c.Size,
			flagsString(c), c.CKid)
	}
	if len(cf.Chunks) != len(chunks) {
		fmt.Printf("\n(showing %d of %d chunks after filter)\n", len(chunks), len(cf.Chunks))
	}
}

// flagsString returns the compact flag characters for a chunk.
func flagsString(c Chunk) string {
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

// extractChunks writes each chunk's data to outDir/<CTG>_<CNO>.bin.
// Packed chunks are decompressed before writing.
// Chunks with FcrpOnExtra are skipped since their data is not in the main file.
func extractChunks(r io.ReaderAt, chunks []Chunk, outDir string, verbose bool) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", outDir, err)
	}

	var extracted, skipped int
	for _, c := range chunks {
		tag := ctgToString(c.CTG)
		if c.IsOnExtra() {
			fmt.Fprintf(os.Stderr, "skip %s/0x%08X: data is on companion file (fcrpOnExtra)\n", tag, c.CNO)
			skipped++
			continue
		}

		name := fmt.Sprintf("%s_%08X.bin", tag, c.CNO)
		dest := filepath.Join(outDir, name)

		var data []byte
		if c.Size > 0 {
			raw := make([]byte, c.Size)
			if _, err := r.ReadAt(raw, int64(c.Offset)); err != nil {
				return fmt.Errorf("reading %s/0x%08X at 0x%X: %w", tag, c.CNO, c.Offset, err)
			}
			if c.IsPacked() {
				var err error
				data, err = DecodeKauaiChunk(raw)
				if err != nil {
					return fmt.Errorf("decompressing %s/0x%08X: %w", tag, c.CNO, err)
				}
			} else {
				data = raw
			}
		}

		if err := os.WriteFile(dest, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", dest, err)
		}

		if verbose {
			extra := ""
			if c.IsPacked() {
				extra = " [decompressed]"
			}
			if c.IsForest() {
				extra += " [forest]"
			}
			fmt.Printf("wrote %s (%d bytes)%s\n", name, len(data), extra)
		}
		extracted++
	}

	fmt.Printf("extracted %d chunks to %s", extracted, outDir)
	if skipped > 0 {
		fmt.Printf(" (%d skipped, on companion file)", skipped)
	}
	fmt.Println()
	return nil
}

// parseCTGString converts a user-supplied 4-char string (e.g. "MVIE") to the
// uint32 value that ParseChunkyFile stores in Chunk.CTG.
func parseCTGString(s string) uint32 {
	b := [4]byte{' ', ' ', ' ', ' '}
	for i := 0; i < 4 && i < len(s); i++ {
		b[i] = s[i]
	}
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
