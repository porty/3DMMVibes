# Sound Storage in 3D Movie Maker

3D Movie Maker supports four sound types, which appear in two distinct contexts: **scene-level sounds** (tied to a frame in a scene) and **actor sounds** (tied to an actor's animation timeline). Both ultimately reference `MSND` chunks that wrap the raw audio data.

**Sound types** (`sty` field — defined in `inc/msnd.h`):

| Value | Constant     | Meaning          |
|-------|--------------|------------------|
| 0     | `styNil`     | None / no sound  |
| 1     | `styUnused`  | Obsolete (retained for file compatibility) |
| 2     | `stySfx`     | Sound effect (WAV) |
| 3     | `stySpeech`  | Voice / speech (WAV) |
| 4     | `styMidi`    | MIDI music       |

---

See also [actors-and-props.md](actors-and-props.md) for how `GLMS` motion-match sounds relate to `ACTN` action chunks, and [chunky-files.md](chunky-files.md) for which archives contain `MSND` data (`SNDS.3CN`).

---

## MSND — Movie Sound

**Tag:** `'MSND'` (`kctgMsnd`)
**Source:** `src/engine/msnd.cpp`, `inc/msnd.h`

A wrapper chunk that describes one sound asset and its playback properties. It appears as a child of both `MVIE` (movie-level sounds) and `SCEN` (scene-level sounds); both contexts use the same on-disk layout.

### On-disk header — `MSNDF`

| Offset | Type    | Field        | Description                        |
|--------|---------|--------------|------------------------------------|
| 0      | `short` | `bo`         | Byte-order marker                  |
| 2      | `short` | `osk`        | OS kind                            |
| 4      | `long`  | `sty`        | Sound type (see table above)       |
| 8      | `long`  | `vlmDefault` | Default playback volume            |
| 12     | `bool`  | `fInvalid`   | Set if the sound asset is missing  |

BOM: `kbomMsndf = 0x5FC00000`

### Child chunk

| Tag     | chid              | Description                   |
|---------|-------------------|-------------------------------|
| `SND ` | `kchidSnd = 0`    | Raw audio data (WAV or MIDI)  |

The `SND ` chunk contains the actual PCM/MIDI bytes in a platform-native format; the `MSND` header above holds only metadata.

---

## Scene-Level Sound Events

Scene sounds are stored as `sevtPlaySnd` events inside the **GGFR** (per-frame event group) of a `SCEN` chunk. They fire once per occurrence when playback reaches the keyed frame.

See [movie.md](movie.md) for the overall chunk hierarchy.

### Event record in GGFR

The fixed part of each GGFR entry (8 bytes) carries the frame number and event type; the variable part holds the `SSE` payload:

| Field | Type    | Description                                   |
|-------|---------|-----------------------------------------------|
| `Nfrm`| `long`  | Frame on which this sound event fires         |
| `Sevt`| `long`  | `sevtPlaySnd = 1`                             |

### SSE — Scene Sound Event (variable data)

`SSE` is a variable-length structure: a fixed 16-byte header followed by an array of `ctagc` `TAGC` entries (20 bytes each).

**Fixed header (16 bytes):**

| Offset | Type   | Field   | Description                                       |
|--------|--------|---------|---------------------------------------------------|
| 0      | `long` | `vlm`   | Playback volume                                   |
| 4      | `long` | `sty`   | Sound type                                        |
| 8      | `bool` | `fLoop` | Loop flag (stored as 4-byte value)                |
| 12     | `long` | `ctagc` | Number of `TAGC` entries that follow              |

BOM: `kbomSse = 0xFF000000` (covers the 16-byte fixed header)

**Per-entry `TAGC` (20 bytes, repeated `ctagc` times):**

| Offset | Type   | Field      | Description                                |
|--------|--------|------------|--------------------------------------------|
| 0      | `CHID` | `chid`     | Child chunk ID of the referenced `MSND`   |
| 4      | `TAG`  | `tag`      | TAG referencing the `MSND` chunk (16 bytes: `sid`, `pcrf`, `ctg=MSND`, `cno`) |

BOM per TAGC: `kbomTagc = kbomChid | (kbomTag >> 2)`

A single `sevtPlaySnd` event can reference multiple sounds (one per TAGC entry), all of which play simultaneously when the frame is reached.

---

## Actor Sound Events

Actor sounds are stored as `aetSnd` events in the **GGAE** (actor event group) of an `ACTR` chunk. They fire when the actor's animation reaches the specified route location.

See [movie.md](movie.md) for GGAE structure and other actor event types.

### `aetSnd` event — `AEVSND` variable data (44 bytes)

**Source:** `inc/actor.h`

| Offset | Type      | Field      | Description                                                          |
|--------|-----------|------------|----------------------------------------------------------------------|
| 0      | `tribool` | `fLoop`    | Loop sound: `0`=no, `1`=yes, `2`=maybe                              |
| 4      | `tribool` | `fQueue`   | Queue sound: if true, multiple events of the same type can coexist in one frame |
| 8      | `long`    | `vlm`      | Playback volume                                                      |
| 12     | `long`    | `celn`     | Motion-match cel number; `-1` (`ivNil`) means this is **not** a motion-match sound |
| 16     | `long`    | `sty`      | Sound type                                                           |
| 20     | `tribool` | `fNoSound` | Mute flag — event is recorded but produces no audio                  |
| 24     | `CHID`    | `chid`     | Child chunk ID for user-recorded sounds                              |
| 28     | `long`    | `tag.sid`  | Source ID of the referenced `MSND`                                  |
| 32     | `ulong`   | `tag.pcrf` | Runtime pointer — always `0` on disk                                |
| 36     | `uint32`  | `tag.ctg`  | Chunk type of the referenced `MSND` (`'MSND'`)                      |
| 40     | `uint32`  | `tag.cno`  | Chunk number of the referenced `MSND`                               |

Total: 44 bytes (`kcbVarSnd = size(AEVSND)`)

### Motion-match sounds

When `celn != -1`, the event is a **motion-match sound**: the sound is bound to a specific animation cel rather than a fixed point in time. The runtime queues these into an `SMM` (Sound Motion Match) list and replays them each time the actor cycles through that cel. `SMM` is a runtime-only structure and is not serialized to disk; motion-match bindings are persisted through `aetSnd` events with `celn >= 0`.

The `GLMS` chunk (tag `'GLMS'`, `kctgGlms`) stores the default motion-match sound bindings for each action (`ACTN`) in a template and is found as a child of `ACTN` chunks inside content libraries (`.3CN` / `.3TH`). It is a `GL` of sound binding records. These defaults are copied into `aetSnd` events at author time; the actor event list (`GGAE`) is the authoritative per-actor record in the saved movie.

### Non-motion-match vs. motion-match dispatch

At playback time (`src/engine/actor.cpp`, `_FDoFrm`):

- If `celn == -1` (non-motion-match): the sound is enqueued immediately when the event is encountered.
- If `celn >= 0` (motion-match): the event is entered into the runtime `SMM` list and enqueued to the sound queue (`MSQ`) each time the actor's animation advances through that cel.

---

## Background Default Sound

Each background scene (`BKGD`) can specify a default ambient sound stored in the `BDS` (Background Definition) chunk's `tagSnd` field. This is not a playback event but an author-time default that the scene inherits when the background is set.

**Relevant fields in `BDS` (`inc/bkgd.h`):**

| Field    | Type   | Description                            |
|----------|--------|----------------------------------------|
| `vlm`    | `long` | Default ambient volume                 |
| `fLoop`  | `bool` | Whether the ambient sound loops        |
| `tagSnd` | `TAG`  | Reference to the ambient `MSND` chunk  |

---

## Summary: Where Sounds Live

| Location                         | Chunk   | Event / Field      | Fires when                        |
|----------------------------------|---------|--------------------|-----------------------------------|
| Movie-level ambient              | `MSND` child of `MVIE` | —          | Managed at runtime                |
| Scene-level per-frame sound      | `GGFR` event in `SCEN` | `sevtPlaySnd` (type 1) | Playback reaches frame `Nfrm` |
| Actor non-motion-match sound     | `GGAE` event in `ACTR` | `aetSnd` (type 6), `celn = -1` | Actor path reaches route location |
| Actor motion-match sound         | `GGAE` event in `ACTR` | `aetSnd` (type 6), `celn >= 0` | Actor animation cycles through cel `celn` |
| Background default ambient       | `BDS` in `BKGD`        | `tagSnd`           | Background is set in scene        |
| Template action default MM sound | `GLMS` child of `ACTN` | GL of bindings     | Copied to GGAE at author time     |

---

## Source Files

| File                       | Covers                                                  |
|----------------------------|---------------------------------------------------------|
| `inc/msnd.h`               | `MSNDF`, `MSND` class, `sty*` constants, `MSQ`, `SQE`  |
| `src/engine/msnd.cpp`      | MSND read/write and playback                            |
| `inc/actor.h`              | `AEVSND`, `SMM`, `smmNil`, `kcbVarSnd` constant        |
| `src/engine/actor.cpp`     | `_FDoFrm`, `_FEnqueueSnd`, motion-match dispatch       |
| `src/engine/actrsave.cpp`  | `aetSnd` serialisation in GGAE                         |
| `inc/scene.h`              | `SSE`, `TAGC`, `sevtPlaySnd`, `FAddSnd*` declarations  |
| `src/engine/scene.cpp`     | `SSE` struct, `TAGC` struct, scene sound read/write    |
| `inc/bkgd.h`               | `BDS` struct with `tagSnd` / `fLoop` / `vlm`           |
| `inc/soc.h`                | `kctgMsnd`, `kctgGlms` chunk tag constants             |
