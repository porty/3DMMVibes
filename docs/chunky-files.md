# Chunky Files

A chunky file is a typed-chunk archive used throughout 3D Movie Maker to store all game assets. The Go parser is in `../go/chunky.go`; the CLI tool at `../go/3dmm-go` can list and extract chunks.

For the movie file format (`.3MM`) see [movie.md](movie.md).
For actors and props see [actors-and-props.md](actors-and-props.md).
For sound chunks see [sounds.md](sounds.md).

---

## File Extensions

| Extension | `kftg*` constant | Role |
|-----------|-----------------|------|
| `.CHK` / `.3MM` / `.3TP` | `kftgChunky` / `kftg3mm` / `kftgSocTemp` | Game data and movie save files |
| `.3CN` | `kftgContent` (`MacWin('3con','3CN')`) | Content library — actual 3D assets (models, materials, animations) |
| `.3TH` | `kftgThumbDesc` (`MacWin('3thd','3TH')`) | Thumbnail description — browser UI assets (thumbnails + content references) |

All constants are defined in `inc/soc.h`.

---

## Chunky Archives in the UK Release

The files below are found in `chunks/uk/`. Use `3dmm-go chunky list <file>` to inspect them. Note: put flags before the filename (e.g. `--ctg TMPL <file>`, not `<file> --ctg TMPL`).

### `TMPLS.3CN` — Actor & Prop Templates

**Format:** `.3CN` (content)
**Creator:** `CHMR` · version 5/4
**Chunk count:** ~17 322

The primary content library for all character actors and props. Every 3D template (`TMPL`) lives here along with all dependent chunks.

```
TMPLS.3CN
├─ TMPL  ×66  — template headers (16 bytes each; flags distinguish actors from props)
│  ├─ ACTN  ×9–33   — animation clips
│  │  ├─ GGCL       — cel sequence (GG of CEL + CPS[] per cel)
│  │  ├─ GLXF       — transform matrices (GL of BMAT34)
│  │  └─ GLMS       — motion-match sound bindings
│  ├─ BMDL  ×2–273  — BRender 3D models (one per body part / cel variant)
│  ├─ CMTL  ×1–20   — custom material (costume) definitions
│  ├─ GGCM          — GOK command group (UI scripting)
│  ├─ GLBS          — body-part set list
│  └─ GLPI          — global palette / part info
├─ TMAP  ×1 604  — texture maps
├─ TXXF  ×2 281  — texture transform lists
└─ MTRL  ×4 189  — standard materials
```

CNO ranges:
- `0x1010–0x1025` (21 TMPLs) — costume variants / prop shapes
- `0x2010–0x203C` (45 TMPLs) — the 45 character actors shown in the actor browser

See [actors-and-props.md](actors-and-props.md) for the `TMPL`, `ACTN`, `CEL`, and `CPS` on-disk formats.

---

### `ACTOR.3TH` — Actor Thumbnail Browser

**Format:** `.3TH` (thumbnail description)
**Creator:** `CHMP` · version 4/4
**Chunk count:** 135

Drives the actor picker browser in the Studio. Contains one entry per character actor (45 total).

```
ACTOR.3TH
└─ TMTH  ×45  cno=0x2010…0x203C  (content reference; TFC payload → TMPL in TMPLS.3CN)
   └─ GOKD  ×45                   (GOK UI descriptor; 52 bytes)
      └─ MBMP  ×45                (thumbnail image; ~3 000–3 600 compressed bytes)
```

The TMTH CNO directly matches the target TMPL CNO in `TMPLS.3CN` (e.g. TMTH `0x2010` → TMPL `0x2010`). The `TFC` payload (12 bytes) stores the `(ctg='TMPL', cno)` pair; see [actors-and-props.md](actors-and-props.md) for its layout.

---

### `ACTRESL.3TH` — Actor Resolution Thumbnail Browser

**Format:** `.3TH` (thumbnail description)
**Creator:** `CHMP` · version 5/4
**Chunk count:** 45

An alternate actor browser used at a different resolution. Contains 45 `GOKD` root chunks (no TMTH layer); otherwise parallels `ACTOR.3TH`.

---

### `PROP.3TH` — Prop Thumbnail Browser

**Format:** `.3TH` (thumbnail description)

Drives the prop picker browser. Structure mirrors `ACTOR.3TH` but uses `PRTH` (`kctgPrth`) instead of `TMTH` as the root chunk type, and references `TMPL` chunks with the `ftmplProp` flag set in `TMPLS.3CN`.

---

### `BKGDS.3CN` — Background Scenes (Content)

**Format:** `.3CN` (content)

Holds all background scene definitions. Key chunk types:

| Tag | Description |
|-----|-------------|
| `BKGD` | Background definition header; lists camera angles |
| `CAM ` | Camera matrix and field of view for one angle |
| `MBMP` | Pre-rendered background image for one camera angle |
| `ZBMP` | Z-buffer for one camera angle (used for actor occlusion) |
| `GLLT` | Light list for the scene |
| `TILE` | Tiled background texture |

---

### `BKGDS.3TH` — Background Thumbnail Browser

**Format:** `.3TH` (thumbnail description)

Thumbnail browser for background selection. Uses `BKTH` (`kctgBkth`) as the root chunk type, referencing `BKGD` chunks in `BKGDS.3CN`.

---

### `MTRLS.3CN` — Materials (Content)

**Format:** `.3CN` (content)

Holds standard `MTRL` and custom `CMTL` material definitions used as actor costumes.

---

### `MTRL.3TH` — Material Thumbnail Browser

**Format:** `.3TH` (thumbnail description)

Thumbnail browser for the costume/material picker. Uses `MTTH` (`kctgMtth`) and `CMTH` (`kctgCmth`) root chunk types.

---

### `SNDS.3CN` / `SNDS.3TH` — Sounds

**Format:** `.3CN` (content) + `.3TH` (thumbnail/browser)

Holds `MSND` wrappers and raw `WAVE`/`MIDS` audio data for the sound picker. `MSND` chunk format is documented in [sounds.md](sounds.md).

Thumbnail types: `SVTH` (voice), `SFTH` (SFX), `SMTH` (MIDI).

---

### `TDFS.3CN` — 3-D Font Templates

**Format:** `.3CN` (content)

Holds `TDF` (3-D font definition) chunks used by 3-D text objects. `TMPL` chunks with `ftmplTdt` set reference these.

---

### `3DMOVIE.CHK` — Main Game Chunky

**Format:** `.CHK`

The primary game script and resource file. Contains `GOKD` descriptors and scripts for all Studio UI elements.

---

### `STUDIO.CHK` — Studio UI

**Format:** `.CHK`

Studio-mode UI resources.

---

### `SHARED.CHK` / `SHARECD.CHK` — Shared Resources

**Format:** `.CHK`

Resources shared across all modes (lobby, studio, building). `SHARECD` is the CD-resident counterpart.

---

### `BUILDING.CHK` / `BLDGHD.CHK` — Building Mode

**Format:** `.CHK`

Resources for the Building (project/movie management) mode.

---

### `HELP.CHK` / `HELPAUD.CHK` — Help System

**Format:** `.CHK`

Help text and audio for the in-game help system.

---

## GG Index and CRP Format

The chunk index at the end of every chunky file is a General Group (GG). Its on-disk layout:

```
[GGF header: 20 bytes]
  bo       int16   byte-order marker (0x0001 = LE)
  osk      int16   OS/encoding kind
  ilocMac  int32   total slot count (including deleted)
  bvMac    int32   byte size of variable-data blob
  clocFree int32   free-list head (-1 = none)
  cbFixed  int32   fixed-part size per entry (20 = CRPSM, 32 = CRPBG)

[variable-data blob: bvMac bytes]
  Each live slot occupies one contiguous region:
    [fixed CRP part]
    [ckid × 12 bytes — KID child references: CTG uint32, CNO uint32, CHID uint32]
    [STN chunk name (see below)]

[LOC array: ilocMac × 8 bytes]
  Each entry: bv int32, cb int32
  bv = -1 → deleted/free slot; otherwise byte offset into variable-data blob
```

**CRPSM** (20 bytes, `cbFixed=20` — used by 3DMMForever files, version ≥ 4):

| Offset | Field | Description |
|--------|-------|-------------|
| 0 | CTG uint32 | chunk type tag |
| 4 | CNO uint32 | chunk number |
| 8 | FP int32 | file offset of chunk data |
| 12 | LuGrfcrpCb uint32 | high 24 bits = data size (cb); low 8 bits = grfcrp flags |
| 16 | CKid uint16 | number of child-chunk KID entries |
| 18 | CCrpRef uint16 | number of parent references |

**CRPBG** (32 bytes, `cbFixed=32` — used by original 3DMM files):

| Offset | Field | Description |
|--------|-------|-------------|
| 0 | CTG uint32 | |
| 4 | CNO uint32 | |
| 8 | FP int32 | file offset |
| 12 | Cb int32 | data size |
| 16 | CKid int32 | child count |
| 20 | CCrpRef int32 | parent count |
| 24 | RTI int32 | runtime ID (not meaningful on disk) |
| 28 | Grfcrp uint32 | flags bitmask (v≥4) or four flag bytes (v<4) |

**grfcrp flag bits:**

| Bit | Constant | Meaning |
|-----|----------|---------|
| 0x01 | `FcrpOnExtra` | data on companion file, not main `.chk` |
| 0x02 | `FcrpLoner` | chunk may exist without a parent |
| 0x04 | `FcrpPacked` | data compressed with Kauai codec |
| 0x10 | `FcrpForest` | data is an embedded chunk forest |

---

## STN — Chunk Name String Format

Chunk names in the GG index are stored as Kauai `STN` objects. The on-disk layout (written by `STN::GetData`/`STN::FRead` in `kauai/src/utilstr.cpp`):

```
osk   int16   OS/encoding kind
cch   byte    string length (single-byte encodings)
chars [cch]   string content (no BOM)
NUL   byte    null terminator
```

The `osk` values that appear in practice:

| osk | Constant | Encoding | Length field |
|-----|----------|----------|-------------|
| `0x0303` | `koskSbWin` | Windows single-byte (Win-1252) | 1-byte `cch` |
| `0x0101` | `koskSbMac` | Mac single-byte | 1-byte `cch` |
| `0x0505` | `koskUniWin` | UTF-16 LE | 2-byte `cch` (code units) |
| `0x0404` | `koskUniMac` | UTF-16 BE | 2-byte `cch` (code units) |

All 3DMMForever content files use `koskSbWin = 0x0303`. The total byte size of an STN with `n` characters is `2 + 1 + n + 1 = n + 4`.

> **Note:** the `osk` prefix is easy to mistake for a 2-byte length prefix — do not treat the first 2 bytes of an STN as a length directly.

---

## Chunk Type Reference

All `kctg*` constants are defined in `inc/soc.h` (game-level) and `kauai/src/framedef.h` (framework-level).

### Frequently Encountered Types

| Tag | Constant | Description | Documented |
|-----|----------|-------------|------------|
| `MVIE` | `kctgMvie` | Movie root chunk | [movie.md](movie.md) |
| `SCEN` | `kctgScen` | Scene | [movie.md](movie.md) |
| `ACTR` | `kctgActr` | Actor instance | [movie.md](movie.md) |
| `PATH` | `kctgPath` | Actor route | [movie.md](movie.md) |
| `GGAE` | `kctgGgae` | Actor events | [movie.md](movie.md) |
| `BKGD` | `kctgBkgd` | Background | [movie.md](movie.md) |
| `CAM ` | `kctgCam`  | Camera | [movie.md](movie.md) |
| `MSND` | `kctgMsnd` | Movie sound | [sounds.md](sounds.md) |
| `GLMS` | `kctgGlms` | Motion-match sounds | [sounds.md](sounds.md) |
| `TMPL` | `kctgTmpl` | Actor/prop template | [actors-and-props.md](actors-and-props.md) |
| `ACTN` | `kctgActn` | Animation action | [actors-and-props.md](actors-and-props.md) |
| `TMTH` | `kctgTmth` | Actor thumbnail ref | [actors-and-props.md](actors-and-props.md) |
| `PRTH` | `kctgPrth` | Prop thumbnail ref | [actors-and-props.md](actors-and-props.md) |
| `BKTH` | `kctgBkth` | Background thumbnail ref | — |
| `MBMP` | `kctgBpmp` | Masked bitmap image | below |
| `BMDL` | `kctgBmdl` | BRender 3D model | — |
| `MTRL` | `kctgMtrl` | Material | — |
| `CMTL` | `kctgCmtl` | Custom material | — |
| `GOKD` | — | GOK UI descriptor | — |
| `TBOX` | `kctgTbox` | Text box | [movie.md](movie.md) |
| `GLXF` | `kctgGlxf` | Transform list (GL of BMAT34) | [actors-and-props.md](actors-and-props.md) |
| `GGCL` | `kctgGgcl` | Cel sequence group | [actors-and-props.md](actors-and-props.md) |
| `GLPI` | `kctgGlpi` | Part info list | — |
| `GLBS` | `kctgGlbs` | Body-part set list | — |

---

## `MBMP` — Masked Bitmap

**Tag:** `'MBMP'` (`kctgBpmp`)

An 8-bit / 256-colour indexed-colour bitmap with an 8-bit transparency mask. Used for background images, thumbnails, and scene previews.

Decoder implemented in `../go/mbmp.go`. The `3dmm-go mbmp` command decodes an extracted `MBMP` bin file to PNG.

---

## Inspecting Archives

```bash
# List all chunks (flags must precede the filename):
./go/3dmm-go chunky list chunks/uk/TMPLS.3CN

# Filter to one chunk type and show child types:
./go/3dmm-go chunky list --ctg TMPL --kids chunks/uk/TMPLS.3CN

# Extract all chunks to a directory:
./go/3dmm-go chunky extract --outdir /tmp/out chunks/uk/ACTOR.3TH

# Visualise the parent→child graph (requires Graphviz):
./go/3dmm-go dag --o /tmp/actor.dot chunks/uk/ACTOR.3TH
dot -Tpng -o /tmp/actor.png /tmp/actor.dot
```
