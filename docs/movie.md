# .3MM Movie File Format

A `.3MM` file is a [chunky archive](chunky-files.md) that stores a complete 3D Movie Maker movie. The file format identifier is `kftg3mm` (`MacWin('3mm', '3MM')`).

All multi-byte integers use the native byte order indicated by the `bo` field in each chunk header. Byte order marker `kboCur` = `0x0000FFFE` (little-endian on x86). Chunk data is versioned via `DVER` (two shorts: `swCur` and `swMin`).

## File Hierarchy

```
.3MM
└─ MVIE  (root)
   ├─ GST   chid=0            actor roll-call
   ├─ GST   chid=1            source roll-call
   └─ SCEN  chid=0…N
      ├─ THUM  chid=0         thumbnail bitmap
      ├─ GGFR  chid=0         per-frame scene events
      ├─ GGST  chid=1         scene-start events
      ├─ BKGD  (ref)          background scene
      ├─ MSND  chid=…         scene-level sounds
      ├─ ACTR  chid=0…N
      │  ├─ PATH  chid=0      route points (GL of RPT)
      │  └─ GGAE  chid=0      actor events  (GG of AEV)
      └─ TBOX  chid=0…N
         └─ RTXT  chid=N
            ├─ TEXT  chid=0   raw text bytes
            └─ GLMP  chid=0   formatting map (GL of MPE)
```

---

## Chunk Reference

### MVIE — Movie

**Tag:** `'MVIE'` (`kctgMvie`)
**Source:** `src/engine/movie.cpp`, `inc/movie.h`

The root chunk of every `.3MM` file. Contains a small header (`MFP`) followed by
movie-level metadata managed entirely at runtime via the `MVIE` class.

#### On-disk header — `MFP` (Movie File Prefix)

| Offset | Type    | Field    | Description                                   |
|--------|---------|----------|-----------------------------------------------|
| 0      | `short` | `bo`     | Byte-order marker (`kboCur`)                  |
| 2      | `short` | `osk`    | OS kind (`koskCur`)                           |
| 4      | `DVER`  | `dver`   | File version; `swCur = kcvnCur = 2`, `swMin = kcvnBack = 2` |

`kcvnMin = 1` is the oldest version this code can read.

#### Runtime fields (MVIE class, not serialised directly)

| Field         | Type   | Description                          |
|---------------|--------|--------------------------------------|
| `_aridLim`    | `long` | Highest actor ID assigned so far     |
| `_cscen`      | `long` | Total number of scenes               |
| `_stnTitle`   | `STN`  | Movie title string                   |

#### Children

| Tag    | chid              | Description                       |
|--------|-------------------|-----------------------------------|
| `GST`  | `0`               | Actor roll-call (see GST)         |
| `GST`  | `kchidGstSource=1`| Source roll-call                  |
| `SCEN` | `0…N`             | Scene chunks (one per scene)      |
| `MSND` | various           | Movie-level sounds                |

---

### SCEN — Scene

**Tag:** `'SCEN'` (`kctgScen`)
**Source:** `src/engine/scene.cpp`, `inc/scene.h`

One chunk per scene. Contains a `SCENH` header.

#### On-disk header — `SCENH`

| Offset | Type    | Field        | Description                             |
|--------|---------|--------------|-----------------------------------------|
| 0      | `short` | `bo`         | Byte-order marker                       |
| 2      | `short` | `osk`        | OS kind                                 |
| 4      | `long`  | `nfrmLast`   | Index of last frame in scene            |
| 8      | `long`  | `nfrmFirst`  | Index of first frame in scene           |
| 12     | `TRANS` | `trans`      | Transition effect at end of scene       |

BOM: `0x5FC00000`

`TRANS` is an enum (`transNil`, `transCut`, `transFade`, …).

#### Children

| Tag    | chid      | Description                                        |
|--------|-----------|----------------------------------------------------|
| `THUM` | `0`       | Scene thumbnail (MBMP)                             |
| `GGFR` | `0`       | Per-frame event group                              |
| `GGST` | `1`       | Scene-start event group                            |
| `BKGD` | (ref)     | Background reference (from content library)        |
| `ACTR` | `0…N`     | Actor instances                                    |
| `TBOX` | `0…N`     | Text boxes                                         |
| `MSND` | various   | Scene-level sounds                                 |

---

### GST — Global String Table (Actor Roll-Call)

**Tag:** `'GST '` (`kctgGst`)
**Source:** `src/engine/movie.cpp` (see `_FOpenEx`, `FSaveToDoc`)

Stored as a Kauai `GST` (General String Table) object. Each entry holds an actor name string plus an extra-data blob of type `MACTR`.

#### Extra-data per entry — `MACTR`

| Offset | Type     | Field      | Description                                  |
|--------|----------|------------|----------------------------------------------|
| 0      | `long`   | `arid`     | Unique actor ID                              |
| 4      | `long`   | `cactRef`  | Number of ACTR instances referencing this    |
| 8      | `ulong`  | `grfbrws`  | Browser display flags (`fbrwsProp`, `fbrwsTdt`) |
| 12     | `TAG`    | `tagTmpl`  | Tag pointing to the actor's TMPL chunk       |

BOM: `0xFC000000 | (kbomTag >> 4)`

`chid=0` is the actor roll-call; `chid=kchidGstSource=1` is the source roll-call used
for tracking where content originally came from.

---

### ACTR — Actor Instance

**Tag:** `'ACTR'` (`kctgActr`)
**Source:** `src/engine/actrsave.cpp`, `inc/actor.h`

One chunk per actor placed in a scene. Contains an `ACTF` header.

#### On-disk header — `ACTF`

| Offset | Type    | Field          | Description                                     |
|--------|---------|----------------|-------------------------------------------------|
| 0      | `short` | `bo`           | Byte-order marker                               |
| 2      | `short` | `osk`          | OS kind                                         |
| 4      | `XYZ`   | `dxyzFullRte`  | Full-route translation offset (X, Y, Z)         |
| 16     | `long`  | `arid`         | Unique actor ID (matches GST entry)             |
| 20     | `long`  | `nfrmFirst`    | First frame this actor is alive                 |
| 24     | `long`  | `nfrmLast`     | Last frame this actor is alive                  |
| 28     | `TAG`   | `tagTmpl`      | Reference to the actor's 3D template (TMPL)     |

BOM: `0x5FFC0000 | kbomTag`

#### XYZ struct

| Offset | Type  | Field | Description  |
|--------|-------|-------|--------------|
| 0      | `BRS` | `dxr` | X coordinate |
| 4      | `BRS` | `dyr` | Y coordinate |
| 8      | `BRS` | `dzr` | Z coordinate |

BOM: `0xFC000000`
`BRS` is a BRender fixed-point scalar.

#### Children

| Tag    | chid | Description                             |
|--------|------|-----------------------------------------|
| `PATH` | `0`  | Route/path for this actor               |
| `GGAE` | `0`  | Actor event timeline                    |
| `TMPL` | —    | Embedded template (custom actors only)  |

---

### PATH — Actor Route

**Tag:** `'PATH'` (`kctgPath`)
**Source:** `src/engine/actrsave.cpp`

Child of ACTR. Serialised as a Kauai `GL` (General List) of `RPT` structures.

#### RPT — Route Point

| Offset | Type  | Field | Description                                               |
|--------|-------|-------|-----------------------------------------------------------|
| 0      | `XYZ` | `xyz` | 3D world position of this waypoint                        |
| 12     | `BRS` | `dwr` | Distance weight to next point; `kdwrNil = BR_SCALAR(-1.0)` means use template cel stepsize |

BOM: `0xFF000000`

The default Z position for new actors is `kzrDefault = BR_SCALAR(-25.0)`.

---

### GGAE — Actor Events

**Tag:** `'GGAE'` (`kctgGgae`)
**Source:** `src/engine/actrsave.cpp`

Child of ACTR. Serialised as a Kauai `GG` (General Group) where each entry is a
variable-length `AEV` (Actor Event) record.

Each entry records a frame-numbered event together with event-type-specific data.
Event types (`AET`) include:

| AET constant    | Meaning                                   |
|-----------------|-------------------------------------------|
| `aetActn`       | Action / animation change                 |
| `aetCost`       | Costume / material change                 |
| `aetAdd`        | Actor enters scene at this frame          |
| `aetFreeze`     | Actor freezes in place                    |
| `aetMove`       | Position step along route                 |
| `aetRotH`       | Heading rotation                          |
| `aetSnd`        | Sound event (references an MSND chunk)    |
| `aetStep`       | Cel step event                            |

---

### GGFR — Per-Frame Scene Events

**Tag:** `'GGFR'` (`kctgFrmGg` / `kctgGgFrm`)
**Source:** `src/engine/scene.cpp`
**Child of:** SCEN at chid=0

A Kauai `GG` storing scene-level events keyed by frame number (camera cuts, background switches, lighting changes, etc.).

---

### GGST — Scene-Start Events

**Tag:** `'GGST'` (`kctgStartGg` / `kctgGgStart`)
**Source:** `src/engine/scene.cpp`
**Child of:** SCEN at chid=1

A Kauai `GG` storing events that fire once when the scene begins, independent of
frame position.

---

### THUM — Scene Thumbnail

**Tag:** `'THUM'` (`kctgThumbMbmp`)
**Source:** `src/engine/scene.cpp`
**Child of:** SCEN at chid=0

An MBMP (Masked Bitmap) chunk — an 8-bit indexed-colour bitmap with an optional
alpha mask used to show a preview of the scene in the movie timeline strip.

---

### TBOX — Text Box

**Tag:** `'TBOX'` (`kctgTbox`)
**Source:** `src/engine/tbox.cpp`, `inc/tbox.h`

One chunk per text box in a scene. Contains a `TBOXH` header.

#### On-disk header — `TBOXH`

| Offset | Type   | Field        | Description                                          |
|--------|--------|--------------|------------------------------------------------------|
| 0      | `short`| `bo`         | Byte-order marker                                    |
| 2      | `short`| `osk`        | OS kind                                              |
| 4      | `long` | `nfrmFirst`  | First frame the text box is visible                  |
| 8      | `long` | `nfrmMax`    | Last frame the text box is visible                   |
| 12     | `long` | `xpLeft`     | Left edge (pixels)                                   |
| 16     | `long` | `xpRight`    | Right edge (pixels)                                  |
| 20     | `long` | `ypTop`      | Top edge (pixels)                                    |
| 24     | `long` | `ypBottom`   | Bottom edge (pixels)                                 |
| 28     | `CHID` | `chid`       | chid of the child RTXT chunk                         |
| 32     | `bool` | `fStory`     | `true` = story text box; `false` = credits text box  |

#### Children

| Tag    | chid | Description          |
|--------|------|----------------------|
| `RTXT` | `N`  | Rich text content    |

---

### RTXT — Rich Text

**Tag:** `'RTXT'` (`kctgRichText`)
**Source:** `kauai/src/rtxt.cpp`, `kauai/src/rtxt.h`
**Child of:** TBOX

A Kauai rich-text document. Contains the text content and formatting metadata.

#### Children

| Tag    | chid | Description                                    |
|--------|------|------------------------------------------------|
| `TEXT` | `0`  | Raw text bytes                                 |
| `GLMP` | `0`  | Formatting property map (GL of MPE entries)    |

---

### TEXT — Plain Text

**Tag:** `'TEXT'` (`kctgText`)
**Source:** `kauai/src/rtxt.cpp`
**Child of:** RTXT at chid=0

Raw character data for the text box content. Stored as a flat byte stream.

---

### GLMP — Text Formatting Map

**Tag:** `'GLMP'` (`kctgGlmp`)
**Source:** `kauai/src/rtxt.cpp`, `kauai/src/framedef.h` line 175
**Child of:** RTXT at chid=0

A Kauai `GL` of `MPE` (Mapping Property Entry) records. Each entry maps a
character or paragraph position to a `CHP` or `PAP` property structure.

#### CHP — Character Properties

| Field       | Type    | Description                               |
|-------------|---------|-------------------------------------------|
| `grfont`    | `ulong` | Font attribute flags (bold, italic, etc.) |
| `onn`       | `long`  | Font selector index                       |
| `dypFont`   | `long`  | Font size in points                       |
| `dypOffset` | `long`  | Baseline offset for sub/superscript       |
| `acrFore`   | `ACR`   | Foreground (text) colour                  |
| `acrBack`   | `ACR`   | Background colour                         |

#### PAP — Paragraph Properties

| Field           | Type    | Description                          |
|-----------------|---------|--------------------------------------|
| `jc`            | `byte`  | Justification (`jcLeft/Right/Center`)|
| `nd`            | `byte`  | Indent type (none/first/rest/all)    |
| `dxpTab`        | `short` | Tab stop width in pixels             |
| `numLine`       | `short` | Line height multiplier (÷256)        |
| `dypExtraLine`  | `short` | Additional line spacing in pixels    |
| `numAfter`      | `short` | Post-paragraph spacing multiplier    |
| `dypExtraAfter` | `short` | Additional post-paragraph spacing    |

---

## Supporting Chunk Types

These chunks appear inside referenced content libraries (`.3CN`, `.3TH`) rather than
directly in `.3MM` files, but are referenced by TAG values stored in ACTR and SCEN.

All tag constants below are defined in `inc/soc.h`.

| Tag     | Constant     | Description                              |
|---------|--------------|------------------------------------------|
| `TMPL`  | `kctgTmpl`   | 3D character/prop template               |
| `BKGD`  | `kctgBkgd`   | Background scene definition              |
| `ACTN`  | `kctgActn`   | Named action / animation clip            |
| `MTRL`  | `kctgMtrl`   | Material (surface shader)                |
| `CMTL`  | `kctgCmtl`   | Custom material                          |
| `MSND`  | `kctgMsnd`   | Movie sound (music / SFX)                |
| `SND `  | `kctgSnd`    | Raw sound data                           |
| `CAM `  | `kctgCam`    | Camera definition                        |
| `GLLT`  | `kctgGllt`   | Light list                               |
| `GLMS`  | `kctgGlms`   | Motion-match sound list                  |
| `GLXF`  | `kctgGlxf`   | Transform list                           |
| `GGCL`  | `kctgGgcl`   | Cel group                                |
| `BDS `  | `kctgBds`    | Body/skeleton definition                 |
| `BPMP`  | `kctgBpmp`   | Body-part bitmap                         |
| `TDF `  | `kctgTdf`    | 3D font definition                       |
| `TDT `  | `kctgTdt`    | 3D text template                         |
| `PICT`  | `kctgPict`   | Picture / image                          |
| `GLPI`  | `kctgGlpi`   | Palette info                             |
| `GLBS`  | `kctgGlbs`   | Billboard / sprite info                  |
| `GLTM`  | `kctgGltm`   | Timing info                              |
| `INFO`  | `kctgInfo`   | Informational metadata                   |

---

## Key Data Types

| Type   | Description                                                        |
|--------|--------------------------------------------------------------------|
| `BRS`  | BRender fixed-point scalar (32-bit, 16.16)                         |
| `BRA`  | BRender angle                                                      |
| `CTG`  | Chunk tag — 4 ASCII characters packed into a `ulong`               |
| `CNO`  | Chunk number — 32-bit unique identifier within a file              |
| `CHID` | Child ID — 32-bit identifier used to distinguish children with the same tag |
| `TAG`  | `{ CTG ctg; CNO cno; }` — reference to a chunk (possibly in another file) |
| `GL`   | General List — typed dynamic array (Kauai collection)              |
| `GG`   | General Group — variable-length-entry dynamic array                |
| `GST`  | General String Table — associative table of string + extra data    |
| `TRANS`| Transition enum: `transNil`, `transCut`, `transFade`, …            |

---

## Source Files

| File                              | Covers                                  |
|-----------------------------------|-----------------------------------------|
| `src/engine/movie.cpp`            | MVIE read/write, GST roll-call          |
| `inc/movie.h`                     | MVIE class, MFP, MACTR declarations     |
| `src/engine/scene.cpp`            | SCEN, GGFR, GGST, THUM read/write      |
| `inc/scene.h`                     | SCEN class, SCENH declaration           |
| `src/engine/actrsave.cpp`         | ACTR, PATH, GGAE read/write (detailed)  |
| `inc/actor.h`                     | ACTR class, ACTF, XYZ, RPT, AEV        |
| `src/engine/tbox.cpp`             | TBOX read/write                         |
| `inc/tbox.h`                      | TBOX class, TBOXH declaration           |
| `kauai/src/rtxt.cpp`              | RTXT, TEXT, GLMP read/write             |
| `kauai/src/rtxt.h`                | CHP, PAP, MPE declarations              |
| `inc/soc.h`                       | All `kctg*` chunk tag constants         |
| `kauai/src/framedef.h`            | Framework chunk tag constants           |
