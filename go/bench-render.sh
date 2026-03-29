#!/usr/bin/env bash
set -euo pipefail

BENCH=BenchmarkRenderMovieRGB24
BENCHTIME=${BENCHTIME:-5s}
CPU_PROF=cpu.prof
MEM_PROF=mem.prof

echo "==> Running $BENCH (benchtime=$BENCHTIME) ..."
go test \
  -bench="$BENCH" \
  -benchtime="$BENCHTIME" \
  -benchmem \
  -cpuprofile="$CPU_PROF" \
  -memprofile="$MEM_PROF" \
  .

echo ""
echo "==> Profiles written:"
echo "      CPU : $CPU_PROF"
echo "      Mem : $MEM_PROF"
echo ""
echo "==> Next steps:"
echo ""
echo "  Flamegraph (opens browser at http://localhost:8080, View -> Flame Graph):"
echo "    go tool pprof -http=:8080 $CPU_PROF"
echo ""
echo "  CPU top functions (CLI):"
echo "    go tool pprof $CPU_PROF"
echo "    (then: top20 / list <funcname> / web)"
echo ""
echo "  Allocation hotspots:"
echo "    go tool pprof -alloc_space $MEM_PROF"
echo "    (then: top20 / list <funcname>)"
echo ""
echo "  In-use heap:"
echo "    go tool pprof -inuse_space $MEM_PROF"
