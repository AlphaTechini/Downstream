#!/bin/sh
# Measure one change from examples/changes: the current commit is the baseline,
# the same commit with the patch applied is the candidate. Works in a scratch
# clone, so the checkout is never modified.
#
# Usage: scripts/plant.sh <change> <journeys.json> <runs> <out-dir>
# Needs: Go (go.mod version) and k6 on PATH.
set -eu
change=$1
journeys=$2
runs=$3
out=$(mkdir -p "$4" && cd "$4" && pwd)
src=$(pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git clone --quiet "$src" "$work/repo"
cd "$work/repo"
base=$(git rev-parse HEAD)
git apply "examples/changes/$change.patch"
git -c user.name="Downstream plant" -c user.email="plant@example.invalid" \
  commit --quiet --all --message "Apply examples/changes/$change.patch"
head=$(git rev-parse HEAD)
go build -o "$work/downstream" ./engine/cmd/downstream
{
  echo "change: $change"
  echo "journeys: $journeys"
  echo "base: $base"
  echo "date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "cpus: $(nproc)"
  echo "cpu: $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ //')"
  echo "go: $(go version)"
  echo "k6: $(k6 version)"
  echo "commands: downstream scan --base <base> --head <head> --out scan.json; downstream run --base <base> --head <head> --journeys $journeys --out run-N.json; downstream summary --run run-N.json --scan scan.json"
} > "$out/machine.txt"
"$work/downstream" scan --base "$base" --head "$head" --out "$out/scan.json"
i=1
while [ "$i" -le "$runs" ]; do
  n=$(printf '%02d' "$i")
  "$work/downstream" run --base "$base" --head "$head" --journeys "$journeys" \
    --out "$out/run-$n.json" 2> "$out/run-$n.log" || echo "run $n exited non-zero" >> "$out/run-$n.log"
  "$work/downstream" summary --run "$out/run-$n.json" --scan "$out/scan.json" > "$out/summary-$n.txt"
  i=$((i + 1))
done
