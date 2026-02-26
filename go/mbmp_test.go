package main

import (
	"bytes"
	"image"
	"testing"
)

// TestMBMP_roundTrip verifies that a simple non-mask MBMP round-trips through
// WriteMBMP → ReadMBMP unchanged.
//
// Layout: 4×2 image with negative reference-point coordinates.
//
//	Row 0:  transparent, opaque(7), opaque(42), transparent   → [1, 2, 7, 42]
//	Row 1:  all transparent (rgcb[1] == 0)
func TestMBMP_roundTrip(t *testing.T) {
	img := &MBMPImage{
		Pix:   []uint8{0, 7, 42, 0, 0, 0, 0, 0},
		Alpha: []uint8{0, 255, 255, 0, 0, 0, 0, 0},
		Rect: image.Rectangle{
			Min: image.Point{X: -2, Y: -1},
			Max: image.Point{X: 2, Y: 1},
		},
	}

	var buf bytes.Buffer
	n, err := WriteMBMP(img, &buf)
	if err != nil {
		t.Fatalf("WriteMBMP: %v", err)
	}
	if n != buf.Len() {
		t.Errorf("WriteMBMP returned %d bytes but buffer has %d", n, buf.Len())
	}

	got, err := ReadMBMP(&buf)
	if err != nil {
		t.Fatalf("ReadMBMP: %v", err)
	}

	if got.Rect != img.Rect {
		t.Errorf("Rect: got %v, want %v", got.Rect, img.Rect)
	}
	if !bytes.Equal(got.Pix, img.Pix) {
		t.Errorf("Pix: got %v, want %v", got.Pix, img.Pix)
	}
	if !bytes.Equal(got.Alpha, img.Alpha) {
		t.Errorf("Alpha: got %v, want %v", got.Alpha, img.Alpha)
	}
	if got.Mask != img.Mask || got.Fill != img.Fill {
		t.Errorf("Mask/Fill: got %v/%d, want %v/%d", got.Mask, got.Fill, img.Mask, img.Fill)
	}
}

// TestMBMP_mask verifies mask-mode: pixel values are not stored in the data;
// all opaque pixels are assigned the fill value on decode.
func TestMBMP_mask(t *testing.T) {
	img := &MBMPImage{
		Pix:   []uint8{5, 5, 5},
		Alpha: []uint8{0, 255, 255},
		Rect: image.Rectangle{
			Min: image.Point{X: 0, Y: 0},
			Max: image.Point{X: 3, Y: 1},
		},
		Mask: true,
		Fill: 5,
	}

	var buf bytes.Buffer
	if _, err := WriteMBMP(img, &buf); err != nil {
		t.Fatalf("WriteMBMP: %v", err)
	}

	// In mask mode, pixel bytes are not stored, so row data is [1, 2].
	got, err := ReadMBMP(&buf)
	if err != nil {
		t.Fatalf("ReadMBMP: %v", err)
	}
	if !got.Mask {
		t.Error("Mask: got false, want true")
	}
	if got.Fill != 5 {
		t.Errorf("Fill: got %d, want 5", got.Fill)
	}
	// Pixels 1 and 2 should be opaque with fill value.
	if got.Alpha[0] != 0 || got.Alpha[1] != 255 || got.Alpha[2] != 255 {
		t.Errorf("Alpha: got %v, want [0 255 255]", got.Alpha)
	}
	if got.Pix[1] != 5 || got.Pix[2] != 5 {
		t.Errorf("Pix[1:3]: got %v, want [5 5]", got.Pix[1:3])
	}
}

// TestMBMP_runOverflow verifies that runs longer than 255 pixels are split
// correctly using zero-count spacer runs.
//
// A row of 257 identical opaque pixels (index 0xAA) should encode as:
//
//	[0, 255, 255×0xAA, 0, 2, 2×0xAA]
func TestMBMP_runOverflow(t *testing.T) {
	const width = 257
	pix := bytes.Repeat([]byte{0xAA}, width)
	alpha := bytes.Repeat([]byte{0xFF}, width)

	img := &MBMPImage{
		Pix:   pix,
		Alpha: alpha,
		Rect:  image.Rectangle{Min: image.Point{}, Max: image.Point{X: width, Y: 1}},
	}

	var buf bytes.Buffer
	if _, err := WriteMBMP(img, &buf); err != nil {
		t.Fatalf("WriteMBMP: %v", err)
	}

	got, err := ReadMBMP(&buf)
	if err != nil {
		t.Fatalf("ReadMBMP: %v", err)
	}
	if !bytes.Equal(got.Pix, pix) {
		t.Errorf("Pix mismatch after overflow round-trip")
	}
	if !bytes.Equal(got.Alpha, alpha) {
		t.Errorf("Alpha mismatch after overflow round-trip")
	}
}

// TestMBMP_empty verifies that an empty MBMP (no pixels) encodes to a
// 28-byte chunk and decodes back correctly.
func TestMBMP_empty(t *testing.T) {
	img := &MBMPImage{
		Rect: image.Rectangle{
			Min: image.Point{X: -3, Y: -3},
			Max: image.Point{X: -3, Y: -3},
		},
	}

	var buf bytes.Buffer
	n, err := WriteMBMP(img, &buf)
	if err != nil {
		t.Fatalf("WriteMBMP: %v", err)
	}
	if n != mbmphSize {
		t.Errorf("expected %d bytes for empty MBMP, got %d", mbmphSize, n)
	}

	got, err := ReadMBMP(&buf)
	if err != nil {
		t.Fatalf("ReadMBMP: %v", err)
	}
	if !got.Rect.Empty() {
		t.Errorf("Rect: expected empty, got %v", got.Rect)
	}
}

// TestMBMP_multiRow exercises a 3×3 image with mixed transparent/opaque rows.
func TestMBMP_multiRow(t *testing.T) {
	// Row 0: [opaque(1), transparent, opaque(2)]
	// Row 1: [all transparent]
	// Row 2: [transparent, transparent, opaque(3)]
	pix := []uint8{
		1, 0, 2,
		0, 0, 0,
		0, 0, 3,
	}
	alpha := []uint8{
		255, 0, 255,
		0, 0, 0,
		0, 0, 255,
	}

	img := &MBMPImage{
		Pix:   pix,
		Alpha: alpha,
		Rect:  image.Rectangle{Min: image.Point{}, Max: image.Point{X: 3, Y: 3}},
	}

	var buf bytes.Buffer
	if _, err := WriteMBMP(img, &buf); err != nil {
		t.Fatalf("WriteMBMP: %v", err)
	}

	got, err := ReadMBMP(&buf)
	if err != nil {
		t.Fatalf("ReadMBMP: %v", err)
	}
	if got.Rect != img.Rect {
		t.Errorf("Rect: got %v, want %v", got.Rect, img.Rect)
	}
	if !bytes.Equal(got.Pix, pix) {
		t.Errorf("Pix: got %v, want %v", got.Pix, pix)
	}
	if !bytes.Equal(got.Alpha, alpha) {
		t.Errorf("Alpha: got %v, want %v", got.Alpha, alpha)
	}
}
