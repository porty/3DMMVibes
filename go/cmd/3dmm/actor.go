package main

import (
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mm "github.com/porty/3dmm-go"
	"github.com/porty/3dmm-go/imgterm"
	"github.com/urfave/cli/v2"
)

// actorCommand returns the `actor` top-level command.
func actorCommand() *cli.Command {
	return &cli.Command{
		Name:  "actor",
		Usage: "Tools for working with actor templates",
		Subcommands: []*cli.Command{
			actorListCommand(),
			actorRenderCommand(),
		},
	}
}

// actorListCommand returns the `actor list` subcommand.
func actorListCommand() *cli.Command {
	return &cli.Command{
		Name:      "list",
		Usage:     "List all TMPL chunks and their actions",
		ArgsUsage: "[TMPLS.3CN]",
		Action:    actorListAction,
	}
}

func actorListAction(c *cli.Context) error {
	path := c.Args().First()
	if path == "" {
		path = "TMPLS.3CN"
	}

	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	cf, err := mm.ParseChunkyFile(f)
	if err != nil {
		return err
	}

	// grftmpl flags (from inc/tmpl.h).
	const (
		ftmplOnlyCustomCostumes = uint32(1)
		ftmplTdt                = uint32(2)
		ftmplProp               = uint32(4)
	)

	// grfactn flags (from inc/tmpl.h).
	const (
		factnRotateX = uint32(1)
		factnRotateY = uint32(2)
		factnRotateZ = uint32(4)
		factnStatic  = uint32(8)
	)

	for _, chunk := range cf.Chunks {
		if chunk.CTG != mm.TagTMPL {
			continue
		}

		// Read TMPLF header for grftmpl.
		data, err := mm.ChunkData(f, chunk)
		if err != nil || len(data) < 16 {
			fmt.Printf("TMPL 0x%08X  <unreadable>\n", chunk.CNO)
			continue
		}
		grftmpl := binary.LittleEndian.Uint32(data[12:16])

		kind := "character"
		if grftmpl&ftmplProp != 0 {
			kind = "prop"
		} else if grftmpl&ftmplTdt != 0 {
			kind = "tdt"
		}

		// Count body parts from GLBS.
		numParts := 0
		for _, kid := range chunk.Kids {
			if kid.CTG == mm.TagGLBS {
				glbsChunk, ok := cf.FindChunk(kid.CTG, kid.CNO)
				if ok {
					glbsData, err := mm.ChunkData(f, glbsChunk)
					if err == nil && len(glbsData) >= 12 {
						numParts = int(binary.LittleEndian.Uint32(glbsData[8:12]))
					}
				}
				break
			}
		}

		// Collect ACTN children sorted by CHID.
		type actnInfo struct {
			chid  uint32
			cels  int
			flags uint32
		}
		var actns []actnInfo
		for _, kid := range chunk.Kids {
			if kid.CTG != mm.TagACTN {
				continue
			}
			actnChunk, ok := cf.FindChunk(kid.CTG, kid.CNO)
			if !ok {
				continue
			}
			// Read grfactn from ACTNF header (8 bytes: bo, osk, grfactn).
			actnData, err := mm.ChunkData(f, actnChunk)
			var grfactn uint32
			if err == nil && len(actnData) >= 8 {
				grfactn = binary.LittleEndian.Uint32(actnData[4:8])
			}
			// Count cels from GGCL.
			cels := 0
			for _, akid := range actnChunk.Kids {
				if akid.CTG == mm.TagGGCL {
					ggclChunk, ok := cf.FindChunk(akid.CTG, akid.CNO)
					if ok {
						ggclData, err := mm.ChunkData(f, ggclChunk)
						if err == nil && len(ggclData) >= 12 {
							cels = int(binary.LittleEndian.Uint32(ggclData[8:12]))
						}
					}
					break
				}
			}
			actns = append(actns, actnInfo{chid: kid.CHID, cels: cels, flags: grfactn})
		}
		// Sort by CHID.
		sort.Slice(actns, func(i, j int) bool { return actns[i].chid < actns[j].chid })

		name := chunk.Name
		if name == "" {
			name = "(unnamed)"
		}
		fmt.Printf("TMPL 0x%08X  %-9s  parts=%d  actions=%d  %s\n",
			chunk.CNO, kind, numParts, len(actns), name)

		for _, a := range actns {
			var flags []string
			if a.flags&factnStatic != 0 {
				flags = append(flags, "static")
			}
			if a.flags&factnRotateX != 0 {
				flags = append(flags, "rotX")
			}
			if a.flags&factnRotateY != 0 {
				flags = append(flags, "rotY")
			}
			if a.flags&factnRotateZ != 0 {
				flags = append(flags, "rotZ")
			}
			flagStr := ""
			if len(flags) > 0 {
				flagStr = "  [" + strings.Join(flags, ",") + "]"
			}
			fmt.Printf("  action %2d  cels=%d%s\n", a.chid, a.cels, flagStr)
		}
	}
	return nil
}

// actorRenderCommand returns the `actor render` subcommand.
func actorRenderCommand() *cli.Command {
	return &cli.Command{
		Name:      "render",
		Usage:     "Render an actor template as a flat-shaded PNG",
		ArgsUsage: "[TMPLS.3CN]",
		Description: "Renders one cel of an actor template as a flat-shaded PNG.\n" +
			"Body parts are colored by their body-part-set index (ibset).\n" +
			"Use --cno all to render every TMPL in the file.\n" +
			"--cno accepts a hex CNO (0x2010), an actor name (\"Keesha\"), or \"all\".\n" +
			"Character actor CNOs: 0x2010–0x203C in TMPLS.3CN.",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "cno",
				Usage: `CNO of the TMPL chunk (hex, e.g. 0x2010), actor name (e.g. "Keesha"), or "all"`,
			},
			&cli.StringFlag{Name: "o", Usage: `Output file (png) or directory (gif --cno all); default: stdout`},
			&cli.BoolFlag{Name: "t", Usage: "Display the image in the terminal (png only)"},
			&cli.IntFlag{Name: "width", Value: 512, Usage: "Output image width in pixels"},
			&cli.IntFlag{Name: "height", Value: 512, Usage: "Output image height in pixels"},
			&cli.IntFlag{Name: "actn", Value: 0, Usage: "Action CHID to render"},
			&cli.IntFlag{Name: "cel", Value: 0, Usage: "Cel index within the action"},
			&cli.IntFlag{Name: "cols", Value: 8, Usage: "Number of columns when rendering --cno all (png only)"},
			&cli.StringFlag{
				Name:    "format",
				Aliases: []string{"f"},
				Value:   "png",
				Usage:   `Output format: "png" or "gif"`,
			},
		},
		Action: actorRenderAction,
	}
}

func actorRenderAction(c *cli.Context) error {
	cnoStr := c.String("cno")
	if cnoStr == "" {
		_ = cli.ShowSubcommandHelp(c)
		return cli.Exit("--cno is required", 1)
	}

	format := c.String("format")
	if format != "png" && format != "gif" {
		return cli.Exit(`--format must be "png" or "gif"`, 1)
	}

	p := mm.RenderParams{
		Width:      c.Int("width"),
		Height:     c.Int("height"),
		ActionCHID: uint32(c.Int("actn")),
		CelIdx:     c.Int("cel"),
	}
	terminal := c.Bool("t")
	outPath := c.String("o")

	// Open and parse the chunky file.
	path := c.Args().First()
	if path == "" {
		path = "TMPLS.3CN"
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	cf, err := mm.ParseChunkyFile(f)
	if err != nil {
		return err
	}

	// Load palette for texture rendering (best-effort; nil palette = ibsetColors fallback).
	pal, _, _ := mm.FindGLCR(cf, f)
	p.Palette = pal

	// Collect the CNOs to render.
	var cnos []uint32
	if cnoStr == "all" {
		for _, chunk := range cf.Chunks {
			if chunk.CTG == mm.TagTMPL {
				cnos = append(cnos, chunk.CNO)
			}
		}
		if len(cnos) == 0 {
			return fmt.Errorf("no TMPL chunks found in %s", path)
		}
	} else {
		var cno uint32
		if _, err := fmt.Sscanf(cnoStr, "0x%x", &cno); err != nil {
			if _, err2 := fmt.Sscanf(cnoStr, "%x", &cno); err2 != nil {
				// Try matching by name.
				nameLower := strings.ToLower(cnoStr)
				for _, chunk := range cf.Chunks {
					if chunk.CTG == mm.TagTMPL && strings.ToLower(chunk.Name) == nameLower {
						cnos = append(cnos, chunk.CNO)
					}
				}
				if len(cnos) == 0 {
					return fmt.Errorf("no TMPL found with name %q", cnoStr)
				}
			} else {
				cnos = []uint32{cno}
			}
		} else {
			cnos = []uint32{cno}
		}
	}

	// Render each CNO.
	const cropMargin = 8

	if terminal {
		for _, cno := range cnos {
			img, err := mm.RenderTemplate(cf, f, cno, p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip TMPL/0x%08X: %v\n", cno, err)
				continue
			}
			fmt.Printf("TMPL 0x%08X\n", cno)
			if err, _ := imgterm.Display(cropToContent(img, cropMargin)); err != nil {
				return fmt.Errorf("display TMPL/0x%08X: %w", cno, err)
			}
			fmt.Println()
		}
		return nil
	}

	// GIF output: rotate the actor 360° over 48 frames at ~12fps.
	if format == "gif" {
		if len(cnos) == 1 {
			g, err := renderActorGIF(cf, f, cnos[0], p)
			if err != nil {
				return err
			}
			var w io.Writer = os.Stdout
			if outPath != "" {
				wf, err := os.Create(outPath)
				if err != nil {
					return fmt.Errorf("create %s: %w", outPath, err)
				}
				defer wf.Close()
				w = wf
			}
			return gif.EncodeAll(w, g)
		}
		// Multiple CNOs: write one GIF per actor to a directory.
		outDir := outPath
		if outDir == "" {
			outDir = "."
		}
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return fmt.Errorf("create output dir: %w", err)
		}
		for _, cno := range cnos {
			name := fmt.Sprintf("TMPL_0x%08X.gif", cno)
			for _, chunk := range cf.Chunks {
				if chunk.CTG == mm.TagTMPL && chunk.CNO == cno && chunk.Name != "" {
					name = chunk.Name + ".gif"
					break
				}
			}
			g, err := renderActorGIF(cf, f, cno, p)
			if err != nil {
				fmt.Fprintf(os.Stderr, "skip TMPL/0x%08X: %v\n", cno, err)
				continue
			}
			outFile := filepath.Join(outDir, name)
			wf, err := os.Create(outFile)
			if err != nil {
				return fmt.Errorf("create %s: %w", outFile, err)
			}
			if err := gif.EncodeAll(wf, g); err != nil {
				wf.Close()
				return fmt.Errorf("encode GIF %s: %w", outFile, err)
			}
			wf.Close()
		}
		return nil
	}

	// PNG output: render all, crop, then arrange in grid or column.
	var imgs []*image.NRGBA
	for _, cno := range cnos {
		img, err := mm.RenderTemplate(cf, f, cno, p)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip TMPL/0x%08X: %v\n", cno, err)
			continue
		}
		imgs = append(imgs, cropToContent(img, cropMargin))
	}
	if len(imgs) == 0 {
		return fmt.Errorf("no templates rendered successfully")
	}

	cols := c.Int("cols")
	var out *image.NRGBA
	if len(cnos) > 1 && cols > 1 {
		out = stitchGrid(imgs, cols)
	} else {
		out = stitchVertical(imgs)
	}

	var w *os.File
	if outPath == "" {
		w = os.Stdout
	} else {
		w, err = os.Create(outPath)
		if err != nil {
			return fmt.Errorf("create %s: %w", outPath, err)
		}
		defer w.Close()
	}
	return png.Encode(w, out)
}

// renderActorGIF renders an actor template rotating 360° over 48 frames at ~12fps.
// Each frame uses the full p.Width × p.Height canvas (no cropping) so the canvas
// size is stable across all frames.
func renderActorGIF(cf *mm.ChunkyFile, r *os.File, cno uint32, p mm.RenderParams) (*gif.GIF, error) {
	const nFrames = 48
	const gifDelay = 8 // centiseconds ≈ 12.5fps (closest integer to 100/12)

	palette := mm.ActorPalette()
	var g gif.GIF
	g.LoopCount = 0 // loop forever

	for i := range nFrames {
		p.YawDeg = float64(i) * 360.0 / nFrames
		img, err := mm.RenderTemplate(cf, r, cno, p)
		if err != nil {
			return nil, fmt.Errorf("render frame %d: %w", i, err)
		}
		palImg := image.NewPaletted(img.Bounds(), palette)
		draw.Draw(palImg, img.Bounds(), img, image.Point{}, draw.Src)
		g.Image = append(g.Image, palImg)
		g.Delay = append(g.Delay, gifDelay)
	}
	return &g, nil
}

// cropToContent crops an NRGBA image to the bounding box of non-black pixels,
// plus a small margin. Returns the original image if no content is found.
func cropToContent(img *image.NRGBA, margin int) *image.NRGBA {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X, b.Min.Y
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := img.NRGBAAt(x, y)
			if c.R != 0 || c.G != 0 || c.B != 0 {
				if x < minX {
					minX = x
				}
				if x > maxX {
					maxX = x
				}
				if y < minY {
					minY = y
				}
				if y > maxY {
					maxY = y
				}
			}
		}
	}
	if minX > maxX || minY > maxY {
		return img // nothing found
	}
	minX = max(b.Min.X, minX-margin)
	minY = max(b.Min.Y, minY-margin)
	maxX = min(b.Max.X, maxX+margin+1)
	maxY = min(b.Max.Y, maxY+margin+1)
	out := image.NewNRGBA(image.Rect(0, 0, maxX-minX, maxY-minY))
	draw.Draw(out, out.Bounds(), img, image.Point{minX, minY}, draw.Src)
	return out
}

// stitchGrid arranges images into a grid with the given number of columns.
// Each cell is sized to the maximum width and height across all images.
// Images are centered within their cell on a black background.
func stitchGrid(imgs []*image.NRGBA, cols int) *image.NRGBA {
	if len(imgs) == 0 {
		return image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	cellW, cellH := 0, 0
	for _, img := range imgs {
		if dx := img.Bounds().Dx(); dx > cellW {
			cellW = dx
		}
		if dy := img.Bounds().Dy(); dy > cellH {
			cellH = dy
		}
	}
	rows := (len(imgs) + cols - 1) / cols
	out := image.NewNRGBA(image.Rect(0, 0, cols*cellW, rows*cellH))
	for i, img := range imgs {
		col := i % cols
		row := i / cols
		b := img.Bounds()
		xOff := col*cellW + (cellW-b.Dx())/2
		yOff := row*cellH + (cellH-b.Dy())/2
		draw.Draw(out, image.Rect(xOff, yOff, xOff+b.Dx(), yOff+b.Dy()), img, image.Point{}, draw.Src)
	}
	return out
}

// stitchVertical concatenates images top-to-bottom into a single NRGBA image.
// Images may have different widths; narrower ones are centered on a black background.
func stitchVertical(imgs []*image.NRGBA) *image.NRGBA {
	if len(imgs) == 0 {
		return image.NewNRGBA(image.Rect(0, 0, 1, 1))
	}
	w := 0
	totalH := 0
	for _, img := range imgs {
		if dx := img.Bounds().Dx(); dx > w {
			w = dx
		}
		totalH += img.Bounds().Dy()
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, totalH))
	y := 0
	for _, img := range imgs {
		b := img.Bounds()
		dx := b.Dx()
		xOff := (w - dx) / 2 // center narrower images
		draw.Draw(out, image.Rect(xOff, y, xOff+dx, y+b.Dy()), img, image.Point{}, draw.Src)
		y += b.Dy()
	}
	return out
}
