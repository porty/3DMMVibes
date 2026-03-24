package main

import (
	"fmt"
	"image"
	"image/color"
	"os"

	mm "github.com/porty/3dmm-go"
	"github.com/porty/3dmm-go/imgterm"
	"github.com/urfave/cli/v2"
)

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

	cf, err := mm.ParseChunkyFile(f)
	if err != nil {
		return err
	}

	cnoVal := c.Int("cno")
	var bkgdChunk mm.Chunk
	var found bool
	if cnoVal >= 0 {
		bkgdChunk, found = cf.FindChunk(mm.TagBKGD, uint32(cnoVal))
		if !found {
			return fmt.Errorf("BKGD chunk with CNO 0x%08X not found", uint32(cnoVal))
		}
	} else {
		for _, ch := range cf.Chunks {
			if ch.CTG == mm.TagBKGD {
				bkgdChunk = ch
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("no BKGD chunk found in %s", c.Args().First())
		}
	}

	base, glcrFound, err := mm.FindGLCR(cf, f)
	if err != nil {
		return fmt.Errorf("loading GLCR palette: %w", err)
	}
	if !glcrFound {
		fmt.Fprintln(os.Stderr, "notice: no GLCR palette chunk found; rendering in grayscale")
		base = mm.GrayscalePalette()
	}

	scene, err := mm.LoadBackgroundScene(f, cf, bkgdChunk.CTG, bkgdChunk.CNO, base)
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
					mc := angle.Img.At(x, y).(mm.MBMPColor)
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
