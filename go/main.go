package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/porty/3dmm-go/imgterm"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:  "3dmm-go",
		Usage: "Tools for working with 3D Movie Maker files",
		Commands: []*cli.Command{
			chunkyCommand(),
			mbmpCommand(),
			bkgdCommand(),
			genpaletteCommand(),
			dagCommand(),
			renderCommand(),
		},
	}
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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

	cf, err := ParseChunkyFile(f)
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

	cf, err := ParseChunkyFile(f)
	if err != nil {
		return err
	}

	chunks := applyFilters(cf.Chunks, c.String("ctg"), c.Int("cno"))
	return extractChunks(f, cf, path, chunks, c.String("outdir"), c.Bool("verbose"), c.Bool("raw"))
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
func listChunks(cf *ChunkyFile, chunks []Chunk, showKids bool) {
	fmt.Printf("creator: %-4s  version: %d/%d  total chunks: %d\n\n",
		ctgToString(cf.Creator), cf.VerCur, cf.VerBack, len(cf.Chunks))
	fmt.Printf("%-4s  %-10s  %-12s  %-10s  %-5s  %-8s  %s\n",
		"CTG", "CNO", "Offset", "Size", "Flags", "Children", "Name")
	fmt.Println(strings.Repeat("-", 72))
	for _, c := range chunks {
		kids := fmt.Sprintf("%d", c.CKid)
		if showKids && c.CKid > 0 {
			kids = kidsString(c.Kids)
		}
		fmt.Printf("%-4s  0x%08X  0x%08X    %-10d  %-5s  %-8s  %s\n",
			ctgToString(c.CTG), c.CNO, c.Offset, c.Size,
			flagsString(c), kids, c.Name)
	}
	if len(cf.Chunks) != len(chunks) {
		fmt.Printf("\n(showing %d of %d chunks after filter)\n", len(chunks), len(cf.Chunks))
	}
}

// kidsString returns a compact summary of child chunk types, e.g. "BMDL×4 MTRL×8".
func kidsString(kids []KID) string {
	counts := make(map[string]int)
	order := []string{}
	for _, k := range kids {
		tag := ctgToString(k.CTG)
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

// extractChunks writes each chunk's data to outDir/<CTG>_<CNO>.bin and writes
// a manifest.json summarising all chunks. Packed chunks are decompressed unless
// raw is true. Chunks with FcrpOnExtra are skipped.
func extractChunks(r io.ReaderAt, cf *ChunkyFile, sourcePath string, chunks []Chunk, outDir string, verbose, raw bool) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating output directory %s: %w", outDir, err)
	}

	var extracted, skipped int
	var manifestChunks []ManifestChunk

	for _, c := range chunks {
		tag := ctgToString(c.CTG)
		if c.IsOnExtra() {
			fmt.Fprintf(os.Stderr, "skip %s/0x%08X: data is on companion file (fcrpOnExtra)\n", tag, c.CNO)
			skipped++
			manifestChunks = append(manifestChunks, buildManifestChunk(c, nil, nil, nil))
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
					sizeUnpacked = peekUnpackedSize(rawBytes)
				} else {
					sizeUnpacked = c.Size
				}
			} else {
				if c.IsPacked() {
					var err error
					data, err = DecodeKauaiChunk(rawBytes)
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
		manifestChunks = append(manifestChunks, buildManifestChunk(c, &name, &co, &su))
	}

	fmt.Printf("extracted %d chunks to %s", extracted, outDir)
	if skipped > 0 {
		fmt.Printf(" (%d skipped, on companion file)", skipped)
	}
	fmt.Println()

	m := &Manifest{
		SourceFile:   filepath.Base(sourcePath),
		Creator:      ctgToString(cf.Creator),
		VerCur:       cf.VerCur,
		VerBack:      cf.VerBack,
		CRPFormat:    crpFormatString(cf.CRPFormat),
		ExtractedRaw: raw,
		Chunks:       manifestChunks,
	}
	if err := writeManifest(outDir, m); err != nil {
		return err
	}
	fmt.Printf("wrote manifest.json to %s\n", outDir)
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

// mbmpCommand returns the `mbmp` command.
func mbmpCommand() *cli.Command {
	return &cli.Command{
		Name:      "mbmp",
		Usage:     "Decode an MBMP chunk and write it as a PNG image",
		ArgsUsage: "<input.mbmp> [...]",
		Description: "Decode an MBMP chunk and write it as a PNG image.\n" +
			"Without --palette, indices are rendered as grayscale (R=G=B=index).",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "o", Usage: "Output PNG file (default: stdout)"},
			&cli.BoolFlag{Name: "view", Usage: "View the image in terminal"},
			&cli.BoolFlag{Name: "info", Usage: "Get information about the MBMP"},
			&cli.StringFlag{Name: "palette", Usage: "Palette file (1024 bytes: 256 × RGBA); if omitted, grayscale"},
		},
		Action: mbmpAction,
	}
}

func mbmpAction(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	var pal []color.RGBA
	if p := c.String("palette"); p != "" {
		pal = loadPalette(p)
	}

	outFile := c.String("o")
	view := c.Bool("view")
	info := c.Bool("info")

	for i, path := range c.Args().Slice() {
		if i > 0 {
			fmt.Println()
		}
		if c.NArg() > 1 {
			fmt.Println(path)
		}

		f, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}

		img, err := ReadMBMP(f)
		f.Close()
		if err != nil {
			return err
		}

		bounds := img.Bounds()

		if info {
			fmt.Printf("Width: %d\nHeight: %d\n", bounds.Dx(), bounds.Dy())
			continue
		}

		out := image.NewNRGBA(bounds)
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			for x := bounds.Min.X; x < bounds.Max.X; x++ {
				mc := img.At(x, y).(MBMPColor)
				if pal != nil {
					pc := pal[mc.Index]
					out.SetNRGBA(x, y, color.NRGBA{R: pc.R, G: pc.G, B: pc.B, A: mc.A})
				} else {
					out.SetNRGBA(x, y, color.NRGBA{R: mc.Index, G: mc.Index, B: mc.Index, A: mc.A})
				}
			}
		}

		if view {
			if err, _ := imgterm.Display(out); err != nil {
				return fmt.Errorf("display: %w", err)
			}
			continue
		}

		var w io.Writer
		if outFile == "" {
			w = os.Stdout
		} else {
			pf, err := os.Create(outFile)
			if err != nil {
				return fmt.Errorf("create %s: %w", outFile, err)
			}
			defer pf.Close()
			w = pf
		}

		if err := png.Encode(w, out); err != nil {
			return fmt.Errorf("encode PNG: %w", err)
		}
	}
	return nil
}

// loadPalette reads a binary palette file (256 × 4 bytes, RGBA) and returns
// the 256 colors. Exits on any error.
func loadPalette(path string) []color.RGBA {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read palette %s: %v\n", path, err)
		os.Exit(1)
	}
	if len(data) != 1024 {
		fmt.Fprintf(os.Stderr, "error: palette %s: expected 1024 bytes (256 × RGBA), got %d\n", path, len(data))
		os.Exit(1)
	}
	pal := make([]color.RGBA, 256)
	for i := range pal {
		pal[i] = color.RGBA{R: data[i*4], G: data[i*4+1], B: data[i*4+2], A: data[i*4+3]}
	}
	return pal
}

// bkgdCommand returns the `bkgd` command.
func bkgdCommand() *cli.Command {
	return &cli.Command{
		Name:      "bkgd",
		Usage:     "Render background camera angles from a chunky file to the terminal",
		ArgsUsage: "<file.chk>",
		Description: "Render each camera angle of a BKGD chunk to the terminal.\n" +
			"The palette is loaded automatically from a GLCR chunk in the file.\n" +
			"If no GLCR is found, indices are rendered as grayscale.",
		Flags: []cli.Flag{
			&cli.IntFlag{Name: "cno", Value: -1, Usage: "BKGD chunk number (-1 = first found)"},
			&cli.BoolFlag{Name: "z", Usage: "Render z-buffer as grayscale (white=close, black=far) instead of color"},
			&cli.Float64Flag{Name: "zmul", Value: 1.0, Usage: "Z-buffer contrast multiplier (>1 boosts contrast, only used with -z)"},
		},
		Action: bkgdAction,
	}
}

func bkgdAction(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	f, err := os.Open(c.Args().First())
	if err != nil {
		return fmt.Errorf("open %s: %w", c.Args().First(), err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		return err
	}

	cnoVal := c.Int("cno")
	var bkgdChunk Chunk
	var found bool
	if cnoVal >= 0 {
		bkgdChunk, found = cf.FindChunk(ctgBKGD, uint32(cnoVal))
		if !found {
			return fmt.Errorf("BKGD chunk with CNO 0x%08X not found", uint32(cnoVal))
		}
	} else {
		for _, ch := range cf.Chunks {
			if ch.CTG == ctgBKGD {
				bkgdChunk = ch
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no BKGD chunk found in %s", c.Args().First())
		}
	}

	base, glcrFound, err := FindGLCR(cf, f)
	if err != nil {
		return fmt.Errorf("loading GLCR palette: %w", err)
	}
	if !glcrFound {
		fmt.Fprintln(os.Stderr, "notice: no GLCR palette chunk found; rendering in grayscale")
		base = GrayscalePalette()
	}

	scene, err := LoadBackgroundScene(f, cf, bkgdChunk.CTG, bkgdChunk.CNO, base)
	if err != nil {
		return err
	}

	fmt.Printf("BKGD 0x%08X: %d camera angle(s), palette base index %d\n\n",
		bkgdChunk.CNO, len(scene.Angles), scene.IndexBase)

	zbuf := c.Bool("z")
	zmul := c.Float64("zmul")

	for _, angle := range scene.Angles {
		fmt.Printf("Camera %d\n", angle.Index)

		var out *image.NRGBA

		if zbuf {
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
					gf := (1.0 - float64(z)/0xFFFF) * 255 * zmul
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
					mc := angle.Img.At(x, y).(MBMPColor)
					pc := scene.Palette.Colors[mc.Index]
					r, g, b, _ := pc.RGBA()
					out.SetNRGBA(x, y, color.NRGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: mc.A})
				}
			}
		}

		if err, _ := imgterm.Display(out); err != nil {
			return fmt.Errorf("display camera %d: %w", angle.Index, err)
		}
		fmt.Println()
	}
	return nil
}

// genpaletteCommand returns the `genpalette` command.
func genpaletteCommand() *cli.Command {
	return &cli.Command{
		Name:      "genpalette",
		Usage:     "Generate a palette from two images",
		ArgsUsage: "<src-image> <comparison-image>",
		Description: "Generate a palette from two images and write it in binary form.\n" +
			"Each color is written as 4 bytes: R G B A.",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "o", Usage: "Output file (default: stdout)"},
		},
		Action: genpaletteAction,
	}
}

func genpaletteAction(c *cli.Context) error {
	if c.NArg() < 2 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	src := loadImage(c.Args().Get(0))
	cmp := loadImage(c.Args().Get(1))

	palette, err := GenPalette(src, cmp)
	if err != nil {
		return fmt.Errorf("genpalette: %w", err)
	}

	var w io.Writer
	if outFile := c.String("o"); outFile == "" {
		w = os.Stdout
	} else {
		pf, err := os.Create(outFile)
		if err != nil {
			return fmt.Errorf("create %s: %w", outFile, err)
		}
		defer pf.Close()
		w = pf
	}

	for _, col := range palette.Colors {
		r, g, b, a := col.RGBA()
		buf := [4]byte{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
		if err := binary.Write(w, binary.BigEndian, buf); err != nil {
			return fmt.Errorf("write palette: %w", err)
		}
	}
	return nil
}

// loadImage opens an image file (PNG, etc.) and decodes it, exiting on error.
func loadImage(path string) image.Image {
	f, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open %s: %v\n", path, err)
		os.Exit(1)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: decode %s: %v\n", path, err)
		os.Exit(1)
	}
	return img
}

// dagCommand returns the `dag` command.
func dagCommand() *cli.Command {
	return &cli.Command{
		Name:      "dag",
		Usage:     "Write a Graphviz DOT file of the chunk parent→child graph",
		ArgsUsage: "<file.chk>",
		Description: "Write a Graphviz DOT digraph of chunk parent→child relationships.\n" +
			"Render with: dot -Tpng -o graph.png output.dot",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "o", Usage: "Output DOT file (default: stdout)"},
			&cli.StringFlag{Name: "ctg", Usage: `Filter by chunk type (4 chars, e.g. "MVIE")`},
			&cli.IntFlag{Name: "cno", Value: -1, Usage: "Filter by chunk number (-1 = all chunks)"},
		},
		Action: dagAction,
	}
}

func dagAction(c *cli.Context) error {
	if c.NArg() < 1 {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("", 1)
	}

	f, err := os.Open(c.Args().First())
	if err != nil {
		return fmt.Errorf("open %s: %w", c.Args().First(), err)
	}
	defer f.Close()

	cf, err := ParseChunkyFile(f)
	if err != nil {
		return err
	}

	if ctg := c.String("ctg"); ctg != "" || c.Int("cno") >= 0 {
		cf.Chunks = applyFilters(cf.Chunks, ctg, c.Int("cno"))
	}

	var w io.Writer
	if outFile := c.String("o"); outFile == "" {
		w = os.Stdout
	} else {
		df, err := os.Create(outFile)
		if err != nil {
			return fmt.Errorf("create %s: %w", outFile, err)
		}
		defer df.Close()
		w = df
	}

	ChunkDAG(cf, w)
	return nil
}
