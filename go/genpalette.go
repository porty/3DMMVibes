package mm

import (
	"errors"
	"image"
	"image/color"
)

type Palette struct {
	Colors []color.Color
}

func GenPalette(src image.Image, comparison image.Image) (*Palette, error) {
	srcBounds := src.Bounds()
	cmpBounds := comparison.Bounds()

	w := min(srcBounds.Dx(), cmpBounds.Dx())
	h := min(srcBounds.Dy(), cmpBounds.Dy())
	if w == 0 || h == 0 {
		return nil, errors.New("genpalette: images have no overlap")
	}

	type acc struct{ r, g, b, a, n uint64 }
	var accs [256]acc

	for dy := range h {
		for dx := range w {
			srcPx := src.At(srcBounds.Min.X+dx, srcBounds.Min.Y+dy)
			_, _, _, sa := srcPx.RGBA()
			if sa == 0 {
				continue
			}
			var idx uint8
			if mc, ok := srcPx.(MBMPColor); ok {
				idx = mc.Index
			} else {
				r, _, _, _ := srcPx.RGBA()
				idx = uint8(r >> 8)
			}
			cmpPx := comparison.At(cmpBounds.Min.X+dx, cmpBounds.Min.Y+dy)
			r, g, b, a := cmpPx.RGBA()
			accs[idx].r += uint64(r >> 8)
			accs[idx].g += uint64(g >> 8)
			accs[idx].b += uint64(b >> 8)
			accs[idx].a += uint64(a >> 8)
			accs[idx].n++
		}
	}

	colors := make([]color.Color, 256)
	for i, ac := range accs {
		if ac.n == 0 {
			colors[i] = color.RGBA{}
			continue
		}
		colors[i] = color.RGBA{
			R: uint8(ac.r / ac.n),
			G: uint8(ac.g / ac.n),
			B: uint8(ac.b / ac.n),
			A: uint8(ac.a / ac.n),
		}
	}
	return &Palette{Colors: colors}, nil
}
