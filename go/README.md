# 3DMM Go

Go rewrite of the 3D Movie Maker engine.

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
