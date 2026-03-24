package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"

	mm "github.com/porty/3dmm-go"
	"github.com/porty/3dmm-go/imgterm"
	"github.com/urfave/cli/v2"
)

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

		img, err := mm.ReadMBMP(f)
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
				mc := img.At(x, y).(mm.MBMPColor)
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
