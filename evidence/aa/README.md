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

## Set `counters`: the same rule, with the process counters added in `b57fe87`

Same machine and procedure ([counters/machine.txt](counters/machine.txt)), 20 runs, nothing
else running. Raw figures: [counters/passes.tsv](counters/passes.tsv) ("app" rows hold the
counters per pass). Summary: [counters/summary.txt](counters/summary.txt). "Excess" is how far
C lands above the worse of B1 and B2, as a fraction of that baseline; on identical code its
largest value is the smallest tolerance that would have given no false regression in these
runs.

| Counter | B1/B2 spread, max | Excess, p90 | Excess, max | Source |
|---|---|---|---|---|
| CPU ms per completed journey | 17.46% | 6.85% | 11.17% | summary.txt L14 |
| Bytes written per completed journey | 0.61% | 0.03% | 0.35% | summary.txt L15 |
| Write syscalls per completed journey | 3.40% | 0.88% | 1.51% | summary.txt L16 |
| Open file descriptors | 0.00% | 0.00% | 0.00% | summary.txt L17 |
| Threads | 11.11% | 1.11% | 11.11% | summary.txt L18 |
| Peak RSS | 1.33% | 0.36% | 0.93% | summary.txt L19 |

The rule as committed still gave 0 false regressions in 20 runs (summary.txt L2). CPU per
journey is far noisier here than the tolerance first proposed for it (3 to 4%): each pass uses
only about 0.15 s of CPU, so start-up and scheduling are a large share of it. Bytes written per
journey are the steadiest counter, but not exact: the app's responses are likely to differ in
length between passes because they carry generated tokens and IDs (not checked). A thread count of 9 or 10 moves by one thread, which is
11%, so threads need an absolute tolerance rather than a relative one.
