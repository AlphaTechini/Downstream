# Planted regressions and the clean control, measured

Each set is five runs of `scripts/plant.sh <change> <journeys> 5 <dir>` on one 4-CPU machine
with Go 1.22.12 and k6 v0.56.0 (`machine.txt` in each set), followed by
`python3 scripts/plant_summary.py <dir> evidence/plants/<set>`. The baseline is commit
`5b81f03`; the candidate is the same commit with the patch from `examples/changes/` applied.
Rule in effect: the gated rule of `engine/classify` (latency advisory). Raw output per run:
`summary-NN.txt` (the `downstream summary` counts) and `gated.tsv` (every app-wide gated finding,
and any step finding that is not noise, with B1, B2, C, allowed rise, noise band and
smallest detectable change). `table.txt` lists the
gated findings that are not noise.

`raise-hash-cost` was written by an agent in an earlier session; `remove-list-limit` is a
scripted edit, not AI-written (the `sed` command is in
[`examples/changes/README.md`](../../examples/changes/README.md)). Both are planted; neither
comes from real history. These runs say nothing about how often Downstream would catch a real
change.

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
- The "about 42 times more bytes written" expected for an unbounded list did not happen
  here: the demo's list grows only to the books the journeys create in one pass, so bytes per
  journey rose by about 58%.
