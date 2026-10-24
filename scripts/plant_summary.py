#!/usr/bin/env python3
"""Tabulate the gated findings of runs written by scripts/plant.sh.

Prints one line per run with the `downstream summary` counts, then every gated
finding that is not noise, with B1, B2, C, the allowed rise, the noise band and
the smallest detectable change. Writes gated.tsv with every app-wide gated finding and
every step finding that is not noise (all other step findings are noise).

Usage: python3 scripts/plant_summary.py <dir-with-run-NN.json> [<evidence-dir>]
"""
import json
import sys
from pathlib import Path


def main():
    root = Path(sys.argv[1])
    dest = Path(sys.argv[2]) if len(sys.argv) > 2 else root
    dest.mkdir(parents=True, exist_ok=True)
    runs = sorted(root.glob("run-*.json"))
    if not runs:
        print(f"no run-*.json in {root}", file=sys.stderr)
        return 1
    rows = ["run\tfinding\tclass\tb1\tb2\tc\tallowed\tband\tdetectable\tdelta_pct"]
    print("run\tsummary")
    for path in runs:
        summ = (root / path.name.replace("run-", "summary-").replace(".json", ".txt"))
        counts = " ".join(summ.read_text().split()) if summ.exists() else "-"
        print(f"{path.stem}\t{counts}")
        run = json.loads(path.read_text())
        for f in run.get("findings", []):
            if not f.get("gated"):
                continue
            rows.append("\t".join([
                path.stem, f["id"], f["class"], f"{f['b1']:.4f}", f"{f['b2']:.4f}",
                f"{f['c']:.4f}", f"{f.get('allowed', 0):.4f}", f"{f['band']:.4f}",
                f"{f.get('detectable', 0):.4f}", f"{f['delta_pct']:.2f}",
            ]))
    (dest / "gated.tsv").write_text("\n".join(
        [rows[0]] + [r for r in rows[1:] if r.split("\t")[1].startswith("app.")
                      or r.split("\t")[2] != "noise"]) + "\n")
    print()
    print("gated findings that are not noise:")
    for r in rows[1:]:
        if r.split("\t")[2] != "noise":
            print(r)
    return 0


if __name__ == "__main__":
    sys.exit(main())
