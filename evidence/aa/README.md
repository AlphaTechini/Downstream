# A/A runs: false regressions on identical code

An A/A run gives the engine the same commit as base and head, so B1, C and B2 all run
identical code and every finding classed `regression` is false. These runs measure how often
the classification rule in `engine/classify` cries wolf, and how far apart two baseline runs
of the same code land.

Procedure (repeatable with any commit):

```
scripts/aa.sh 20 <scratch-dir>                       # 20 runs: downstream run --base X --head X
python3 scripts/aa_summary.py <scratch-dir> evidence/aa/<set>
```

`run` accepts the same SHA twice; the runner builds it once
([before/run-01.log](before/run-01.log) L4) and still runs three passes (L7-L10).

## Set `before`: the rule as committed in `0fa756e`

Machine, versions and the exact command: [before/machine.txt](before/machine.txt) (4 CPUs,
Go 1.22.12, k6 v0.56.0, journeys `examples/shelf/journeys.example.json`, 2 VUs and 10
iterations per VU). Nothing else ran on the machine during these runs. Raw figures per step
and pass: [before/passes.tsv](before/passes.tsv). Findings that are not noise (every other finding was noise; summary.txt counts each class):
[before/findings.tsv](before/findings.tsv). Summary: [before/summary.txt](before/summary.txt).

| Measured | Value | Source |
|---|---|---|
| Runs, all completed three passes | 20 | summary.txt L1 |
| Runs with at least one false regression | 0 of 20 | summary.txt L2 |
| False regressions per latency finding (p50, p95, mean; 440 each) | 0 | summary.txt L8-L10 |
| Same, if the 10 ms absolute floor were removed (band and 20% rules only) | p95 52 (11.8%), p50 43 (9.8%), mean 38 (8.6%) | summary.txt L8-L10, column "no floor" |
| Runs with at least one false regression if the floor were removed | 19 of 20 | summary.txt L3 |
| Withheld findings | 0 | summary.txt L6-L11 |
| B1/B2 spread, p95 (median, p90, max over 440 steps) | 32.5%, 105.8%, 391.8% | summary.txt L10 |
| B1/B2 spread, app CPU seconds | 6.7%, 16.0%, 21.4% | summary.txt L6 |
| B1/B2 spread, peak RSS | 0.4%, 1.1%, 1.3% | summary.txt L11 |

What this shows: on this machine the rule gives no false regression, but only because of the
absolute floor. Every step of the demo takes a few milliseconds, so no difference between
identical builds reaches 10 ms. The band and relative rules by themselves would flag about one
latency finding in ten, and almost every run. The same floor means a real latency regression
smaller than 10 ms is never reported on this app. App CPU per pass is 0.13 to 0.15 s
(passes.tsv, "app" rows), so the 10 ms resolution of `/proc/<pid>/stat` alone is about 7% of
it.

What it does not show: the noise on the GitLab runner or in the Duo sandbox (the runner's
clean-change runs are in [../runs.md](../runs.md)), or noise on a slower machine, where the
laptop runs in [../runs.md](../runs.md) did produce false regressions.
