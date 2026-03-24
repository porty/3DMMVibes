package main

import (
	"encoding/binary"
	"fmt"
	"image"
	_ "image/png"
	"io"
	"os"

	mm "github.com/porty/3dmm-go"
	"github.com/urfave/cli/v2"
)

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

	palette, err := mm.GenPalette(src, cmp)
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
