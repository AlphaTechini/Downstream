# Planted regressions and the clean control, measured

Each set is five runs of `scripts/plant.sh <change> <journeys> 5 <dir>` on one 4-CPU machine
with Go 1.22.12 and k6 v0.56.0 (`machine.txt` in each set), followed by
`python3 scripts/plant_summary.py <dir> evidence/plants/<set>`. The baseline is the commit
"Plant an unbounded book list in the demo and run it in CI" (its SHA at the time is in
`machine.txt`); the candidate is the same commit with the patch from `examples/changes/` applied.
Rule in effect: the gated rule of `engine/classify` (latency advisory). Raw output per run:
`summary-NN.txt` (the `downstream summary` counts) and `gated.tsv` (every app-wide gated finding,
and any step finding that is not noise, with B1, B2, C, allowed rise, noise band and
smallest detectable change). `table.txt` lists the gated findings that are not noise.

Both changes are planted; neither comes from real history. `remove-list-limit` is a scripted
edit: the patch is the output of the fixed `sed` command in
[`examples/changes/README.md`](../../examples/changes/README.md). These runs say nothing about
how often Downstream would catch a real change.

| Set | Runs with a regression | Gated signal (all five runs) | Source |
|---|---|---|---|
| `raise-hash-cost`, `journeys.example.json` | 5 of 5 | CPU per completed journey from 2.1 to 2.4 ms (B1, B2) to 255.3 to 265.6 ms (C) | [table](raise-hash-cost-example/table.txt) L2-L6 and the `app.cpu_ms_per_journey` lines |
| `remove-list-limit`, `journeys.thorough.json` | 5 of 5 | bytes written per completed journey +58.10% to +59.80%; peak RSS +6.05% to +10.89% | [table](remove-list-limit-thorough/table.txt) L2-L6 and the `app.write_bytes_per_journey`, `app.peak_rss_mb` lines |
| `clean-change`, `journeys.example.json` (control) | 0 of 5 | none: every gated finding is noise | [table](clean-change-example/table.txt) L2-L8 |
| `clean-change`, `journeys.thorough.json` (control) | 0 of 5 | none: every gated finding is noise | [table](clean-change-thorough/table.txt) L2-L8 |

What else the runs show:

- `raise-hash-cost` also raises peak RSS by 3.17% to 3.82%. In four runs of five the engine
  pairs it with a fall in write syscalls per journey (4.90% to 6.75%) and reports both as a
  trade-off, which becomes a question on the merge request; in run 3 the RSS rise is a second
  regression in the same issue ([table](raise-hash-cost-example/table.txt)). Its latency moves
  too, but only as advisory findings (`advisory=` in L2-L6).
- `remove-list-limit` needs journeys that list more books than one page holds; the example
  journeys list one tag and never pass the page size, so it was not run with them. Its latency
  stays within noise (`advisory=0` in L2-L6). One run of the same plant and journeys under the
  rule before gating (latency plus a 10 MB memory floor) reported `regression=0`; that run's
  output was not kept, so it is not evidence here.
- A measurement of an unbounded list on a different service, outside this repository and
  not checked here, had bytes written rise about 42 times. That did not happen here: the
  demo's list grows only to the books the journeys create in one pass, so bytes per journey
  rose by about 58%.

## Later checks, and why peak RSS is allowed 5%

[`overlay/`](overlay/) repeats the four sets, three runs each, with the engine at the commit
"Read allocations, GC cycles and goroutines through a build overlay". Every plant run reports
a regression and every clean run none; the runtime counters also report `remove-list-limit`
(alloc bytes and GC cycles rise, because `listBooks` reserves room for the limit on every
call; `overlay/remove-list-limit-journeys.thorough/table.txt`).

[`rss-2pct/`](rss-2pct/) repeats the four sets, three runs each, with the engine at the commit
"Fix the review findings in the engine, flow and docs", when peak RSS was allowed 2%. Both
plants were reported in every run, but one clean run was too: `clean-change` with the
thorough journeys, run 2, peak RSS 12.71 MB against 12.44 and 12.38 MB, +2.17% over the worse
baseline ([gated.tsv](rss-2pct/clean-change-thorough/gated.tsv), the `run-02` peak RSS line).
A/A runs never sample this, because base and head are the same binary there; on clean changes
the candidate's peak RSS ranged from -3.51% to +2.17% of the worse baseline (this file's
`clean-change-thorough` set and `rss-2pct/clean-change-thorough`). Peak RSS is now allowed 5%.

[`final/`](final/) repeats the four sets with the 5% tolerance (commit "Allow peak memory 5%
between two builds"), three runs each. Every plant run reports a regression and every clean
run none (`summary-NN.txt` in each set): `raise-hash-cost` through CPU per completed journey,
`remove-list-limit` through bytes written, alloc bytes, GC cycles and peak RSS
(`final/*/table.txt`). With peak RSS allowed 5%, `raise-hash-cost` no longer raises a
trade-off question.
