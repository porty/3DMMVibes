# Kauai Codec Technical Specification

Source: `kauai/src/codkauai.cpp`, `kauai/src/codkpri.h`

## Overview

The Kauai framework defines two compressed stream formats, both implemented in `KCDC::FConvert`:

| Format ID | Constant | Description |
|---|---|---|
| `kcfmtKauai` | 0 | **KCDC** — primary LZ format |
| `kcfmtKauai2` | 1 | **KCD2** — variant with run-grouped literals |

Both formats share the same offset constants, tail structure, and logarithmic length encoding. KCDC is the simpler and more common of the two.

---

## Common Structure

Every compressed stream is laid out as:

```
[flags byte: 0x00] [bit stream] [tail: 6 × 0xFF]
```

- **Flags byte**: Always `0x00`. Reserved for future extension.
- **Bit stream**: Described per format below.
- **Tail**: Exactly 6 bytes of `0xFF`. The decoder verifies this before decompressing; it guarantees the decoder will not read past the end of the source buffer.

---

## Bit-Stream Conventions

Bits are packed **least-significant-bit first** within each byte. The decoder maintains:

- `pbSrc`: source byte pointer, positioned 4 bytes *past* the start of the current window.
- `luCur`: a 32-bit unsigned integer = `*(uint32_t*)(pbSrc - 4)`, a little-endian load. This provides a sliding view of 4 bytes at once.
- `ibit`: bit index into `luCur` (0 = LSB). Ranges 0–7 between advance operations.

**Reading n bits from position ibit:**
```
value = (luCur >> ibit) & ((1 << n) - 1)
ibit += n
```

**Advancing** after accumulating bits (call this periodically to keep ibit < 8):
```
pbSrc += (ibit >> 3)
luCur  = *(uint32_t*)(pbSrc - 4)
ibit  &= 7
```

At stream start, skip the flags byte:
```
pbSrc += 5     // 1 flags byte + 4 for initial luCur load
luCur  = *(uint32_t*)(pbSrc - 4)
ibit   = 0
```

---

## Logarithmic Length Encoding

Both formats use an Elias-gamma–style prefix code for lengths. The value `v` must satisfy `1 ≤ v ≤ 4096`.

**Encoding `v`:**
1. Let `k = floor(log2(v))` — the index of the highest set bit.
2. Write `k` one-bits.
3. Write one zero-bit (terminator).
4. Write the low `k` bits of `v` (i.e., `v & ((1 << k) - 1)`), LSB first.

Total bits written: `2k + 1`.

**Decoding:**
1. Count consecutive one-bits; let the count be `k`. (If `k > 11`, signal end-of-stream or error per format rules.)
2. The zero-bit at position `ibit + k` is the terminator — skip it.
3. Read the next `k` bits as an unsigned integer `r`.
4. `v = (1 << k) + r`

Advance: `ibit += 2*k + 1`.

**Examples:**

| v | Bit sequence (LSB first) | Total bits |
|---|---|---|
| 1 | `0` | 1 |
| 2 | `1 0 0` | 3 |
| 3 | `1 0 1` | 3 |
| 4 | `1 1 0 0 0` | 5 |
| 5 | `1 1 0 1 0` | 5 |
| 8 | `1 1 1 0 0 0 0` | 7 |
| 4095 | `1×11 0 1×11` | 23 |
| 4096 | `1×11 0 0×11` | 23 |

---

## Offset Encoding (shared by KCDC and KCD2)

Four selector-bit patterns select one of four offset ranges. The pattern and raw offset are written as a single unit, LSB first:

| Bits (LSB first) | Raw field | Offset formula | Offset range |
|---|---|---|---|
| `1 0` + 6-bit raw | 6 bits | `raw + 0x0001` | 1–64 |
| `1 1 0` + 9-bit raw | 9 bits | `raw + 0x0041` | 65–576 |
| `1 1 1 0` + 12-bit raw | 12 bits | `raw + 0x0241` | 577–4672 |
| `1 1 1 1` + 20-bit raw | 20 bits | see below | 4673–1,053,247 |

**20-bit special case:**
- If the 20-bit raw value is `0xFFFFF` (all bits set): **end-of-stream** marker (KCDC only).
- Otherwise: `offset = raw + 0x1241`.

**Constants:**
```
kdibMinKcdc0 / kdibMinKcd2_0 = 0x000001    (min offset, 6-bit)
kdibMinKcdc1 / kdibMinKcd2_1 = 0x000041    (min offset, 9-bit)
kdibMinKcdc2 / kdibMinKcd2_2 = 0x000241    (min offset, 12-bit)
kdibMinKcdc3 / kdibMinKcd2_3 = 0x001241    (min offset, 20-bit)
kdibMinKcdc4 / kdibMinKcd2_4 = 0x101240    (exclusive upper limit, 20-bit)
```

---

## KCDC Format (`kcfmtKauai`)

KCDC is a straightforward LZ77 codec. The bit stream after the flags byte is a sequence of tokens. Each token is either a literal byte or a back-reference.

### Literal Token (9 bits)

Bit 0 (the first bit of the token) = `0` signals a literal.

```
bit 0    : 0  (literal marker)
bits 1–8 : raw byte value, LSB first
```

Consume 9 bits total: `ibit += 9`.

Output the byte `(luCur >> (ibit_before + 1)) & 0xFF`.

### Back-Reference Token

Bit 0 = `1` signals a back-reference. The token then encodes an offset and a match length.

**Step 1 — Read the offset** using the offset encoding table above (including its selector prefix bits). Note the match length base:
- 6-bit, 9-bit, or 12-bit offset: match length base = 1
- 20-bit offset (non-terminal): match length base = 2

After reading the offset bits: `advance(ibit >> 3); ibit &= 7`.

**Step 2 — Read the match length** using the log encoding above.
`match_length = base + log_decode_value`

Advance again: after consuming `2*k + 1` bits.

**Step 3 — Copy bytes:**
Copy `match_length` bytes from `output[current_pos - offset]` to `output[current_pos]`, byte by byte (allowing overlapping copies for run-length effects).

### End of Stream

A 20-bit offset with raw value `0xFFFFF` terminates the stream. After detecting this, output is complete.

### Minimum Match Lengths

| Offset type | Minimum match |
|---|---|
| 6-bit (offset ≤ 64) | 2 |
| 9-bit (offset ≤ 576) | 2 |
| 12-bit (offset ≤ 4672) | 2 |
| 20-bit (offset > 4672) | 3 |

### KCDC Decoder Pseudocode

```python
pbSrc = compressed_data + 5   # skip 1 flags byte + 4 for initial load
luCur = u32le(pbSrc - 4)
ibit  = 0

def bit(i):    return (luCur >> i) & 1
def bits(i,n): return (luCur >> i) & ((1 << n) - 1)
def advance():
    global pbSrc, luCur, ibit
    pbSrc += ibit >> 3
    luCur  = u32le(pbSrc - 4)
    ibit  &= 7

while True:
    if bit(ibit) == 0:                          # literal
        out.write(bits(ibit + 1, 8))
        ibit += 9
    else:
        cb = 1
        if bit(ibit + 1) == 0:                  # 6-bit offset
            dib  = bits(ibit + 2, 6) + 0x0001
            ibit += 8
        elif bit(ibit + 2) == 0:                # 9-bit offset
            dib  = bits(ibit + 3, 9) + 0x0041
            ibit += 12
        elif bit(ibit + 3) == 0:                # 12-bit offset
            dib  = bits(ibit + 4, 12) + 0x0241
            ibit += 16
        else:                                    # 20-bit offset
            raw  = bits(ibit + 4, 20)
            ibit += 24
            if raw == 0xFFFFF: break             # end of stream
            dib  = raw + 0x1241
            cb   = 2
        advance()

        # log-decode the length
        k = 0
        while bit(ibit + k) == 1:
            k += 1
            assert k <= 11
        cb += (1 << k) + bits(ibit + k + 1, k)
        ibit += 2 * k + 1
        advance()

        # copy match (byte by byte; overlap is intentional)
        src = len(out) - dib
        for _ in range(cb):
            out.write(out[src]); src += 1

    advance()
```

### KCDC Encoding Notes

The encoder uses a linked-list hash on 2-byte keys (`byte[i] << 8 | byte[i+1]`) to find back-reference candidates. It performs a greedy search for the longest match at each position, with heuristics to prefer shorter offsets when the length improvement is marginal. The encoder is noted as unoptimized (slow but correct).

---

## KCD2 Format (`kcfmtKauai2`)

KCD2 restructures the token layout: instead of encoding one literal at a time, it groups consecutive literal bytes into runs. The key structural difference is that **length comes before type**, and literals are stored as raw bytes (byte-aligned) rather than bit-by-bit.

### Token Structure

Each token begins with a log-encoded count, followed by a type bit:

```
[log-encoded count: k ones, 0, k bits]  [type bit: 0=literal, 1=back-ref]
```

**End of stream** is signaled when the log-encoding's `k` exceeds `kcbitMaxLenKcd2 = 11`. This naturally occurs as the decoder reads into the `0xFF` tail bytes.

After decoding `cb = log_decode_value` and the type bit:

### Literal Run (type bit = 0)

The run contains `cb` raw bytes.

After the type bit, let `p = ibit` (the current bit offset within the current byte, 1–8).

The literal bytes are stored in a split-byte layout:
1. Bits `p..7` of the **current** source byte (the byte containing the type bit): the **low `8−p` bits** of the **last** literal byte of the run (`byte[cb-1]`).
2. The next `cb−1` bytes (byte-aligned): literal bytes `0` through `cb−2`, in order.
3. Bits `0..p−1` of the byte immediately following: the **high `p` bits** of `byte[cb-1]`.

To reconstruct `byte[cb-1]`:
```
low  = (current_source_byte >> p) & ((1 << (8-p)) - 1)
high = next_byte_after_run & ((1 << p) - 1)
byte[cb-1] = low | (high << (8-p))
```

The source pointer advances by `cb` bytes after reading the literal run.

**Implementation note**: The reference decoder in `_FDecode2` contains a bug in the last-byte reconstruction. The expression `(byte)~(luCur & mask) | (new_luCur & mask)` does not correctly recover the low bits of the last byte. The correct reconstruction is:
```c
bT = (byte)(luCur >> ibit) | ((new_luCur & mask) << (8 - ibit));
```
where `mask = (1 << ibit) - 1` and `ibit` is the bit position after the type bit.

### Back-Reference (type bit = 1)

The log-decoded count gives the base match length. After `cb++` (incrementing by 1 to account for the type bit), the offset is read using the same encoding as KCDC.

For the 20-bit offset case, an additional `cb++` occurs (minimum match = 3). For all other offsets, minimum match = 2.

The offset field follows the same encoding table as KCDC. The 20-bit offset does **not** have an end-of-stream marker in KCD2; end-of-stream is detected via the log-encoding overflow instead.

**Operator precedence bug in reference decoder**: The 20-bit KCD2 offset is decoded as:
```c
// BUG — '+' has higher precedence than '&':
dib = (luCur >> (ibit + 4)) & ((1 << kcbitKcd2_3) - 1) + kdibMinKcd2_3;
// Correct intent:
dib = ((luCur >> (ibit + 4)) & ((1 << kcbitKcd2_3) - 1)) + kdibMinKcd2_3;
```
Correct implementations should apply the mask before adding the minimum.

After the offset, copy `cb` bytes from `output[current_pos - dib]`, byte by byte.

---

## Constants Reference

```c
// Tail
kcbTailKcdc = kcbTailKcd2 = 6       // bytes of 0xFF at end of stream

// Length encoding
kcbMaxLenKcdc = kcbMaxLenKcd2 = 4096  // maximum match/run length
kcbitMaxLenKcdc = kcbitMaxLenKcd2 = 11 // max k in log encoding before EOS/error

// Offset field widths
kcbitKcdc0 = kcbitKcd2_0 = 6
kcbitKcdc1 = kcbitKcd2_1 = 9
kcbitKcdc2 = kcbitKcd2_2 = 12
kcbitKcdc3 = kcbitKcd2_3 = 20

// Offset range minimums (add to raw field value)
kdibMinKcdc0 = kdibMinKcd2_0 = 0x000001   // 6-bit: offsets 1–64
kdibMinKcdc1 = kdibMinKcd2_1 = 0x000041   // 9-bit: offsets 65–576
kdibMinKcdc2 = kdibMinKcd2_2 = 0x000241   // 12-bit: offsets 577–4672
kdibMinKcdc3 = kdibMinKcd2_3 = 0x001241   // 20-bit: offsets 4673–1,053,247
kdibMinKcdc4 = kdibMinKcd2_4 = 0x101240   // exclusive upper limit (not used at runtime)
```

---

## Worked Example (KCDC)

Input: 37 consecutive `'a'` bytes (0x61).

**Compressed bit stream** (LSB first within each byte):

```
Byte 0 (flags): 0x00

Token 1 — literal 'a':
  0          (literal marker)
  10000110   (0x61, LSB first)

Token 2 — back-reference (offset=1, length=36):
  1          (back-reference marker)
  0          (6-bit offset selector)
  000000     (raw offset 0 → offset = 0 + 1 = 1)
  11111      (log-encode(35): k=5 ones)
  0          (terminator)
  00011      (low 5 bits of 35 = 3)

Token 3 — end of stream:
  1 1 1 1    (20-bit offset selector)
  11111111111111111111  (raw = 0xFFFFF → EOS)

[padding to byte boundary]
[6 bytes of 0xFF tail]
```

Uncompressed size: 37 bytes = 296 bits. Compressed stream: 9 + 1+2+6+5+1+5 + 4+20 = ~53 bits + padding + 48 tail bits.

---

## Implementation Checklist

- [ ] Verify 6-byte `0xFF` tail before decompressing.
- [ ] Reject streams where the flags byte is non-zero.
- [ ] Use byte-by-byte copy for back-references (not `memcpy`) to support overlapping runs.
- [ ] On KCDC decoding: detect end-of-stream via 20-bit offset raw value `0xFFFFF`.
- [ ] On KCD2 decoding: detect end-of-stream via log-encoding overflow (`k > 11`).
- [ ] On KCD2 20-bit offset: apply the mask **before** adding `kdibMinKcd2_3`.
- [ ] On KCD2 literal last-byte: reconstruct from split bits, not the reference formula.
- [ ] Output buffer bounds-checking: verify `pbDst + cb <= pbLimDst` and `pbDst - dib >= pvDst` before each copy.
