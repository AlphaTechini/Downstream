#!/bin/sh
# Checks the frontend stage on the React and Svelte fixtures. For each fixture it commits
# the base app, a head with planted regressions (plant/) and a head with a harmless text
# change (clean/), runs `downstream frontend` on both pairs and checks the verdicts: every
# planted kind is a regression, and the clean change has none.
#
# Needs Linux, Go 1.22+, Node 20.19+ with npm, git and network access to registry.npmjs.org.
# The last line is "verify-frontend: PASS" or "verify-frontend: FAIL".
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

echo "node $(node --version), npm $(npm --version), $(go version)"
(cd "$ROOT" && go build -o "$WORK/downstream" ./engine/cmd/downstream)
FE=$ROOT/tools/fecheck
if [ ! -d "$FE/node_modules" ]; then
  (cd "$FE" && npm ci --legacy-peer-deps --no-audit --no-fund)
fi

commit() {
  git add -A && git -c user.name=fixture -c user.email=fixture@localhost commit -qm "$1"
  git rev-parse HEAD
}

status=0
for fx in react-vite svelte-vite; do
  src=$ROOT/fixtures/frontend/$fx
  repo=$WORK/$fx
  mkdir -p "$repo"
  (cd "$src" && tar -cf - --exclude=./plant --exclude=./clean --exclude=./journeys.json --exclude=./node_modules --exclude=./dist .) | (cd "$repo" && tar -xf -)
  cd "$repo"
  git init -q
  base=$(commit base)
  cp -R "$src/plant/." .
  plant=$(commit plant)
  git checkout -q "$base"
  cp -R "$src/clean/." .
  clean=$(commit clean)
  cd "$ROOT"

  for pair in plant clean; do
    head=$plant
    [ "$pair" = clean ] && head=$clean
    echo
    echo "== $fx, $pair"
    "$WORK/downstream" frontend --repo "$repo" --base "$base" --head "$head" \
      --journeys "$src/journeys.json" --fecheck "$FE" --out "$WORK/$fx-$pair.json"
    "$WORK/downstream" report --frontend "$WORK/$fx-$pair.json" --out "$WORK/$fx-$pair.md"
    cat "$WORK/$fx-$pair.md"
  done

  python3 - "$WORK/$fx-plant.json" "$WORK/$fx-clean.json" "$fx" <<'EOF' || status=1
import json, sys
plant, clean, fx = json.load(open(sys.argv[1])), json.load(open(sys.argv[2])), sys.argv[3]
def regressions(f):
    return {x["metric"] for j in (f.get("compare") or {}).get("journeys", []) for x in j["findings"] if x["verdict"] == "regression"}
want = {"js bytes (gzip)", "missing assets", "accessibility", "DOM size", "JS errors"}
got = regressions(plant)
ok = plant["status"] == "checked" and want <= got and clean["status"] == "checked" and not regressions(clean)
print(f"{fx}: planted {sorted(got)}; missing {sorted(want - got)}; clean regressions {sorted(regressions(clean))}: {'ok' if ok else 'FAIL'}")
sys.exit(0 if ok else 1)
EOF
done

echo
if [ "$status" -eq 0 ]; then echo "verify-frontend: PASS"; else echo "verify-frontend: FAIL"; fi
exit "$status"
