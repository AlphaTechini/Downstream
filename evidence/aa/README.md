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

Each set's `machine.txt` records the commit SHA as it was when the runs were made; find the
commit on this branch by the subject named under each set. `run` accepts the same SHA twice; the runner builds it once
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
smaller than 10 ms is never reported on this app. App CPU per pass is 0.13 to 0.21 s
(passes.tsv, "app" rows), so the 10 ms resolution of `/proc/<pid>/stat` alone is 5% to 8% of
it.

What it does not show: the noise on the GitLab runner or in the Duo sandbox (the runner's
clean-change runs are in [../runs.md](../runs.md)), or noise on a slower machine, where the
laptop runs in [../runs.md](../runs.md) did produce false regressions.

## Set `counters`: the same rule, with the process counters

At the commit "Count bytes written, write syscalls, fds, threads and completed journeys".

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

## Set `after`: the gated rule

At the commit "Rerun the A/A check under the gated rule" (engine as of "Decide regressions
on process counters and keep latency advisory").

Same machine and procedure ([after/machine.txt](after/machine.txt)), 20 fresh runs (not the
runs the tolerances were picked from), nothing else running. Summary:
[after/summary.txt](after/summary.txt); raw figures: [after/passes.tsv](after/passes.tsv);
findings that are not noise (all others were noise): [after/findings.tsv](after/findings.tsv). Only gated findings
can be a regression now; latency is advisory.

| Measured | before (old rule) | after (gated rule) | Source |
|---|---|---|---|
| Runs with at least one false regression | 0 of 20 | 0 of 20 | before/summary.txt L2, after/summary.txt L2 |
| False regressions among gated findings (CPU per journey, bytes and write syscalls per journey, fds, threads, peak RSS, failures; 560 findings) | - | 0 | after/summary.txt L6, L8, L10, L13-L16 |
| Withheld findings | 0 | 0 | before/summary.txt L6-L11, after/summary.txt L6-L16 |
| Largest excess of C over the worse baseline, CPU per journey (tolerance 20%) | 11.17% (counters set) | 13.96% | counters/summary.txt L14, after/summary.txt L19 |
| Largest excess, bytes written per journey (tolerance 1%) | 0.35% (counters set) | 0.48% | counters/summary.txt L15, after/summary.txt L20 |
| Largest excess, write syscalls per journey (tolerance 4%) | 1.51% (counters set) | 1.51% | counters/summary.txt L16, after/summary.txt L21 |
| Largest excess, peak RSS (tolerance 2%) | 0.93% (counters set) | 0.81% | counters/summary.txt L19, after/summary.txt L24 |

The old rule's zero came from the 10 ms latency floor (see `before`); the new rule's zero
comes from tolerances that sit above the largest A/A excess. The margin is smallest for CPU
per journey (13.96% observed against 20% allowed), so that tolerance is the most likely to
give a false regression on a noisier machine. Neither set says anything about the GitLab
runner or the Duo sandbox.

## Set `runtime`: the gated rule with the runtime counters from the build overlay

At the commit "Read allocations, GC cycles and goroutines through a build overlay". Same
machine and procedure ([runtime/machine.txt](runtime/machine.txt)), 20 runs, nothing else
running. Summary: [runtime/summary.txt](runtime/summary.txt). The tolerances for the four
runtime counters were first picked from 10 earlier runs that are not committed; these 20 runs
are the committed check of them.

| Measured | Value | Source |
|---|---|---|
| Runs with at least one false regression | 0 of 20 | summary.txt L2 |
| Largest excess, allocations per journey (tolerance 10%) | 4.18% | summary.txt L31 |
| Largest excess, alloc bytes per journey (tolerance 5%) | 1.70% | summary.txt L32 |
| Goroutines (tolerance +1), B1/B2 spread and excess | 0, 0 | summary.txt L33 |
| GC cycles per journey | 0 in every pass: a pass allocates too little to start a GC cycle, so this counter cannot show anything on the demo | summary.txt L11 (no spread) |
| Largest excess, CPU per journey (tolerance 20%) | 16.85% | summary.txt L23 |
| Largest excess, write syscalls per journey (tolerance 4%) | 3.18% | summary.txt L25 |
| CPU per journey findings withheld or improved on identical code | 2 withheld, 1 improved, of 20 | summary.txt L8 |

The margins for CPU per journey and write syscalls per journey are thinner than in the
`after` set (13.96% and 1.51%). These two tolerances are the ones most likely to give a false
regression on another machine.

## Set `final`: window CPU and GC cycles per pass

At the commit "Fix the review findings in the engine, flow and docs": CPU per journey is
measured over the k6 window from `/proc/<pid>/task/*/schedstat`, and GC cycles are judged per
pass. Same machine and procedure ([final/machine.txt](final/machine.txt)), 20 runs, nothing
else running. Summary: [final/summary.txt](final/summary.txt). Peak RSS was still allowed 2%
in these runs; it is allowed 5% since the next commit, which can only remove regressions, not
add them.

| Measured | Value | Source |
|---|---|---|
| Runs with at least one false regression | 0 of 20 | summary.txt L2 |
| Withheld findings | 0 | summary.txt L6-L20 |
| Largest excess, CPU per journey over the k6 window (tolerance 20%) | 8.11% | summary.txt L23 |
| B1/B2 spread, CPU per journey over the k6 window, max | 12.92% | summary.txt L23 |
| Largest excess, write syscalls per journey (tolerance 4%) | 1.07% | summary.txt L25 |
| Largest excess, allocations per journey (tolerance 10%) | 4.58% | summary.txt L31 |
| GC cycles per pass | 0 in every pass (no spread) | summary.txt L11 |

Window CPU is steadier than the whole-process CPU of the `runtime` set (largest excess 16.85%,
spread 32.96%), because process start-up and shutdown are no longer counted.
