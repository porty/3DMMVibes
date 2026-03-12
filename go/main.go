package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/porty/3dmm-go/imgterm"
)

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: 3dmm-go <command> [flags]")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Commands:")
		fmt.Fprintln(os.Stderr, "  chunky      List or extract chunks from a chunky file (.chk)")
		fmt.Fprintln(os.Stderr, "  mbmp        Decode an MBMP chunk and write it as a PNG image")
		fmt.Fprintln(os.Stderr, "  bkgd        Render background camera angles from a chunky file to the terminal")
		fmt.Fprintln(os.Stderr, "  genpalette  Generate a palette from two images")
		fmt.Fprintln(os.Stderr, "  dag         Write a Graphviz DOT file of the chunk parent→child graph")
	}
	flag.Parse()

	if flag.NArg() < 1 {
		flag.Usage()
		os.Exit(1)
	}

	switch flag.Arg(0) {
	case "chunky":
		chunkyMain(flag.Args()[1:])
	case "mbmp":
		mbmpMain(flag.Args()[1:])
	case "bkgd":
		bkgdMain(flag.Args()[1:])
	case "genpalette":
		genpaletteMain(flag.Args()[1:])
	case "dag":
		dagMain(flag.Args()[1:])
	default:
		fmt.Fprintf(os.Stderr, "error: unknown command %q\n", flag.Arg(0))
		flag.Usage()
		os.Exit(1)
	}
}

func chunkyMain(args []string) {
	fs := flag.NewFlagSet("chunky", flag.ExitOnError)
	doExtract := fs.Bool("extract", false, "Extract chunks to individual files")
	outDir := fs.String("outdir", ".", "Output directory for extracted chunks")
	ctgStr := fs.String("ctg", "", `Filter by chunk type (4 chars, e.g. "MVIE")`)
	cnoVal := fs.Int("cno", -1, "Filter by chunk number (-1 = all chunks)")
	verbose := fs.Bool("v", false, "Print each file written during extraction")
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

func mbmpMain(args []string) {
	fs := flag.NewFlagSet("mbmp", flag.ExitOnError)
	outFile := fs.String("o", "", "Output PNG file (default: stdout)")
	view := fs.Bool("view", false, "View the image in terminal")
	info := fs.Bool("info", false, "Get information about the MBMP")
	paletteFile := fs.String("palette", "", "Palette file (1024 bytes: 256 × RGBA); if omitted, grayscale")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: 3dmm-go mbmp [-view] [-info] [-palette palette.bin] [-o output.png] <input.mbmp>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Decode an MBMP chunk and write it as a PNG image.")
		fmt.Fprintln(os.Stderr, "Without -palette, indices are rendered as grayscale (R=G=B=index).")
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

	var pal []color.RGBA
	if *paletteFile != "" {
		pal = loadPalette(*paletteFile)
	}

	for i, path := range fs.Args() {
		if i > 0 {
			fmt.Println()
		}
		if fs.NArg() > 1 {
			fmt.Println(path)
		}

		f, err := os.Open(path)
		if err != nil {
			fatalf("open %s: %v", path, err)
		}

		img, err := ReadMBMP(f)
		f.Close()
		if err != nil {
			fatalf("%v", err)
		}

		bounds := img.Bounds()

		if *info {
			fmt.Printf("Width: %d\nHeight: %d\n", bounds.Dx(), bounds.Dy())
			continue
		}

		// Convert to NRGBA. With a palette, map indices to their actual colors.
		// Without a palette, use grayscale (R=G=B=index) with the decoded alpha.
		out := image.NewNRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				c := img.At(x, y).(MBMPColor)
				if pal != nil {
					pc := pal[c.Index]
					// Use src alpha so transparency from the MBMP mask is preserved.
					out.SetNRGBA(x, y, color.NRGBA{R: pc.R, G: pc.G, B: pc.B, A: c.A})
				} else {
					out.SetNRGBA(x, y, color.NRGBA{R: c.Index, G: c.Index, B: c.Index, A: c.A})
				}
			}
		}

		if *view {
			if err, _ := imgterm.Display(out); err != nil {
				panic(err)
			}
			continue
		}

		var w io.Writer
		if *outFile == "" {
			w = os.Stdout
		} else {
			pf, err := os.Create(*outFile)
			if err != nil {
				fatalf("create %s: %v", *outFile, err)
			}
			defer pf.Close()
			w = pf
		}

		if err := png.Encode(w, out); err != nil {
			fatalf("encode PNG: %v", err)
		}
	}
}

// loadPalette reads a binary palette file (256 × 4 bytes, RGBA) and returns
// the 256 colors. Exits on any error.
func loadPalette(path string) []color.RGBA {
	data, err := os.ReadFile(path)
	if err != nil {
		fatalf("read palette %s: %v", path, err)
	}
	if len(data) != 1024 {
		fatalf("palette %s: expected 1024 bytes (256 × RGBA), got %d", path, len(data))
	}
	pal := make([]color.RGBA, 256)
	for i := range pal {
		pal[i] = color.RGBA{R: data[i*4], G: data[i*4+1], B: data[i*4+2], A: data[i*4+3]}
	}
	return pal
}

func bkgdMain(args []string) {
	fs := flag.NewFlagSet("bkgd", flag.ExitOnError)
	cnoVal := fs.Int("cno", -1, "BKGD chunk number (-1 = first found)")
	zbuf := fs.Bool("z", false, "Render z-buffer as grayscale (white=close, black=far) instead of color")
	zmul := fs.Float64("zmul", 1.0, "Z-buffer contrast multiplier (>1 boosts contrast, only used with -z)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: 3dmm-go bkgd [-cno N] [-z] [-zmul N] <file.chk>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Render each camera angle of a BKGD chunk to the terminal.")
		fmt.Fprintln(os.Stderr, "The palette is loaded automatically from a GLCR chunk in the file.")
		fmt.Fprintln(os.Stderr, "If no GLCR is found, indices are rendered as grayscale.")
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

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fatalf("open %s: %v", fs.Arg(0), err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		fatalf("%v", err)
	}

	// Find the BKGD chunk.
	var bkgdChunk Chunk
	var found bool
	if *cnoVal >= 0 {
		bkgdChunk, found = cf.FindChunk(ctgBKGD, uint32(*cnoVal))
		if !found {
			fatalf("BKGD chunk with CNO 0x%08X not found", uint32(*cnoVal))
		}
	} else {
		for _, c := range cf.Chunks {
			if c.CTG == ctgBKGD {
				bkgdChunk = c
				found = true
				break
			}
		}
		if !found {
			fatalf("no BKGD chunk found in %s", fs.Arg(0))
		}
	}

	// Auto-load palette from GLCR chunk; fall back to grayscale.
	base, glcrFound, err := FindGLCR(cf, f)
	if err != nil {
		fatalf("loading GLCR palette: %v", err)
	}
	if !glcrFound {
		fmt.Fprintln(os.Stderr, "notice: no GLCR palette chunk found; rendering in grayscale")
		base = GrayscalePalette()
	}

	scene, err := LoadBackgroundScene(f, cf, bkgdChunk.CTG, bkgdChunk.CNO, base)
	if err != nil {
		fatalf("%v", err)
	}

	fmt.Printf("BKGD 0x%08X: %d camera angle(s), palette base index %d\n\n",
		bkgdChunk.CNO, len(scene.Angles), scene.IndexBase)

	for _, angle := range scene.Angles {
		fmt.Printf("Camera %d\n", angle.Index)

		var out *image.NRGBA

		if *zbuf {
			if angle.ZBuf == nil {
				fmt.Fprintf(os.Stderr, "camera %d: no ZBMP chunk found, skipping\n", angle.Index)
				continue
			}
			bounds := angle.ZBuf.Rect
			out = image.NewNRGBA(bounds)
			dx := bounds.Dx()
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					i := (y-bounds.Min.Y)*dx + (x - bounds.Min.X)
					z := angle.ZBuf.Pix[i]
					// 0x0000 = closest (white), 0xFFFF = farthest (black)
					gf := (1.0 - float64(z)/0xFFFF) * 255 * *zmul
					if gf > 255 {
						gf = 255
					}
					gray := uint8(gf)
					out.SetNRGBA(x, y, color.NRGBA{R: gray, G: gray, B: gray, A: 255})
				}
			}
		} else {
			bounds := angle.Img.Bounds()
			out = image.NewNRGBA(bounds)
			for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
				for x := bounds.Min.X; x < bounds.Max.X; x++ {
					c := angle.Img.At(x, y).(MBMPColor)
					pc := scene.Palette.Colors[c.Index]
					r, g, b, _ := pc.RGBA()
					out.SetNRGBA(x, y, color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: c.A})
				}
			}
		}

		if err, _ := imgterm.Display(out); err != nil {
			fatalf("display camera %d: %v", angle.Index, err)
		}
		fmt.Println()
	}
}

func genpaletteMain(args []string) {
	fs := flag.NewFlagSet("genpalette", flag.ExitOnError)
	outFile := fs.String("o", "", "Output file (default: stdout)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: 3dmm-go genpalette [-o output] <src-image> <comparison-image>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Generate a palette from two images and write it in binary form.")
		fmt.Fprintln(os.Stderr, "Each color is written as 4 bytes: R G B A.")
		fmt.Fprintln(os.Stderr, "")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		os.Exit(1)
	}
	if fs.NArg() < 2 {
		fs.Usage()
		os.Exit(1)
	}

	src := loadImage(fs.Arg(0))
	cmp := loadImage(fs.Arg(1))

	palette, err := GenPalette(src, cmp)
	if err != nil {
		fatalf("genpalette: %v", err)
	}

	var w io.Writer
	if *outFile == "" {
		w = os.Stdout
	} else {
		pf, err := os.Create(*outFile)
		if err != nil {
			fatalf("create %s: %v", *outFile, err)
		}
		defer pf.Close()
		w = pf
	}

	for _, c := range palette.Colors {
		r, g, b, a := c.RGBA()
		buf := [4]byte{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
		if err := binary.Write(w, binary.BigEndian, buf); err != nil {
			fatalf("write palette: %v", err)
		}
	}
}

// loadImage opens an image file (PNG, etc.) and decodes it, exiting on error.
func loadImage(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		fatalf("decode %s: %v", path, err)
	}
	return img
}

func dagMain(args []string) {
	fs := flag.NewFlagSet("dag", flag.ExitOnError)
	outFile := fs.String("o", "", "Output DOT file (default: stdout)")
	ctgStr := fs.String("ctg", "", `Filter by chunk type (4 chars, e.g. "MVIE")`)
	cnoVal := fs.Int("cno", -1, "Filter by chunk number (-1 = all chunks)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: 3dmm-go dag [-o output.dot] [-ctg TYPE] [-cno NUM] <file.chk>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Write a Graphviz DOT digraph of chunk parent→child relationships.")
		fmt.Fprintln(os.Stderr, "Render with: dot -Tpng -o graph.png output.dot")
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

	f, err := os.Open(fs.Arg(0))
	if err != nil {
		fatalf("open %s: %v", fs.Arg(0), err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		fatalf("%v", err)
	}

	// When a filter is applied, restrict to those chunks but preserve their
	// KID edges so referenced children still appear in the graph.
	if *ctgStr != "" || *cnoVal >= 0 {
		cf.Chunks = applyFilters(cf.Chunks, *ctgStr, *cnoVal)
	}

	var w io.Writer
	if *outFile == "" {
		w = os.Stdout
	} else {
		df, err := os.Create(*outFile)
		if err != nil {
			fatalf("create %s: %v", *outFile, err)
		}
		defer df.Close()
		w = df
	}

	ChunkDAG(cf, w)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
