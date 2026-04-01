# 3DMM Go

Go rewrite of parts of the 3D Movie Maker engine.

This is a work in progress, and is knowably broken.
Actor rendering doesn't seem quite right.
Movie rendering is pretty bad - ignores the z-buffer of the camera angle, actor triangles are rendered all over the place, there is no sound or transitions and the actions seem to be way too fast - but you can render it straight to mp4 via ffmpeg!

## Installation

```bash
cd cmd/3dmm && go install .
```

## Commands

### `chunky` — inspect and extract chunky files (.chk, .3mm, etc.)

```bash
# List all chunks
3dmm chunky list TMPLS.3CN

# Filter by type or number
3dmm chunky list --ctg TMPL TMPLS.3CN
3dmm chunky list --ctg MVIE --kids movie.3mm

# Summary of chunk types across one or more files
3dmm chunky info 3DMOVIE.CHK
3dmm chunky info   # scans current directory

# Extract all chunks to a directory
3dmm chunky extract --outdir ./chunks TMPLS.3CN
3dmm chunky extract --ctg TMPL --verbose TMPLS.3CN
```

### `actor` — list or render actor/prop templates (TMPLS.3CN)

You can pass in the filename of the chunky archive that contains the actors, but leaving it out will read `TMPLS.3CN` from the current directory.

```bash
# List all templates with their actions
3dmm actor list
3dmm actor list TMPLS.3CN # same as above
3dmm actor list my-custom-actors.3cn

# Render a single actor as PNG
3dmm actor render --name Keesha -o keesha.png
3dmm actor render --cno 0x2010 -o actor.png

# Display in terminal
3dmm actor render --name Keesha -t

# Render as animated GIF (360° rotation)
3dmm actor render --name Keesha -f gif -o keesha.gif

# Render all actors into a grid PNG or a directory of GIFs
3dmm actor render --cno all -o actors.png
3dmm actor render --cno all -f gif --o ./gifs
```

### `bkgd` — render background camera angles to the terminal

```bash
3dmm bkgd scene.chk
3dmm bkgd --cno 0x00000001 scene.chk

# View z-buffer (depth) instead of color
3dmm bkgd -z scene.chk
```

### `dag` — dump chunk parent→child graph as Graphviz DOT

```bash
# Write DOT to stdout, then render
3dmm dag TMPLS.3CN | dot -Tpng -o graph.png

# Filter to a subtree
# actors are very complex; TMPLS.3CN very much so
3dmm dag --name Keesha -o keesha.dot TMPLS.3CN
3dmm dag --ctg TMPL --cno 0x2010 -o tmpl.dot TMPLS.3CN
```

### `mbmp` — decode MBMP image chunks to PNG

```bash
3dmm mbmp frame.mbmp -o frame.png
3dmm mbmp --view frame.mbmp            # display in terminal
3dmm mbmp --info frame.mbmp            # print dimensions only
3dmm mbmp --palette pal.bin frame.mbmp -o frame.png
```

### `render` — render a .3MM movie to frames or video

```bash
# PNG frames
3dmm render png --assets ./content movie.3mm
3dmm render png --assets ./content --outdir ./frames --scene 0 movie.3mm

# Raw RGB24 piped to ffmpeg manually
3dmm render rgb24 --assets ./content movie.3mm \
  | ffmpeg -f rawvideo -video_size 640x480 -pixel_format rgb24 -framerate 12 -i - output.mp4

# Or use the built-in ffmpeg subcommand
3dmm render ffmpeg --assets ./content movie.3mm output.mp4
```

## Benchmarking

The render benchmark exercises the full `RenderMovieRGB24` pipeline on a real movie file (`testdata/HOSPITAL.3MM`).

### Quick run

```bash
go test -bench=BenchmarkRenderMovieRGB24 -benchmem .
```

Prints `ns/op`, `B/op`, and `allocs/op` per iteration.

### CPU + memory profiles + flamegraph

Use the included script:

```bash
./bench-render.sh
```

This captures both a CPU profile and a memory profile, then prints instructions for viewing them.

### Manual profiling

**CPU profile:**
```bash
go test -bench=BenchmarkRenderMovieRGB24 -benchtime=5s -cpuprofile=cpu.prof .
go tool pprof -http=:8080 cpu.prof   # View -> Flame Graph in browser
```

**Allocation profile:**
```bash
go test -bench=BenchmarkRenderMovieRGB24 -benchtime=5s -memprofile=mem.prof .
go tool pprof -alloc_space mem.prof
```

Inside the `pprof` CLI: `top20` lists the hottest functions; `list <funcname>` annotates source lines.
