# Engine runs

Raw output of the engine CLI (`engine/cmd/downstream`) run end to end on the demo app
[`examples/shelf`](../examples/shelf/). These are CI and local runs of the engine. They are
not runs of the Duo flow: no flow session, merge request note or issue is involved.

The candidate in every run is the pipeline commit plus one patch from
[`examples/changes`](../examples/changes/): `raise-hash-cost` is a planted regression,
`clean-change` touches only `GET /health` and should come out clean. Each run is the
protocol B1, C, B2 (baseline twice, candidate once) with 2 VUs and 10 iterations per VU,
so each step's p95 comes from 20 samples (`count` in `run.json`).

In `summary.txt`, `regression=` counts issue groups, not steps.

## Pipeline 63: `engine-e2e` on GitLab CI

Commit `0fa756e`, [pipeline 2978805738](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase/-/pipelines/2978805738),
runner `saas-linux-small-amd64`, image `golang:1.22`, k6 v0.56.0.

| Change | Result | Source |
|---|---|---|
| `raise-hash-cost` (planted) | 22 regressed steps in 2 issue groups; `log_in` p95 2.8 / 1.6 ms (B1 / B2) to 537.9 ms; app CPU time 0.16 s to 27.10 s | [job log](runs/ci-63-engine-e2e/raise-hash-cost-job-log.txt) L77, L206; [report](runs/ci-63-engine-e2e/raise-hash-cost/report.md) L5, L12, L53 |
| `raise-hash-cost` blast radius | the 7 routes that reach `hashSecret`, 5 of them through `requireAuth` | [report](runs/ci-63-engine-e2e/raise-hash-cost/report.md) L90-96 |
| `raise-hash-cost` issues | one group via `hashSecret` (19 steps) and one "outside the diff's call paths" (3 steps) | [issues.json](runs/ci-63-engine-e2e/raise-hash-cost/issues.json) L5, L31 |
| `clean-change` (control) | 0 regressions, 0 withheld | [job log](runs/ci-63-engine-e2e/clean-change-job-log.txt) L77, L80, L197; [report](runs/ci-63-engine-e2e/clean-change/report.md) L5 |

The second issue group is a side effect of the planted change: `GET /books`,
`GET /books/{id}` and `GET /books/{id}/reviews` do not call `hashSecret`, but they share the
CPU with requests that do. The engine reports it as a separate issue, so this change gives
two issues for one cause.

## Pipeline 66: `engine-noise`, clean change only

Commit `baacc58`, [pipeline 2979381062](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase/-/pipelines/2979381062)
on the `measure-noise` branch. 5 parallel jobs, 4 runs each, all on
`saas-linux-small-amd64` ([noise-1 log](runs/ci-66-engine-noise/noise-1-job-log.txt) L2, L27).

| Result | Source |
|---|---|
| 20 of 20 runs: `regression=0`, `withheld=0`, `passes=3` | each `noise-<k>-job-log.txt` in [ci-66-engine-noise](runs/ci-66-engine-noise/) L72, L84, L96, L108; `Job succeeded` L119 |
| Largest p95 increase of any step over the worse baseline, across 440 step comparisons: 4.6 ms (the regression floor is 10 ms) | `noise-<k>/report-<i>.md`, Change column |

Check the largest increase with:

```
cat evidence/runs/ci-66-engine-noise/noise-*/report-*.md | grep -oE '\| \+[0-9.]+ ms' | sort -t+ -k2 -n | tail -1
```

## Local WSL2 runs, clean change only

A developer laptop (Windows 11, WSL2 Ubuntu 24.04) with Go 1.22.12 and k6 v0.56.0, the same
versions as CI. Step latencies here are 5 to 30 ms instead of 1 to 5 ms on the runner.

| Set | Runs | Flagged as regression | Source |
|---|---|---|---|
| [`loop/`](runs/local-wsl-clean-change/loop/) (the `engine-e2e` commands in a loop) | 8 | 3 (runs 2, 3, 6) | `log-<i>.txt` L13, `report-<i>.md` |
| [`job-script/`](runs/local-wsl-clean-change/job-script/) (the `engine-noise` script) | 4 | 3 (runs 2, 3, 4) | `summary-<i>.txt`, `report-<i>.md` |

Every one of these is a false positive: the clean change does not touch the measured routes.
In the flagged reports a few steps of the candidate run are 10.2 to 72.1 ms slower while the
other steps of the same run are not. With 20 samples, a p95 is set by the slowest one or two
requests, so a short stall on a busy machine is enough to pass the 10 ms and 20% thresholds. Five more local runs were made but their output was overwritten and is not
counted here.

What this shows: on the GitLab runner the clean control has wide margin; on a slower, noisier
machine the same thresholds give false positives. What it does not show: the noise inside the
Duo Agent Platform sandbox, which has not been measured.

## Redactions

The only alteration to the job logs is replacing the CI git identity's placeholder address
with `<redacted-email>` (line 52 of each `*-job-log.txt`), so `scripts/check_evidence.py`
accepts them. Artifacts under `out/` are unchanged.
