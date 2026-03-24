package mm

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// buildKCDCPayload prepends the 8-byte CODM header (cfmt=0, cbDst) to a raw
// KCDC bit stream so that DecodeKauaiChunk can be called directly.
func buildKCDCPayload(stream []byte, cbDst int) []byte {
	hdr := make([]byte, 8)
	// binary.BigEndian.PutUint32(hdr[0:4], 0)           // cfmt = KCDC
	copy(hdr, kcdc)
	binary.BigEndian.PutUint32(hdr[4:8], uint32(cbDst)) // decompressed size
	return append(hdr, stream...)
}

// TestKCDC_37a exercises the worked example from docs/kauai-codec.md:
// input = 37 consecutive 'a' (0x61) bytes.
//
// Compressed bit stream (LSB first within each byte):
//
//	Byte 0 (flags):  0x00
//	Token 1 — literal 'a':     0  10000110   (9 bits)
//	Token 2 — back-ref off=1 len=36:
//	                            1 0 000000    (8 bits: marker+6b selector+6b raw)
//	                            11111 0 00011 (11 bits: log-encode(35))
//	Token 3 — EOS:             1111 11111111111111111111  (24 bits)
//	[byte-alignment padding]
//	Tail: 6 × 0xFF
func TestKCDC_37a(t *testing.T) {
	// Lay out the bit stream manually (LSB-first within each byte).
	// Bit positions in the stream:
	//   0       : literal marker (0)
	//   1-8     : 0x61 LSB-first = 1,0,0,0,0,1,1,0
	//   9       : back-ref marker (1)
	//   10      : 6-bit selector (0)
	//   11-16   : raw offset 0 (000000)
	//   17-21   : k=5 ones (11111)
	//   22      : terminator (0)
	//   23-27   : r=3 LSB-first (1,1,0,0,0)  [3 = 0b00011]
	//   28-31   : EOS selector first 4 bits (1,1,1,1)
	//   32-51   : EOS raw value 0xFFFFF (20 ones)
	//   [52-55 padding, 56-103 tail]
	bits := []byte{
		// bit 0..7  → byte 1 (src[1])
		// bit0=0 bit1=1 bit2=0 bit3=0 bit4=0 bit5=0 bit6=1 bit7=1
		// = 0b11000010 = 0xC2
		0xC2,
		// bit 8..15 → byte 2 (src[2])
		// bit8=0(MSB of 'a') bit9=1(backref) bit10=0(6b-sel) bit11..15=00000
		// = 0b00000010 = 0x02
		0x02,
		// bit 16..23 → byte 3 (src[3])
		// bit16=0(last raw offset bit) bit17..21=11111(k=5) bit22=0(term) bit23=1(r[0])
		// = 0b10111110 = 0xBE
		0xBE,
		// bit 24..31 → byte 4 (src[4])
		// bit24=1(r[1]) bit25..27=000(r[2..4]) bit28..31=1111(EOS selector)
		// = 0b11110001 = 0xF1
		0xF1,
		// bit 32..39 → byte 5 (src[5]): first 8 of the 20 EOS ones
		0xFF,
		// bit 40..47 → byte 6 (src[6]): next 8 EOS ones
		0xFF,
		// bit 48..51 → byte 7 (src[7]): last 4 EOS ones + 4 padding zeros
		// = 0b00001111 = 0x0F
		0x0F,
	}
	// Flags byte prepended.
	stream := append([]byte{0x00}, bits...)
	// 6-byte 0xFF tail.
	stream = append(stream, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)

	want := bytes.Repeat([]byte{'a'}, 37)
	payload := buildKCDCPayload(stream, len(want))

	got, err := DecodeKauaiChunk(payload)
	if err != nil {
		t.Fatalf("DecodeKauaiChunk error: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestKCDC_singleLiteral checks that a stream containing a single literal
// byte round-trips correctly.
func TestKCDC_singleLiteral(t *testing.T) {
	// Bit stream for one literal byte 0x42 ('B') followed by EOS:
	//
	//   bits  0     = 0            literal marker
	//   bits  1-8   = 0,1,0,0,0,0,1,0   0x42 LSB-first
	//   bits  9-12  = 1,1,1,1     20-bit offset selector
	//   bits 13-32  = 20×1        raw = 0xFFFFF → EOS
	//
	// Packed into bytes (LSB of each byte = lowest-numbered bit):
	//   src[1] bits  0- 7: 0,0,1,0,0,0,0,1  → 0x84
	//   src[2] bits  8-15: 0,1,1,1,1,1,1,1  → 0xFE   (bit8 = MSB of 0x42 = 0)
	//   src[3] bits 16-23: all ones          → 0xFF   (within 20-ones block 13..32)
	//   src[4] bits 24-31: all ones          → 0xFF   (still within 20-ones block)
	//   src[5..10]          = 0xFF tail      (bit 32 = first tail bit = 1 ✓)
	stream := []byte{
		0x00,                               // flags
		0x84,                               // bits 0-7
		0xFE,                               // bits 8-15
		0xFF,                               // bits 16-23
		0xFF,                               // bits 24-31 (all within the 20 EOS ones, positions 13..32)
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, // tail
	}

	payload := buildKCDCPayload(stream, 1)
	got, err := DecodeKauaiChunk(payload)
	if err != nil {
		t.Fatalf("DecodeKauaiChunk error: %v", err)
	}
	if len(got) != 1 || got[0] != 0x42 {
		t.Errorf("got %v, want [0x42]", got)
	}
}

// TestDecodeKauaiChunk_badMagic checks that an unknown format ID is rejected.
func TestDecodeKauaiChunk_badMagic(t *testing.T) {
	data := make([]byte, 20)
	binary.BigEndian.PutUint32(data[0:4], 99) // unknown cfmt
	binary.BigEndian.PutUint32(data[4:8], 1)
	_, err := DecodeKauaiChunk(data)
	if err == nil {
		t.Fatal("expected error for unknown cfmt")
	}
}
