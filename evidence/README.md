# Evidence

Raw material behind the claims in the top-level README. Nothing here is a mock-up.

| File | What it is |
|---|---|
| [day0-tests.md](day0-tests.md) | Results table for the day-0 platform tests, with line references |
| [day0/run1-probe-v1-job-log.txt](day0/run1-probe-v1-job-log.txt) | CI job log of a flow session (probe v1, stopped after one action) |
| [day0/run2-probe-v2-job-log.txt](day0/run2-probe-v2-job-log.txt) | Probe v2, five-step run |
| [day0/run3-probe-v3-job-log.txt](day0/run3-probe-v3-job-log.txt) | Probe v3, k6 install, network and timeout checks |
| [day0/probe-notes.txt](day0/probe-notes.txt) | Transcription of the notes the probe posted (owner's copy-paste, not a raw log) |
| [gitlab-project-2026-10-22.json](gitlab-project-2026-10-22.json) | GitLab API record of the showcase project (visibility, default branch) |
| [runs.md](runs.md) | Results of the engine runs below, with line references |
| [runs/ci-63-engine-e2e/](runs/ci-63-engine-e2e/) | Pipeline 63 `engine-e2e` job logs and `out/` artifacts for the planted and the clean change |
| [runs/ci-66-engine-noise/](runs/ci-66-engine-noise/) | Pipeline 66 `engine-noise`: 5 job logs, 20 clean-change runs |
| [runs/local-wsl-clean-change/](runs/local-wsl-clean-change/) | 12 clean-change runs on a WSL2 laptop, kept as evidence of false positives on a noisy machine |
| [aa/](aa/) | A/A runs (same commit as base and head): false regressions and counter noise on identical code, with the scripts that produce them ([aa/README.md](aa/README.md)) |
| [plants/](plants/) | Planted regressions and the clean control, five runs each ([plants/README.md](plants/README.md)) |
| [SHA256SUMS](SHA256SUMS) | SHA-256 of each evidence file above; checked by `make verify` |

The runs under `runs/` are engine CLI runs in CI and on a laptop. The logged end-to-end flow
runs required by `docs/requirements.md` (a flow session ending in an MR note) do not exist yet.
