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
