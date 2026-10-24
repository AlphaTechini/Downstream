#!/bin/sh
# A/A check: run the engine with the same commit as base and head, N times,
# and keep every run.json. Any "regression" in these runs is false, because
# all three passes run identical code.
#
# Usage: scripts/aa.sh <runs> <out-dir> [journeys.json]
# Needs: Go (go.mod version), k6 on PATH, a clean checkout (base = HEAD).
set -eu
runs=$1
out=$2
journeys=${3:-examples/shelf/journeys.example.json}
mkdir -p "$out"
sha=$(git rev-parse HEAD)
go build -o "$out/downstream" ./engine/cmd/downstream
{
  echo "commit: $sha"
  echo "journeys: $journeys"
  echo "date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "cpus: $(nproc)"
  echo "cpu: $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ //')"
  echo "kernel: $(uname -r)"
  echo "go: $(go version)"
  echo "k6: $(k6 version)"
  echo "command: downstream run --base $sha --head $sha --journeys $journeys --out run-N.json"
} > "$out/machine.txt"
i=1
while [ "$i" -le "$runs" ]; do
  n=$(printf '%02d' "$i")
  "$out/downstream" run --base "$sha" --head "$sha" --journeys "$journeys" \
    --out "$out/run-$n.json" 2> "$out/run-$n.log" || echo "run $n exited non-zero" >> "$out/run-$n.log"
  i=$((i + 1))
done
rm -f "$out/downstream"
