package main

import (
	"errors"
	"image"
	"image/color"
)

type Palette struct {
	Colors []color.Color
}

func GenPalette(src image.Image, comparison image.Image) (*Palette, error) {
	return nil, errors.New("TODO: implement")
}
