# Actors and Props

Actors and props are the 3D characters and objects the user places and animates in a scene. From the engine's perspective both are instances of the same template (`TMPL`) system — a prop is simply a template with the `ftmplProp` flag set.

This document covers how actor/prop content is structured on disk, and how placed instances in a movie reference that content.

See also:
- [chunky-files.md](chunky-files.md) — which chunky archives hold this data
- [movie.md](movie.md) — `ACTR`, `PATH`, and `GGAE` (the instance side of an actor in a `.3MM` file)
- [sounds.md](sounds.md) — actor sound events (`aetSnd`) and motion-match sounds

---

## Concepts

| Term | Meaning |
|------|---------|
| **Template** (`TMPL`) | A "species" definition: 3D geometry, skeleton, animations, and materials for one actor or prop type. Stored in content files (`.3CN`). |
| **Body** (`BODY`) | A runtime-only object instantiated from a TMPL; holds the live BRender actors (BACTs). Not serialised. |
| **Action** (`ACTN`) | One named animation clip within a template — e.g. "walk", "rest", "talk". Each action has a sequence of cels. |
| **Cel** | One frame of an action. Specifies per-part model indices, transforms, step distance, and optional sound. |
| **Body-part set** (`ibset`) | A group of body parts that share one material/costume slot. Each TMPL has 1–N body-part sets. |
| **Costume** (`cmid`) | A material or custom-material assignment for a body-part set. The user can cycle through costumes per actor. |
| **Actor ID** (`arid`) | A unique integer assigned to each placed actor instance within a movie. Matches the entry in the movie's GST roll-call. |
| **Tag** (`TAG`) | A cross-file reference: `{ sid, pcrf, ctg, cno }`. The `ctg`/`cno` pair identifies the target chunk; `sid` selects the source file from the tag manager. |

---

## Content Files

Actor/prop content is split across two types of chunky file:

| Extension | `kftg*` constant | Role |
|-----------|-----------------|------|
| `.3CN` (`kftgContent`) | `kftgContent` | Content — holds the actual `TMPL`, `ACTN`, `BMDL`, `MTRL`, `CMTL` data |
| `.3TH` (`kftgThumbDesc`) | `kftgThumbDesc` | Thumbnail description — holds browser UI descriptors and thumbnail images |

In the UK retail release the relevant archives are:

| File | Contents |
|------|---------|
| `TMPLS.3CN` | 66 `TMPL` chunks (45 characters, 21 costumes/prop-variants) plus all dependent chunks |
| `ACTOR.3TH` | Thumbnail browser for the 45 character actors |
| `PROP.3TH` | Thumbnail browser for prop actors |
| `ACTRESL.3TH` | Alternate (resolution) actor thumbnail browser |

See [chunky-files.md](chunky-files.md) for a complete inventory.

---

## Template Chunk — `TMPL`

**Tag:** `'TMPL'` (`kctgTmpl`)
**Source:** `inc/tmpl.h`, `src/engine/tmpl.cpp`
**Found in:** `.3CN` content files (e.g. `TMPLS.3CN`)

A `TMPL` chunk is small (16 bytes) — just the `TMPLF` header. All substantive data lives in its children.

### On-disk header — `TMPLF` (16 bytes)

| Offset | Type    | Field      | Description |
|--------|---------|------------|-------------|
| 0      | `short` | `bo`       | Byte-order marker (`kboCur`) |
| 2      | `short` | `osk`      | OS kind |
| 4      | `BRA`   | `xaRest`   | Rest orientation — X Euler angle |
| 6      | `BRA`   | `yaRest`   | Rest orientation — Y Euler angle |
| 8      | `BRA`   | `zaRest`   | Rest orientation — Z Euler angle |
| 10     | `short` | `swPad`    | Padding (aligns `grftmpl` to long boundary) |
| 12     | `ulong` | `grftmpl`  | Template flags (see below) |

BOM: `kbomTmplf = 0x554c0000`

`BRA` is a BRender angle (unsigned 16-bit; full circle = 65536).

### `grftmpl` flags

| Flag | Value | Meaning |
|------|-------|---------|
| `ftmplOnlyCustomCostumes` | `1` | Suppress generic `MTRL` assignment; only `CMTL` costumes are valid |
| `ftmplTdt` | `2` | This template is a 3-D text object (`TDT`) |
| `ftmplProp` | `4` | This template is a prop (non-character actor) |

### Children

Each `TMPL` chunk's children provide its complete 3D definition. Typical child counts from `TMPLS.3CN` (character actors have 33 ACTNs and dozens of BRender models):

| Tag | Description | Typical count |
|-----|-------------|---------------|
| `ACTN` | Action / animation clip (see below) | 9–33 |
| `BMDL` | BRender model for one body part | 2–273 |
| `CMTL` | Custom material (costume) | 1–20 |
| `GGCM` | GOK command group (UI scripting) | 1 |
| `GLBS` | Body-part set list (GL of `ibset` shorts) | 1 |
| `GLPI` | Body-part parent hierarchy (GL of parent-index shorts; root parts store `ivNil` = −1) | 1 |

The `CHID` of each `BMDL` child is the `chidModl` referenced from `CPS` records in the action cels.

---

## Action Chunk — `ACTN`

**Tag:** `'ACTN'` (`kctgActn`)
**Source:** `inc/tmpl.h`, `src/engine/tmpl.cpp`
**Child of:** `TMPL`

One `ACTN` chunk per named animation clip (e.g. "walk", "rest", "talk"). Its data contains the full animation sequence as a sequence of *cels*.

### On-disk header — `ACTNF` (8 bytes)

| Offset | Type   | Field      | Description |
|--------|--------|------------|-------------|
| 0      | `short`| `bo`       | Byte-order marker |
| 2      | `short`| `osk`      | OS kind |
| 4      | `long` | `grfactn`  | Action flags |

BOM: `kbomActnf = 0x5c000000`

### `grfactn` flags

| Flag | Value | Meaning |
|------|-------|---------|
| `factnRotateX` | `1` | Actor auto-rotates around X when path-following |
| `factnRotateY` | `2` | Actor auto-rotates around Y when path-following |
| `factnRotateZ` | `4` | Actor auto-rotates around Z when path-following |
| `factnStatic`  | `8` | Stationary action (actor does not step along path) |

### Children

| Tag | Description |
|-----|-------------|
| `GGCL` | Cel group — a `GG` (General Group) where each entry has a `CEL` fixed part (8 bytes) plus a variable `CPS[]` array |
| `GLXF` | Transform list — a `GL` of `BMAT34` (48-byte 3×4 BRender matrices) indexed by `CPS.imat34` |
| `GLMS` | Motion-match sound list — a `GL` of sound-binding records; see [sounds.md](sounds.md) |

### `CEL` fixed part (8 bytes in `GGCL`)

| Offset | Type   | Field      | Description |
|--------|--------|------------|-------------|
| 0      | `CHID` | `chidSnd`  | Sound to play at this cel (CHID of a child of ACTN); `0` = no sound |
| 4      | `BRS`  | `dwr`      | Step distance from the previous cel (BRender scalar, 16.16 fixed-point) |

BOM: `kbomCel = 0xF0000000`

### `CPS` variable part (4 bytes per body-part entry in `GGCL`)

One `CPS` record per body part per cel:

| Offset | Type    | Field       | Description |
|--------|---------|-------------|-------------|
| 0      | `short` | `chidModl`  | CHID of the `BMDL` child of `TMPL` to use for this part in this cel |
| 2      | `short` | `imat34`    | Index into `GLXF`'s `GL` of `BMAT34` transforms |

BOM: `kbomCps = 0x50000000`

---

## Material Chunks — `CMTL` and `MTRL`

**Tags:** `'CMTL'` (`kctgCmtl`), `'MTRL'` (`kctgMtrl`)
**Source:** `inc/mtrl.h`, `src/engine/mtrl.cpp`
**Found in:** `.3CN` content files (e.g. `TMPLS.3CN`)

A `TMPL` chunk holds one or more `CMTL` (custom material / costume) children, each of which covers exactly one body-part set. Multiple `CMTL` chunks can cover the same `ibset` — they represent alternate costume slots. Each `CMTL` in turn contains one `MTRL` (material) child per body part in that set.

### CMTL chunk — `CMTLF` (8 bytes)

| Offset | Type    | Field   | Description |
|--------|---------|---------|-------------|
| 0      | `short` | `bo`    | Byte-order marker |
| 2      | `short` | `osk`   | OS kind |
| 4      | `long`  | `ibset` | Index of the body-part set this CMTL applies to |

BOM: `kbomCmtlf = 0x5c000000`

**CMTL children:**

| Tag | CHID | Description |
|-----|------|-------------|
| `MTRL` | 0, 1, 2, … | One material per body part in the set; CHID = position within the ibset |
| `BMDL` | 0, 1, 2, … | Optional replacement model per body part (same CHID as its MTRL) |

### MTRL chunk — `MTRLF` (20 bytes)

| Offset | Type     | Field          | Description |
|--------|----------|----------------|-------------|
| 0      | `short`  | `bo`           | Byte-order marker |
| 2      | `short`  | `osk`          | OS kind |
| 4      | `ulong`  | `brc`          | RGB color: `r=(brc>>16)&0xFF`, `g=(brc>>8)&0xFF`, `b=brc&0xFF` |
| 8      | `ushort` | `brufKa`       | Ambient coefficient (0.16 unsigned fixed-point; unused by Go renderer) |
| 10     | `ushort` | `brufKd`       | Diffuse coefficient (0.16 unsigned fixed-point; unused by Go renderer) |
| 12     | `ushort` | `brufKs`       | Specular coefficient (always 0 in 3DMM) |
| 14     | `byte`   | `bIndexBase`   | Palette base index for solid-color indexed rendering |
| 15     | `byte`   | `cIndexRange`  | Palette color range count |
| 16     | `long`   | `rPower`       | Specular exponent (15.16 BRS fixed-point; unused by Go renderer) |

BOM: `kbomMtrlf = 0x5D530000`

A textured MTRL has a `TMAP` child at CHID=0; a solid-color MTRL has no `TMAP` child. The `brc` color is valid in both cases, though for textured materials the engine uses the texture exclusively.

---

## Texture Map Chunk — `TMAP`

**Tag:** `'TMAP'` (`kctgTmap`)
**Source:** `bren/inc/tmap.h`, `bren/tmap.cpp`
**Child of:** `MTRL` (CHID=0 when present)

A `TMAP` chunk stores a rectangular bitmap of 8-bit palette indices used as a texture map. In 3DMM all textures use the `BR_PMT_INDEX_8` pixel format — each pixel is one byte whose value is a palette index.

### On-disk layout — `TMAPF` header (20 bytes) + pixel data

| Offset | Type    | Field       | Description |
|--------|---------|-------------|-------------|
| 0      | `short` | `bo`        | Byte-order marker |
| 2      | `short` | `osk`       | OS kind |
| 4      | `short` | `cbRow`     | Byte stride per row (≥ `dxp` for 8-bit pixels) |
| 6      | `byte`  | `type`      | Pixel format (`BR_PMT_INDEX_8 = 0x05` in all 3DMM textures) |
| 7      | `byte`  | `grftmap`   | Flags (typically `BR_PMF_LINEAR = 0x02`) |
| 8      | `short` | `xpLeft`    | Left origin of visible region |
| 10     | `short` | `ypTop`     | Top origin of visible region |
| 12     | `short` | `dxp`       | Width in pixels |
| 14     | `short` | `dyp`       | Height in pixels |
| 16     | `short` | `xpOrigin`  | Graphics origin X |
| 18     | `short` | `ypOrigin`  | Graphics origin Y |

Pixel data follows immediately: `cbRow × dyp` bytes. Pixel `(x, y)` is at byte `y*cbRow + x` and its value is a palette index 0–255.

BOM: `kbomTmapf = 0x54555000`

**Palette mapping in Go:** The Go renderer treats the TMAP pixel value directly as an index into the `GLCR` palette (loaded via `FindGLCR`). This ignores the shade table used by the original BRender pipeline for lighting, which is correct given that the Go renderer does not implement lighting.

**Texture UV coordinates** are stored in each `BMDL` vertex at bytes 12–19 as two `int32` BRS (15.16 fixed-point) values `(U, V)`. Values are in the range `[0, 1]`; the Go renderer wraps them with `u -= floor(u)` before sampling.

**Shade table** (`_ptmapShadeTable` in `MTRL`): a separate `TMAP` chunk containing a 2D lookup table mapping `(lighting_level, texture_index) → palette_index`. It is only needed for lit rendering and is not used by the Go renderer.

---

## Thumbnail Browser Files (`.3TH`)

Thumbnail files drive the actor/prop picker browser in the Studio UI. Each `.3TH` file pairs UI descriptors with thumbnail images and cross-file references to the content in the corresponding `.3CN`.

### Chunk hierarchy in a `.3TH` file

```
TMTH  cno=X   (root; payload = TFC referencing TMPL cno=X in TMPLS.3CN)
└─ GOKD cno=Y   (GOK descriptor — UI script/layout; chid=0)
   └─ MBMP cno=Z   (thumbnail image; chid=0x10000)
```

The TMTH CNO matches the TMPL CNO it references in the content file (e.g. TMTH `0x2010` → TMPL `0x2010` in `TMPLS.3CN`).

### `TMTH` — Template Thumbnail

**Tag:** `'TMTH'` (`kctgTmth`) — characters; `'PRTH'` (`kctgPrth`) for props
**Source:** `inc/browser.h`, `src/studio/browser.cpp`

Payload is a `TFC` struct (12 bytes):

### `TFC` — Thumbnail File Content reference (12 bytes)

| Offset | Type    | Field  | Description |
|--------|---------|--------|-------------|
| 0      | `short` | `bo`   | Byte-order marker |
| 2      | `short` | `osk`  | OS kind |
| 4      | `CTG`   | `ctg`  | Chunk type of the target content (`'TMPL'`) |
| 8      | `CNO`   | `cno`  | Chunk number of the target `TMPL` in the `.3CN` file |

BOM: `kbomTfc = 0x5f000000`

The browser code (`BRWR::_FInitRL` in `src/studio/browser.cpp`) enumerates all `TMTH` (or `PRTH`) chunks in the `.3TH` file, reads the `TFC` to get the `(ctg, cno)` of the target `TMPL`, then maps that to the `GOKD` child for display.

### `GOKD` — Game Object Kit Descriptor

**Tag:** `'GOKD'` (`kctgGokd`)

A 52-byte chunk holding a GOK (Game Object Kit) descriptor — a script-driven UI element. The browser uses GOKDs to render actor thumbnail frames with hover/select states. One GOKD per actor/prop entry in the browser.

### `MBMP` — Thumbnail Image

The thumbnail preview image, stored as a Masked Bitmap (8-bit indexed colour with alpha mask). Rendered at ≈64×64 pixels. See [chunky-files.md](chunky-files.md) for the `MBMP` format.

---

## Actor Instances in Movies

When an actor is placed in a scene, the movie stores an `ACTR` chunk (child of `SCEN`) that references the template by `TAG`. The full instance format is documented in [movie.md](movie.md); the key link to this document is the `tagTmpl` field:

```
ACTR.tagTmpl.ctg = 'TMPL'
ACTR.tagTmpl.cno = <TMPL CNO in TMPLS.3CN>
```

The tag manager (`src/engine/tagman.cpp`) resolves this tag at load time by matching the source ID (`sid`) to the appropriate `.3CN` content file.

### Actor identity

Each placed actor instance has an `arid` (actor ID) that links it to:
1. The `ACTR` chunk's `ACTF.arid` field
2. The corresponding `MACTR` entry in the movie's `GST` roll-call (child of `MVIE` at `chid=0`)

The `MACTR` extra-data in the GST also stores `tagTmpl` and the `grfbrws` flags that control whether the actor appears in the prop browser (`fbrwsProp`) or 3-D text browser (`fbrwsTdt`).

---

## Source Files

| File | Covers |
|------|--------|
| `inc/tmpl.h` | `TMPL`, `ACTN` classes; `TMPLF`, `ACTNF`, `CEL`, `CPS` on-disk structs; `grftmpl` / `grfactn` flags |
| `src/engine/tmpl.cpp` | `TMPL` and `ACTN` read/write; body-part set, costume, and action management |
| `inc/body.h` | `BODY` and `COST` classes (runtime, not serialised) |
| `src/engine/body.cpp` | `BODY` instantiation from a `TMPL`; BRender model/material attachment |
| `inc/browser.h` | `TFC` struct; `kbomTfc`; browser display classes |
| `src/studio/browser.cpp` | `BRWR` — thumbnail browser; enumerates `TMTH`/`PRTH` and maps to `GOKD`/`MBMP` |
| `inc/soc.h` | `kctgTmpl`, `kctgActn`, `kctgTmth`, `kctgPrth`, `kftgContent`, `kftgThumbDesc` constants |
| `src/engine/tagman.cpp` | `TAGM` — resolves TAG references to content files |
| `go/actor.go` | Go parser for `ACTR`, `PATH`, `GGAE` (instance side) |
| `go/mtrl.go` | Go parsers for `CMTL`, `MTRL`, `TMAP`; `Material`, `TexMap`, `LoadedCMTL` structs |
| `go/tmpl.go` | `LoadedTemplate.Costumes` / `IBSetPartIndex`; CMTL/MTRL/TMAP loading in `LoadTemplate` |
| `go/bmdl.go` | `BRVertex.U`, `BRVertex.V`; UV extraction in `ParseBMDL` |
| `go/actor_render.go` | Material-aware `RenderTemplate`; UV-interpolated `fillTriangle`; `RenderParams.Palette` |
