// Kauai codec decompressor.
//
// Compressed chunk payload layout (prepended by the CODM wrapper):
//
//	[cfmt: 4 bytes big-endian]  — 0 = KCDC, 1 = KCD2
//	[cbDst: 4 bytes big-endian] — decompressed size in bytes
//	[flags byte: 0x00]
//	[bit stream]
//	[tail: 6 × 0xFF]
//
// References: kauai/src/codkauai.cpp, kauai/src/codkpri.h,
//
//	docs/kauai-codec.md

package mm

import (
	"encoding/binary"
	"fmt"
)

const (
	kcbCodecHeader = 8          // 4-byte cfmt (BE) + 4-byte cbDst (BE)
	kcbTail        = 6          // six 0xFF bytes terminating every compressed stream
	cfmtKCDC       = 0x4B434443 // kcfmtKauai - 'KCDC'
	cfmtKCD2       = 0x4B434432 // kcfmtKauai2 - 'KCD2'
)

var (
	kcdc = []byte{'K', 'C', 'D', 'C'}
	// kcd2 = []byte{'K', 'C', 'D', '2'}
)

// DecodeKauaiChunk decompresses a packed Kauai chunk.
// data is the raw bytes at Chunk.Offset in the file: the 8-byte CODM header
// followed by the compressed bit stream.
func DecodeKauaiChunk(data []byte) ([]byte, error) {
	if len(data) < kcbCodecHeader {
		return nil, fmt.Errorf("kauai codec: data too short for header (%d bytes)", len(data))
	}
	cfmt := int(binary.BigEndian.Uint32(data[0:4]))
	cbDst := int(binary.BigEndian.Uint32(data[4:8]))
	if cbDst <= 0 {
		return nil, fmt.Errorf("kauai codec: invalid decompressed size %d", cbDst)
	}
	compressed := data[kcbCodecHeader:]

	switch cfmt {
	case cfmtKCDC:
		return decodeKCDC(compressed, cbDst)
	case cfmtKCD2:
		return decodeKCD2(compressed, cbDst)
	default:
		return nil, fmt.Errorf("kauai codec: unknown format %d", cfmt)
	}
}

// kcReadU32LE reads a little-endian uint32 from src[pos-4 : pos].
// It mirrors the C original's `*(ulong*)(pbSrc - 4)` window load.
// Returns 0 on out-of-bounds access (should not happen with valid data + tail).
func kcReadU32LE(src []byte, pos int) uint32 {
	start := pos - 4
	if start < 0 || pos > len(src) {
		return 0
	}
	return binary.LittleEndian.Uint32(src[start:pos])
}

// decodeKCDC implements the KCDC (kcfmtKauai = 0) decompressor.
//
// Algorithm: standard LZ77. Each token is either a literal byte (9 bits) or
// a back-reference (offset + log-encoded match length). End-of-stream is a
// 20-bit offset with raw value 0xFFFFF.
//
// This matches the x86 ASM implementation (kcdc_386.h), not the buggy C
// fallback in _FDecode. In particular the match-length data bits are read
// from bit position (ibit + k + 1), not the C fallback's erroneous (k + 1).
func decodeKCDC(src []byte, cbDst int) ([]byte, error) {
	if err := kcValidateStream(src); err != nil {
		return nil, fmt.Errorf("kcdc: %w", err)
	}

	out := make([]byte, 0, cbDst)

	// pbSrc starts at src[1] (past the flags byte), then _Advance(4) moves it
	// to src[5], making luCur = LE32(src[1:5]).
	pos := 5
	luCur := kcReadU32LE(src, pos)
	ibit := uint(0)

	advance := func() {
		pos += int(ibit >> 3)
		luCur = kcReadU32LE(src, pos)
		ibit &= 7
	}

	for {
		if luCur>>(ibit)&1 == 0 {
			// ── Literal (9 bits) ──────────────────────────────────────
			out = append(out, byte(luCur>>(ibit+1)))
			ibit += 9
		} else {
			// ── Back-reference ────────────────────────────────────────
			cb := uint32(1)
			var dib uint32

			switch {
			case luCur>>(ibit+1)&1 == 0: // 6-bit offset
				dib = (luCur >> (ibit + 2)) & 0x3F
				dib += 0x0001
				ibit += 8
			case luCur>>(ibit+2)&1 == 0: // 9-bit offset
				dib = (luCur >> (ibit + 3)) & 0x1FF
				dib += 0x0041
				ibit += 12
			case luCur>>(ibit+3)&1 == 0: // 12-bit offset
				dib = (luCur >> (ibit + 4)) & 0xFFF
				dib += 0x0241
				ibit += 16
			default: // 20-bit offset (or end-of-stream)
				raw := (luCur >> (ibit + 4)) & 0xFFFFF
				ibit += 24
				if raw == 0xFFFFF {
					goto done // end-of-stream marker
				}
				dib = raw + 0x1241
				cb = 2
			}
			advance()

			// Log-decode match length. Count k leading one-bits; the
			// terminating zero-bit is implicit; then read k data bits.
			k := uint(0)
			for luCur>>(ibit+k)&1 == 1 {
				k++
				if k > 11 {
					return nil, fmt.Errorf("kcdc: length decode overflow (k > 11)")
				}
			}
			// v = (1 << k) + r, where r is the k bits after the terminator.
			r := (luCur >> (ibit + k + 1)) & ((1 << k) - 1)
			cb += (1 << k) + r
			ibit += 2*k + 1
			advance()

			// Byte-by-byte copy (overlap intentional for run-length fills).
			srcOff := len(out) - int(dib)
			if srcOff < 0 {
				return nil, fmt.Errorf("kcdc: back-reference offset %d exceeds output size %d", dib, len(out))
			}
			for i := uint32(0); i < cb; i++ {
				out = append(out, out[srcOff])
				srcOff++
			}
		}
		advance()
	}
done:
	if len(out) != cbDst {
		return nil, fmt.Errorf("kcdc: decompressed %d bytes, expected %d", len(out), cbDst)
	}
	return out, nil
}

// decodeKCD2 implements the KCD2 (kcfmtKauai2 = 1) decompressor.
//
// Token layout: [log-encoded count][type bit: 0=literal run, 1=back-ref].
// End-of-stream is signalled when the log-encoding's k counter exceeds 11,
// which naturally occurs when the decoder reads into the 0xFF tail bytes.
//
// Literal bytes are stored byte-aligned with the last byte split across two
// source bytes. The correct split-byte reconstruction is used here (not the
// buggy C fallback in _FDecode2). The 20-bit offset precedence bug from the
// C fallback is also corrected.
func decodeKCD2(src []byte, cbDst int) ([]byte, error) {
	if err := kcValidateStream(src); err != nil {
		return nil, fmt.Errorf("kcd2: %w", err)
	}

	out := make([]byte, 0, cbDst)

	pos := 5
	luCur := kcReadU32LE(src, pos)
	ibit := uint(0)

	advance := func() {
		pos += int(ibit >> 3)
		luCur = kcReadU32LE(src, pos)
		ibit &= 7
	}

	for {
		// ── Log-decode count ──────────────────────────────────────────
		k := uint(0)
		for luCur>>(ibit+k)&1 == 1 {
			k++
			if k > 11 {
				goto done // end-of-stream: k overflow into 0xFF tail
			}
		}
		// cb = (1 << k) + r
		r := (luCur >> (ibit + k + 1)) & ((1 << k) - 1)
		cb := (1 << k) + int(r)
		ibit += 2*k + 1
		advance()

		// ── Type bit ──────────────────────────────────────────────────
		if luCur>>(ibit)&1 == 0 {
			// ── Literal run ───────────────────────────────────────────
			// After the type bit, the remaining bits of the current byte
			// hold the low (8-ibit) bits of the LAST literal byte.
			// The next (cb-1) full bytes are literal bytes [0..cb-2].
			// The low ibit bits of the following byte are the high ibit
			// bits of the last literal byte.
			ibit++ // skip type bit; ibit is now 1–8

			// Low (8-ibit) bits of last literal from current byte.
			lowPart := byte(luCur>>ibit) & byte((1<<(8-ibit))-1)
			if ibit == 8 {
				lowPart = 0 // no bits remain in current byte
			}

			if cb > 1 {
				// Literal bytes [0..cb-2] are byte-aligned starting at pos-3.
				startByte := pos - 3
				endByte := startByte + cb - 1
				if startByte < 0 || endByte > len(src) {
					return nil, fmt.Errorf("kcd2: literal run out of bounds (pos=%d cb=%d)", pos, cb)
				}
				out = append(out, src[startByte:endByte]...)
				pos += cb
			} else {
				pos++
			}
			luCur = kcReadU32LE(src, pos)

			// High ibit bits of last literal from new current byte.
			var lastByte byte
			if ibit == 8 {
				// Entire last byte comes from the new current byte.
				lastByte = byte(luCur)
			} else {
				highPart := byte(luCur) & byte((1<<ibit)-1)
				lastByte = lowPart | (highPart << (8 - ibit))
			}
			out = append(out, lastByte)
		} else {
			// ── Back-reference ────────────────────────────────────────
			// The log-decoded count is the base match length (cb++
			// after reading the type bit; another cb++ for 20-bit offset).
			cb++
			var dib uint32

			switch {
			case luCur>>(ibit+1)&1 == 0: // 6-bit offset
				dib = (luCur >> (ibit + 2)) & 0x3F
				dib += 0x0001
				ibit += 2 + 6
			case luCur>>(ibit+2)&1 == 0: // 9-bit offset
				dib = (luCur >> (ibit + 3)) & 0x1FF
				dib += 0x0041
				ibit += 3 + 9
			case luCur>>(ibit+3)&1 == 0: // 12-bit offset
				dib = (luCur >> (ibit + 4)) & 0xFFF
				dib += 0x0241
				ibit += 4 + 12
			default: // 20-bit offset — mask before add (fixes C precedence bug)
				dib = ((luCur >> (ibit + 4)) & 0xFFFFF) + 0x1241
				ibit += 4 + 20
				cb++
			}

			srcOff := len(out) - int(dib)
			if srcOff < 0 {
				return nil, fmt.Errorf("kcd2: back-reference offset %d exceeds output size %d", dib, len(out))
			}
			for i := 0; i < cb; i++ {
				out = append(out, out[srcOff])
				srcOff++
			}
		}
		advance()
	}
done:
	if len(out) != cbDst {
		return nil, fmt.Errorf("kcd2: decompressed %d bytes, expected %d", len(out), cbDst)
	}
	return out, nil
}

// kcValidateStream checks the flags byte and the 6-byte 0xFF tail.
func kcValidateStream(src []byte) error {
	if len(src) < 1+kcbTail {
		return fmt.Errorf("stream too short (%d bytes, need at least %d)", len(src), 1+kcbTail)
	}
	if src[0] != 0x00 {
		return fmt.Errorf("non-zero flags byte 0x%02X", src[0])
	}
	tail := src[len(src)-kcbTail:]
	for i, b := range tail {
		if b != 0xFF {
			return fmt.Errorf("bad tail byte [%d] = 0x%02X (expected 0xFF)", i, b)
		}
	}
	return nil
}
