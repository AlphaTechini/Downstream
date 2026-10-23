#!/usr/bin/env python3
"""Summarise A/A runs written by scripts/aa.sh.

Every pass in an A/A run executes the same commit, so every finding classed
"regression" is a false regression. Runs from an engine that splits findings into
gated and advisory (run.json findings with "gated") count only gated ones as
regressions; advisory regressions are counted separately. Prints, per metric: findings, how many were
classed regression, improved, withheld or noise, the per-run rate of at least one
false regression, the B1/B2 spread (|B1-B2| / min(B1, B2)), and "no floor": how
many latency or CPU findings the same band and relative rules would call a
regression if the absolute floor (10 ms, 0.5 s) were removed.

For the per-pass process counters (CPU per journey, bytes and write syscalls per
journey, fds, threads, peak RSS, exact CPU) it also prints the B1/B2 spread and the
"excess": how far C lands above the worse of B1 and B2, as a fraction of that worse
baseline. On identical code the largest excess is the smallest tolerance that would
have raised no false regression in these runs.

Writes findings.tsv (one line per finding that is not noise; summary.txt counts every
class) and passes.tsv (the raw
per-pass figures: one line per step per pass, plus one "app" line per pass with the
process counters) next to the runs.

Usage: python3 scripts/aa_summary.py <dir-with-run-NN.json> [<evidence-dir>]

With a second directory, the summary inputs (run-NN.json) stay where they are and
summary.txt, findings.tsv and passes.tsv are written to the evidence directory.
"""
import json
import statistics
import sys
from collections import defaultdict
from pathlib import Path


def pct(values, q):
    values = sorted(values)
    if not values:
        return 0.0
    k = (len(values) - 1) * q
    lo, hi = int(k), min(int(k) + 1, len(values) - 1)
    return values[lo] + (values[hi] - values[lo]) * (k - lo)


def main():
    root = Path(sys.argv[1])
    dest = Path(sys.argv[2]) if len(sys.argv) > 2 else root
    dest.mkdir(parents=True, exist_ok=True)
    runs = sorted(root.glob("run-*.json"))
    if not runs:
        print(f"no run-*.json in {root}", file=sys.stderr)
        return 1
    classes = defaultdict(lambda: defaultdict(int))
    spread = defaultdict(list)
    runs_with_regression = defaultdict(int)
    any_regression_runs = 0
    advisory = defaultdict(int)
    nofloor = defaultdict(int)
    nofloor_runs = 0
    stopped = 0
    counters = ("cpu_ms_per_journey", "write_bytes_per_journey", "write_syscalls_per_journey",
                "open_fds", "threads", "peak_rss_mb", "cpu_seconds_exact", "cpu_seconds")
    cspread = defaultdict(list)
    cexcess = defaultdict(list)
    lines = ["run\tfinding\tmetric\tb1\tc\tb2\tband\tdelta_pct\tclass"]
    passes = ["run\tpass\tjourney\tstep\tcount\tfailures\tp50_ms\tp95_ms\tmean_ms\tprocess"]
    for path in runs:
        run = json.loads(path.read_text())
        for p in run.get("passes", []):
            proc = json.dumps(p.get("process", {}), sort_keys=True, separators=(",", ":"))
            passes.append("\t".join([path.stem, p["label"], "app", "", "", "", "", "", "", proc]))
            for st in p.get("steps", []):
                passes.append("\t".join([
                    path.stem, p["label"], st["journey"], st["step"], str(st["count"]),
                    str(st["failures"]), f"{st['p50_ms']:.3f}", f"{st['p95_ms']:.3f}",
                    f"{st['mean_ms']:.3f}", "",
                ]))
        if run.get("error"):
            stopped += 1
        procs = {p["label"]: p.get("process", {}) for p in run.get("passes", [])}
        if {"B1", "C", "B2"} <= set(procs):
            for k in counters:
                b1, c, b2 = (procs[x].get(k) for x in ("B1", "C", "B2"))
                if b1 is None or c is None or b2 is None:
                    continue
                lo, hi = min(b1, b2), max(b1, b2)
                if lo > 0:
                    cspread[k].append((hi - lo) / lo)
                if hi > 0:
                    cexcess[k].append((c - hi) / hi)
        seen = set()
        nofloor_seen = False
        for f in run.get("findings", []):
            m = f["metric"]
            cls = f["class"]
            if cls == "regression" and f.get("gated") is False:
                advisory[m] += 1
                cls = "advisory regression"
            classes[m][cls] += 1
            lo = min(f["b1"], f["b2"])
            if lo > 0:
                spread[m].append(abs(f["b1"] - f["b2"]) / lo)
            if cls == "regression":
                seen.add(m)
            if m != "failures" and f["delta"] > 2 * f["band"] and f["delta_pct"] >= 20:
                nofloor[m] += 1
                nofloor_seen = True
            lines.append("\t".join([
                path.stem, f["id"], m, f"{f['b1']:.4f}", f"{f['c']:.4f}", f"{f['b2']:.4f}",
                f"{f['band']:.4f}", f"{f['delta_pct']:.2f}", cls,
            ]))
        for m in seen:
            runs_with_regression[m] += 1
        if seen:
            any_regression_runs += 1
        if nofloor_seen:
            nofloor_runs += 1
    (dest / "findings.tsv").write_text(
        "\n".join([lines[0]] + [x for x in lines[1:] if not x.endswith("\tnoise")]) + "\n")
    (dest / "passes.tsv").write_text("\n".join(passes) + "\n")

    n = len(runs)
    print(f"runs: {n} (stopped early: {stopped})")
    print(f"runs with at least one false regression (any metric): {any_regression_runs} of {n}")
    print(f"runs with at least one false regression if the absolute floor were removed: {nofloor_runs} of {n}")
    if advisory:
        print(f"advisory (not deciding) latency regressions: {sum(advisory.values())}")
    print()
    header = ("metric", "findings", "regression", "rate", "runs>=1", "improved", "withheld",
              "noise", "other", "no floor", "spread p50", "spread p90", "spread max")
    print("\t".join(header))
    for m in sorted(classes):
        c = classes[m]
        total = sum(c.values())
        other = total - sum(c[k] for k in ("regression", "improved", "withheld", "noise"))
        s = spread[m]
        print("\t".join([
            m, str(total), str(c["regression"]),
            f"{100 * c['regression'] / total:.1f}%" if total else "-",
            f"{runs_with_regression[m]} of {n}",
            str(c["improved"]), str(c["withheld"]), str(c["noise"]), str(other),
            f"{nofloor[m]} ({100 * nofloor[m] / total:.1f}%)" if m != "failures" and total else "-",
            f"{100 * statistics.median(s):.1f}%" if s else "-",
            f"{100 * pct(s, 0.9):.1f}%" if s else "-",
            f"{100 * max(s):.1f}%" if s else "-",
        ]))
    if cspread or cexcess:
        print()
        print("\t".join(("counter", "runs", "spread p50", "spread max", "excess p50",
                         "excess p90", "excess max")))
        for k in counters:
            sp, ex = cspread.get(k, []), cexcess.get(k, [])
            if not ex:
                continue
            print("\t".join([
                k, str(len(ex)),
                f"{100 * statistics.median(sp):.2f}%" if sp else "-",
                f"{100 * max(sp):.2f}%" if sp else "-",
                f"{100 * statistics.median(ex):.2f}%",
                f"{100 * pct(ex, 0.9):.2f}%",
                f"{100 * max(ex):.2f}%",
            ]))
    return 0


if __name__ == "__main__":
    sys.exit(main())
