# Downstream

**Before a reviewed change reaches users, Downstream runs realistic user traffic through the
old and new builds, finds what else the change quietly broke or slowed, traces the likely
cause, and files a fix.** It is a GitLab Duo Agent Platform flow. *Merged is not safe.*

**Demo video (under 3 minutes): [youtu.be/I2ALGSSF1vM](https://youtu.be/I2ALGSSF1vM).** GitLab's
Duo Developer agent builds two features on NewGit, Downstream finds what they slowed down, and
`fix it` applies a fix and re-measures it.

**The mechanism, in one sentence:** a change is blamed only for what exceeds the disagreement
between two runs of the old build, measured in the same session.

For: maintainers and reviewers of web/API services on GitLab, especially small teams with no
staging environment that replays real user behaviour.

> **Status, 2026-10-24: the flow runs end to end on GitLab.** A mention on a merge request
> builds both versions in the Duo sandbox, runs model-written journeys with k6, posts the
> engine's report and opens one issue per cause; `fix it` on an issue commits the fix on a new
> branch, opens a merge request for the requester and re-measures it. Shown on the demo app in
> this project and on [NewGit](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/newgit),
> a separate project, with the same flow ([evidence/flow.md](evidence/flow.md)). Regressions are
> decided on step failures and process counters (CPU per completed journey, writes, memory,
> allocations, goroutines), each with a tolerance; latency is reported but advisory. With that
> rule the clean change came out clean in 3 of 3 flow runs and the planted and new-feature
> changes were caught. In CI an earlier, latency-based rule passed the clean control in 22 of 22
> runs ([evidence/runs.md](evidence/runs.md)). With the current tolerances the clean change has
> run 4 times in the sandbox, all clean (flow 1.0.12 and 1.0.13): a small sample, not a measured
> rate. The claims table says exactly what has been shown.
>
> **Frontend stage (new, 2026-10-24):** when a change touches pages, templates or a frontend
> build, Downstream also renders the old and new pages in happy-dom (no browser) and reports
> bundle growth with the asset responsible, missing assets, DOM growth, new accessibility and
> markup errors, uncaught JavaScript errors and broken page steps. Shown in the Duo sandbox on a
> NewGit template change, and locally on real React and Svelte (Vite) builds
> ([evidence/frontend/README.md](evidence/frontend/README.md)). Color contrast, layout and paint
> are never checked.

## Engineering highlights

Downstream is not a prompt wrapped around a chat model. It is a measurement engine (Go,
standard library only) driven by an agent, built so that every number it posts can be trusted
and traced. **For more details, visit [technical_implementation.md](technical_implementation.md)**,
a plain-English walkthrough of the architecture, the design decisions and the real-world
scenarios it handles.

- **It measures its own noise in every run.** The old build runs twice, before and after the
  new one, so machine drift is counted as noise instead of blamed on the change
  ([`engine/runner`](engine/runner/)).
- **It decides on what the process did, not on jittery latency.** On identical code the two
  baseline runs differed by 32.5% in step latency at the median
  ([A/A summary](evidence/aa/before/summary.txt) L10), while the new run never exceeded the
  worse baseline by more than 0.35% in bytes written or 1.51% in write syscalls
  ([counters](evidence/aa/counters/summary.txt) L15-L16). So verdicts are gated on CPU per
  journey, bytes written, syscalls, memory, file descriptors, threads and Go runtime counters
  read from `/proc` and from a file injected at build time with `go build -overlay`, without
  editing the target repository ([`engine/runner`](engine/runner/), [`engine/classify`](engine/classify/classify.go)).
- **Tolerances are calibrated, then hardened on the live bot.** Each metric's tolerance came
  from A/A runs; each floor was added in its own merge request after a real false alarm. On the
  current version a clean change came out clean in 3 of 3 live runs while planted and feature
  regressions were caught ([C-18](docs/CLAIMS.md)).
- **It follows the change past the diff.** A Go AST scan maps routes to handlers and walks
  calls to find routes that share code with the changed ones, then requires journeys that reach
  all of them, plus controls elsewhere ([`engine/scan`](engine/scan/), [`engine/journey`](engine/journey/coverage.go)).
- **It checks frontends too, without a browser.** Pages are rendered in happy-dom with the
  app's own JavaScript, on the old build twice and the new build once; bundle growth is traced
  to the asset responsible, and findings are grouped into one issue per cause. Shown in the Duo
  sandbox on a server-rendered app and on real React and Svelte builds
  ([`tools/fecheck`](tools/fecheck/README.md), [C-19, C-20](docs/CLAIMS.md)).
- **It remembers, on Google Cloud.** A sandbox forgets everything after each run, so ten merge
  requests that each add 8% CPU would all pass. The Downstream memory service on Cloud Run keeps
  a numbers-only record of each run, and every report gains a Trend section that compounds the
  changes of merged merge requests ([`cloud/`](cloud/README.md), [C-22](docs/CLAIMS.md)).
- **It closes the loop, under human control.** `fix it` commits a fix on a new branch, opens a
  merge request for the requester and re-measures it, for backend and frontend issues alike;
  Downstream never merges ([C-13](docs/CLAIMS.md), [C-21](docs/CLAIMS.md)).
- **The model never holds the keys.** It writes validated JSON and sentences; every command is
  fixed in the flow, every figure comes from the engine, the engine is pinned outside the merge
  request, and traffic stays on `127.0.0.1` ([`flows/downstream.yml`](flows/downstream.yml)).
- **It runs where GitLab runs it.** Built and proven inside the Duo sandbox, working around its
  PID namespace, read-only home, missing `TMPDIR`, blocked clones and 200 s command limit
  ([evidence/flow.md](evidence/flow.md)).
- **Every claim is checked by a script.** `make verify` fails if a Verified row lacks committed
  evidence or a raw evidence file listed in [`evidence/SHA256SUMS`](evidence/SHA256SUMS) changed
  since it was recorded.

Code entry points: the flow is [`flows/downstream.yml`](flows/downstream.yml), the engine's CLI
is [`engine/cmd/downstream/main.go`](engine/cmd/downstream/main.go) (Go), and the frontend
checker is [`tools/fecheck/fecheck.mjs`](tools/fecheck/fecheck.mjs). The React and Svelte apps
under [`fixtures/frontend/`](fixtures/frontend/README.md) are test fixtures for the frontend
stage, not the product.

## 30-second check

No keys, accounts or installs beyond Python 3 with PyYAML (preinstalled on many Linux systems):

```bash
git clone https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase.git && cd showcase
python3 scripts/validate_yaml.py && python3 scripts/check_evidence.py
```

Expected output:

```text
ok: 2 flow file(s), 1 agent config file(s)
ok: links resolve, verified claims cite evidence, evidence is clean, checksums match
```

If `import yaml` fails, install PyYAML in a virtualenv first (`python3 -m venv .venv && .
.venv/bin/activate && pip install pyyaml`; on Debian or Ubuntu this needs the `python3-venv`
package). `make verify` runs the same two commands. More checks with Go, k6 or Node are in
[Deeper checks](#deeper-checks).

## Built for GitLab Transcend: Life After Code

Requirements below come from the Devpost Rules as read during research on 2026-10-21/22. They
have not been re-read against the live page since and are to be re-checked before submission.

| Brief requirement | Where in this repo | Status |
|---|---|---|
| Genuine use of Duo Agent Platform (agents, flows, MCP) | [`flows/downstream.yml`](flows/downstream.yml), a public custom flow in the AI Catalog with a mention trigger; [`flows/probe.yml`](flows/probe.yml) was the day-0 feasibility flow | Verified: full sessions on two projects ([evidence/flow.md](evidence/flow.md)) |
| Public MIT-licensed GitLab project | [`LICENSE`](LICENSE); this is the [GitLab showcase project](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase) where the flow runs | Public, and GitLab detects the MIT licence, per the GitLab API on 2026-10-24 ([record](evidence/gitlab-project-2026-10-24.json)) |
| Visible CI/CD pipeline history | GitLab agent-session pipelines; copies in [`evidence/day0/`](evidence/day0/). [`.gitlab-ci.yml`](.gitlab-ci.yml) runs `make verify`, the Go tests, and `engine-e2e`, which runs the engine end to end on the demo app for the planted and the clean change; `engine-noise` repeats the clean change on the `measure-noise` branch | 3 probe sessions; engine CI runs from pipelines 63 and 66 in [`evidence/runs/`](evidence/runs/); flow sessions in [`evidence/flow/`](evidence/flow/) |
| Demo video under 3 minutes | [Downstream demo on YouTube](https://youtu.be/I2ALGSSF1vM); script in [`docs/userflow.md`](docs/userflow.md) section 7 | Recorded 2026-10-25 on NewGit !12 and !13, with the fix merge requests !14 and !15 |
| One declared autonomy tier: **Supervised** | the flow never merges; a human picks a fix option, the flow opens a merge request for it, and a human reviews and merges | Verified: `fix it` opened !13 with the requester as reviewer and did not merge ([evidence/flow.md](evidence/flow.md)) |
| Path A (new project) | first commit 2026-10-22 (`git log`) | Intended path |

Judging criteria (from the earlier GitLab AI Hackathon; re-check against the live rules) and
where each is answered:

| Criterion | Where to look |
|---|---|
| Technical implementation, including Duo Agent Platform tools, triggers and context | [technical_implementation.md](technical_implementation.md); [Duo Agent Platform features used](#duo-agent-platform-features-used); the flow [`flows/downstream.yml`](flows/downstream.yml) (mention trigger, `run_command`, notes, issues, commits, merge requests); the claims table with sandbox logs |
| Design and usability | One mention to start; a plain-English journeys note before any numbers; one issue per cause with numbered fix options; `fix it` to apply one; setup in [Use Downstream on your project](#use-downstream-on-your-project) |
| Potential impact | [The problem](#the-problem) (change-caused outages, sourced); [scenarios](technical_implementation.md#3-real-projects-it-is-built-for) for APIs, server-rendered apps, single-page apps and full-stack repositories |
| Idea quality | [The mechanism](#downstream) above; prior art and what is not novel in [`docs/details.md`](docs/details.md) section 13 |

Google Cloud (bonus): the Downstream memory service and the NewGit demo app run on Cloud Run,
with the deployment code in [`cloud/`](cloud/README.md). Live: [memory service and dashboard](https://downstream-memory-1083478229816.us-central1.run.app),
[NewGit](https://newgit-1083478229816.us-central1.run.app). Proof: [evidence/gcp/](evidence/gcp/README.md).

Not claimed: coverage of all nine lifecycle stages, the environmental prize.

## The problem

Review is the last routine human check before a change ships, and changes are where outages
come from. Google's SRE book: "SRE has found that roughly 70% of outages are due to changes in
a live system" ([ch. 1, Change Management](https://sre.google/sre-book/introduction/)). In
Google's postmortem sample for 2010-2017, binary pushes were 37% and configuration pushes 31%
of outage triggers ([SRE Workbook, Table C-1](https://sre.google/workbook/postmortem-analysis/)).
Both are Google-specific.

Our argument, which is not measured here: a diff shows what changed, not what the change does
to routes the author never opened. That gap is what Downstream targets.

Prior art: Kassandra (an earlier Transcend entry, k6 on a merge request), GitLab Load Performance
Testing (a fixed k6 script, paid tiers) and Diffy-style replay (old versus new on the same
traffic). Downstream does not claim to invent running k6 on a merge request; the claim is the
combination of a noise-controlled baseline, the blast radius, model-written journeys, one issue
per cause and the `fix it` loop ([`docs/details.md`](docs/details.md) section 13).

## How it works

| Step | What happens | Status |
|---|---|---|
| 1 | One mention on a merge request starts the flow | Verified in flow sessions ([evidence](evidence/flow.md)) |
| 2 | It maps changed routes, routes that share code with them (blast radius) and routes the merge request adds | Verified: `downstream scan` ([`engine/scan`](engine/scan/)), Go `net/http` only ([evidence](evidence/flow.md)) |
| 3 | The model writes journeys that cover the change, up to 20: existing journeys reaching every changed and affected route, new journeys for every new route, and control journeys elsewhere; the engine rejects a set that leaves a route out. They are shown to the reviewer in plain English first | Verified: coverage rules in [`engine/journey`](engine/journey/coverage.go), model-written journeys in every full session ([evidence](evidence/flow.md)) |
| 4 | k6 runs them on the old build twice and the new build once, so noise is measured; new-feature journeys run in the new build's pass alongside the others, so existing pages are measured while the feature is in use | Verified in the sandbox ([`engine/runner`](engine/runner/), [`engine/k6gen`](engine/k6gen/)) |
| 5 | Only differences beyond the noise and a per-metric tolerance open issues: failing steps, and process counters (CPU per completed journey, bytes written net of responses, write syscalls, memory, fds, threads, allocations, GC, goroutines). Latency is shown but advisory. One issue per cause (repro, likely cause, numbered fix options, starter tagged); trade-offs become a question on the MR | Verified: issues opened by the flow. Under the gated rule the planted change gives 1 issue (CPU per journey); the earlier latency rule gave 2 for 1 cause. A trade-off question was posted in session 10002748 ([evidence](evidence/flow.md)) |
| 6 | `@handle fix it` (option 1) or `fix option N` commits the option on a new branch, opens a merge request mentioning the requester and re-runs the issue's journey on it; `verify` reruns after merge | `fix it` verified (!13, [evidence](evidence/flow.md)); `verify` written, not run |
| 7 | When frontend files change, the model writes page journeys and the engine builds both versions, renders each page in happy-dom (old build twice, so noise is measured) and adds a Frontend section to the report, with one issue per cause | Verified in the sandbox on a server-rendered app (NewGit !5, [evidence](evidence/flow.md)); React and Svelte builds verified locally only ([evidence](evidence/frontend/README.md)) |
| 8 | For projects that opt in, each run's numbers-only record goes to the Downstream memory service on Google Cloud Run, and the report adds a Trend: each metric's change compounded over the merged merge requests, flagged as creep at 25% | Verified in the sandbox on NewGit: the trend on !3 counts merged !11 ([evidence](evidence/gcp/README.md)) |

User-activity simulation, not load testing: a few virtual users for tens of seconds. The model
chooses journeys and writes sentences; code produces every number, table and chart.
Design: [`docs/details.md`](docs/details.md). What reviewers see: [`docs/userflow.md`](docs/userflow.md).
Entry points: the flow is [`flows/downstream.yml`](flows/downstream.yml); the engine's CLI is
[`engine/cmd/downstream/main.go`](engine/cmd/downstream/main.go); the demo app starts from
[`examples/shelf/cmd/shelf/main.go`](examples/shelf/cmd/shelf/main.go).

## Deeper checks

With Go 1.22 or newer and k6 on PATH, `make test` also runs gofmt, go vet and the engine's tests,
including the integration tests that start an app and run k6 (they skip without k6).
On Linux with Node 20.19 or newer and npm, `make verify-frontend` builds the React and Svelte
fixtures and runs the frontend stage on a planted and a clean change (about 2 minutes; it
downloads the fixtures' npm packages).

`make verify` validates the flow and config YAML, checks that docs links resolve, that every Verified
row above links to committed evidence, that logs hold no unredacted email, and that the
evidence files match the checksums in `evidence/SHA256SUMS`. It does not run
Downstream; the logs in `evidence/` show the platform behaviour. Flow runs happen in this
project; their pipelines are under CI/CD > Pipelines.

## Measured results

Flow sessions on GitLab, from [`evidence/flow/`](evidence/flow/) (details in
[evidence/flow.md](evidence/flow.md)):

| Measurement | Value | Source |
|---|---|---|
| Planted `hashSecret` change (!11): regressed steps, app CPU time B1 / C / B2 | 16 of 16; 0.11 / 17.69 / 0.12 s | [mr-11-notes.json](evidence/flow/mr-11-notes.json) L701 |
| `fix it` re-measure (!13 against !11): logged-in steps p95, app CPU time | about 66 ms to 0.2-0.6 ms; 8.80 / 2.37 / 8.86 s, 0 regressions | [mr-13-notes.json](evidence/flow/session-10000200/mr-13-notes.json) L33 |
| New similar-books route (!14): existing steps regressed while it is in use; app CPU time | 8 (`shelf_list` 2.1 ms to 129.4 ms p95); 0.10 / 1.72 / 0.11 s | [mr-14-notes.json](evidence/flow/mr-14-notes.json) L381 |
| NewGit avatar change (NewGit !3), earlier latency rule: regressed steps, app CPU time | 10, controls included; 0.32 / 1.50 / 0.37 s | [mr-3-notes.json](evidence/flow/newgit-session-10000842/mr-3-notes.json) L785 |
| Gated rule, clean change: runs with an issue | 0 of 3 | [gated/mr-16-notes.json](evidence/flow/gated/mr-16-notes.json) L1273, L1475, L1677 |
| Gated rule, planted change: CPU per completed journey, B1 / B2 / C | 2.87 / 2.91 / 451.25 ms | [gated/mr-11-notes.json](evidence/flow/gated/mr-11-notes.json) L1991 |
| Gated rule, new feature: CPU per completed journey, B1 / B2 / C | 3.09 / 3.04 / 94.96 ms | [gated/mr-14-notes.json](evidence/flow/gated/mr-14-notes.json) L1211 |
| Floors (flow 1.0.12), clean change: runs with an issue | 0 of 3 | [floors/mr-16-notes.json](evidence/flow/floors/mr-16-notes.json) L1127, L1329, L1531 |
| Floors (flow 1.0.12), NewGit avatar change: CPU per completed journey, B1 / B2 / C | 10.38 / 9.75 / 32.28 ms | [floors/newgit-mr-3-notes.json](evidence/flow/floors/newgit-mr-3-notes.json) L409 |
| Frontend stage, NewGit template change (experiment bot / live bot 1.0.13): page regressions, issues | 12 on 4 pages, 3 / 15 on 5 pages, 3 | [newgit-mr-5-notes.json](evidence/flow/frontend/newgit-mr-5-notes.json) L151; [live-1.0.13-newgit-mr-5-notes.json](evidence/flow/frontend/live-1.0.13-newgit-mr-5-notes.json) L151 |

Frontend stage on real builds, local WSL2 machine ([frontend-verify.txt](evidence/frontend-verify.txt)):

| Measurement | Value | Source |
|---|---|---|
| React 19.3.1 / Svelte 5.57.2 (Vite 8.3.5), planted change: regressed check kinds | 6 of 6 / 6 of 6 | L61, L121 |
| React planted change: JS bytes (gzip) and the chunk named | 68814 to 73929 (+7.4%), `Reports-CMCplyA2.js` +5112 | L23 |
| Svelte planted change: JS bytes (gzip) and the chunk named | 14518 to 20268 (+39.6%), `Reports-Duhjbbda.js` +5184 | L83 |
| Clean text change, both apps: regressions | 0 / 0 | L47, L107 |

Engine runs in GitLab CI on the demo app, from [`evidence/runs/`](evidence/runs/) (one run per
row unless stated; details in [evidence/runs.md](evidence/runs.md)):

| Measurement | Value | Source |
|---|---|---|
| Planted change: regressed steps | 22 of 22, in 2 issue groups | [report](evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) L5 |
| Planted change: `log_in` p95, B1 / B2 / candidate | 2.8 / 1.6 / 537.9 ms | [report](evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) L12 |
| Planted change: app CPU time, B1 / candidate / B2 | 0.16 / 27.10 / 0.16 s | [report](evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) L53 |
| Clean change: runs flagged as regression | 0 of 22 (pipelines 63 and 66) | [pipeline 63](evidence/runs/ci-63-engine-e2e/clean-change-job-log.txt) L77; [pipeline 66](evidence/runs/ci-66-engine-noise/) |
| Clean change: largest step p95 increase over 20 runs | 4.6 ms (regression floor 10 ms) | `evidence/runs/ci-66-engine-noise/noise-*/report-*.md` |
| A/A (same commit as base and head), 4-CPU dev machine: runs with a false regression | 0 of 20; 19 of 20 if the 10 ms floor were removed | [evidence/aa/README.md](evidence/aa/README.md), [summary](evidence/aa/before/summary.txt) L2-L3 |
| A/A: B1/B2 spread of step p95 (median, p90) | 32.5%, 105.8% | [summary](evidence/aa/before/summary.txt) L10 |
| A/A: largest excess of C over the worse baseline, 20 runs: CPU per journey / bytes written per journey / write syscalls per journey | 11.17% / 0.35% / 1.51% | [summary](evidence/aa/counters/summary.txt) L14-L16 |
| A/A with the gated rule (latency advisory), 20 fresh runs: runs with a false regression | 0 of 20 (560 gated findings) | [summary](evidence/aa/after/summary.txt) L2, L6-L16 |
| Gated rule, 5 runs each, 4-CPU dev machine: planted `raise-hash-cost` / planted `remove-list-limit` / clean control (two journey sets) | regression in 5 of 5 / 5 of 5 / 0 of 10 | [evidence/plants/README.md](evidence/plants/README.md) |
| `remove-list-limit` (scripted edit): bytes written per completed journey | +58.10% to +59.80% | [table](evidence/plants/remove-list-limit-thorough/table.txt) |
| A/A with runtime counters from the build overlay (allocations, alloc bytes, GC cycles per journey, goroutines), 20 runs: runs with a false regression | 0 of 20 | [summary](evidence/aa/runtime/summary.txt) L2, L31-L33 |

The platform, from the logs in `evidence/day0/`:

| Measurement | Value | Source |
|---|---|---|
| Runner | `saas-linux-small-amd64`, 2 CPUs, 8145540 kB memory, 18G free in `/tmp` | [run1](evidence/day0/run1-probe-v1-job-log.txt) L1-2; [probe-notes](evidence/day0/probe-notes.txt) |
| Probe v1 (stopped after one action), whole CI job | 45.4 s | [run1](evidence/day0/run1-probe-v1-job-log.txt) L1, L564 |
| Probe v2, sandbox start to FINISHED | 21.5 s | [run2](evidence/day0/run2-probe-v2-job-log.txt) L114, L529 |
| Probe v3 (four commands, one a deliberate 150 s sleep), sandbox start to FINISHED | 221.8 s | [run3](evidence/day0/run3-probe-v3-job-log.txt) L118, L599 |
| Sandbox network allowlist with our config | 175 entries (the default list has 5) | [run3](evidence/day0/run3-probe-v3-job-log.txt) L91 |

## Architecture and file paths

```text
mention on an MR
 -> flows/downstream.yml              one AgentComponent, one prompt (verified in flow sessions)
     fetches a pinned commit          of this repo (GitHub mirror tarball), builds the engine,
                                      reads agentnotes.md; one flow serves any project
     calls fixed engine commands      engine/cmd/downstream: scan, schema, validate, run, report,
                                      summary, issues, marker, fixcheck, frontend (CI runs in
                                      evidence/runs.md, flow runs in evidence/flow.md)
       builds base and head from git, runs the app on 127.0.0.1
       k6 v0.56.0                     installed by .gitlab/duo/agent-config.yml (verified)
       Go 1.22.12                     already in the sandbox image (verified), nothing to install
       frontend stage                 tools/fecheck (happy-dom, axe-core, html-validate) on
                                      Node 20.20.2 in the sandbox; old build twice, new once
 -> MR notes and issues; each issue ends with a "Downstream state" block so a reply
    (fix it, verify) rebuilds its state from the issue
 -> fix it: create_commit on downstream/fix-*, create_merge_request, re-measure on that MR
```

The model never writes shell for the engine and never writes a number. Details and the
engine's interface: [`docs/details.md`](docs/details.md) sections 3 to 8.

## Duo Agent Platform features used

| Feature | File | What breaks without it |
|---|---|---|
| Custom flow (YAML) | [`flows/`](flows/) | There is no product; the flow is the entry point |
| Mention trigger on the flow's service account, on merge requests and issues | enabled per project; the same bot serves every project | Nothing starts without a human typing a command |
| `run_command` in the sandbox | `toolset` in [`flows/downstream.yml`](flows/downstream.yml) | The engine and k6 cannot run |
| `setup_script` and `network_policy` | [`.gitlab/duo/agent-config.yml`](.gitlab/duo/agent-config.yml) in each target project | k6 is not installed; only GitLab hosts are reachable; apps that listen on a Unix socket need `allow_all_unix_sockets` |
| MR notes (`create_merge_request_note`) | `toolset` in [`flows/downstream.yml`](flows/downstream.yml) | The session UI shows only "Ran tool", so notes are the only visible output |
| Issues and issue replies (`create_issue`, `get_issue`, `create_issue_note`) | `toolset` in [`flows/downstream.yml`](flows/downstream.yml); verified ([evidence](evidence/flow.md)) | No per-cause issues and no `fix it` |
| Commits and merge requests (`get_repository_file`, `create_commit`, `create_merge_request`) | FIX steps in [`flows/downstream.yml`](flows/downstream.yml); verified (!13) | `fix it` could only post a diff |
| AI Catalog (public flow, per-project enablement) | the flow YAML is released to the catalog; a project enables it and picks the mention trigger | Each project would need its own copy of the flow |

## Claims

Proof is a committed file. Longer notes and limits: [`docs/CLAIMS.md`](docs/CLAIMS.md).

| Claim | Status | File | Proof |
|---|---|---|---|
| Mentioning the flow's service account on an MR starts a session | Verified | `flows/probe.yml` | [run1](evidence/day0/run1-probe-v1-job-log.txt) L65, [run2](evidence/day0/run2-probe-v2-job-log.txt) L53, [run3](evidence/day0/run3-probe-v3-job-log.txt) L57 |
| The goal contains the thread through the trigger, with authors, and the MR IID | Verified | `flows/probe.yml` | [run3](evidence/day0/run3-probe-v3-job-log.txt) L57 (ends `Context: {MergeRequest IID: 1}`) |
| The flow runs four shell commands in sequence, each with a 200 s timeout, and gets exit codes back | Verified | `flows/probe.yml` | [run3](evidence/day0/run3-probe-v3-job-log.txt) L515-586 |
| Each command's output was posted as an MR note (the note text is the owner's transcription; sizes match the log) | Verified | `flows/probe.yml` | [probe-notes](evidence/day0/probe-notes.txt), [cross-check](evidence/day0-tests.md) |
| `setup_script` installs k6 v0.56.0; it prints its version in the sandbox | Verified | `.gitlab/duo/agent-config.yml` | [run3](evidence/day0/run3-probe-v3-job-log.txt) L42-45, L515-520 |
| With that network policy, GitHub, npm, PyPI and the Go proxy answer from the sandbox | Verified | `.gitlab/duo/agent-config.yml` | [run3](evidence/day0/run3-probe-v3-job-log.txt) L91, L530-535 |
| A server on 127.0.0.1 inside the sandbox answers | Verified | `flows/probe.yml` | [run3](evidence/day0/run3-probe-v3-job-log.txt) L545-550 |
| A 150 s command completes | Verified | `flows/probe.yml` | [run3](evidence/day0/run3-probe-v3-job-log.txt) L561-586 |
| This GitLab project is public | Verified | GitLab project settings | [API record](evidence/gitlab-project-2026-10-22.json) |
| GitLab shows the project's licence as MIT | Verified, 2026-10-24 | `LICENSE` | [API record](evidence/gitlab-project-2026-10-24.json) L8-11 |
| The flow is public in GitLab's AI Catalog, so any project can enable it | Verified, 2026-10-24 (version 1.0.8) | `flows/downstream.yml` | [catalog record](evidence/ai-catalog-downstream-2026-10-24.json) L6-7 |
| The engine finds a planted regression between two builds and traces it to the changed function | Verified in GitLab CI and inside the flow on the demo app | `engine/` | [CI job log](evidence/runs/ci-63-engine-e2e/raise-hash-cost-job-log.txt) L77; [report](evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) L5, L12, L53, L90-96; [flow session 9999905](evidence/flow/mr-11-notes.json) L701 |
| A mention on a merge request runs the whole flow in the Duo sandbox: build both versions, model-written journeys, k6, the engine's report, issues | Verified, sessions 9999905 and 10000162 | `flows/downstream.yml` | [mr-11-notes.json](evidence/flow/mr-11-notes.json) L701, L785, L987, L1071; [job log](evidence/flow/session-9999905/job-log.txt) L808 |
| `fix it` on an issue commits the fix on a new branch, opens a merge request for the requester, re-measures it and does not merge | Verified, session 10000200 (!13: 16 metrics improved, 0 regressed) | `flows/downstream.yml` | [issue-6-notes.json](evidence/flow/session-10000200/issue-6-notes.json) L151, L235; [mr-13.json](evidence/flow/session-10000200/mr-13.json) L16, L33; [mr-13-notes.json](evidence/flow/session-10000200/mr-13-notes.json) L33 |
| The same flow runs on another project, NewGit, with app notes and control journeys | Verified, session 10000842 (10 steps regressed; control pages slowed too) | NewGit `downstream.yml`, `downstream.md` | [mr-3-notes.json](evidence/flow/newgit-session-10000842/mr-3-notes.json) L757, L785; [job log](evidence/flow/newgit-session-10000842/job-log.txt) L697 |
| A clean change comes out clean on the GitLab runner and in the flow | Verified, 22 of 22 CI runs on `saas-linux-small-amd64` and 1 of 1 flow run in the Duo sandbox (session 10001525); 6 of 12 local WSL2 runs were false positives | `engine/classify` | [pipeline 63](evidence/runs/ci-63-engine-e2e/clean-change-job-log.txt) L77; [pipeline 66](evidence/runs/ci-66-engine-noise/) 20 runs; [summary](evidence/runs.md); [flow session 10001525](evidence/flow/session-10001525/mr-16-notes.json) L151 |
| `fix it` refuses an option the issue does not have | Verified, session 10001522 | `engine/issues` (`fixcheck`) | [issue-6-notes.json](evidence/flow/session-10001522/issue-6-notes.json) L409 |
| Gated rule: a clean change opens no issue; the planted and new-feature changes are caught | Verified in the Duo sandbox: 0 of 3 clean runs flagged; planted CPU per journey 2.87 / 2.91 / 451.25 ms; new feature 3.09 / 3.04 / 94.96 ms | `engine/classify`, `engine/runner` | [gated/mr-16-notes.json](evidence/flow/gated/mr-16-notes.json) L1273, L1475, L1677; [gated/mr-11-notes.json](evidence/flow/gated/mr-11-notes.json) L1991; [gated/mr-14-notes.json](evidence/flow/gated/mr-14-notes.json) L1211 |
| The journey writer adds negative, edge-input and repeated steps, with statuses read from the head code | Verified, session 10002586 | `flows/downstream.yml` R6, `engine/journey/lint.go` | [gated/mr-11-notes.json](evidence/flow/gated/mr-11-notes.json) L1705 |
| A new feature's journeys run on the new build alongside the compared ones, so its cost to existing pages shows | Verified, session 10001024 on !14 (8 existing steps regressed while the new route was in use; the same MR with a separate pass showed 0) | `engine/runner` | [mr-14-notes.json](evidence/flow/mr-14-notes.json) L151, L381 |
| Journeys are written by the model from the scan and run by k6 against an app | Verified in flow sessions | `engine/journey`, `engine/k6gen` | [mr-11-notes.json](evidence/flow/mr-11-notes.json) L701; [mr-3-notes.json](evidence/flow/newgit-session-10000842/mr-3-notes.json) L757 |
| One issue per cause, starter tagged | Verified that the flow opens the issues; the planted change gives 2 issues for 1 cause | `engine/issues` | [issue-4.json](evidence/flow/session-9999905/issue-4.json) L5; [mr-11-notes.json](evidence/flow/mr-11-notes.json) L785 |
| Gated rule with floors (flow 1.0.12): a clean change opens no issue; planted and demo-feature changes are caught | Verified in the Duo sandbox: 0 of 3 clean runs flagged; planted CPU per journey 2.87 / 2.79 / 424.64 ms; NewGit feature 10.38 / 9.75 / 32.28 ms | `engine/classify` | [floors/mr-16-notes.json](evidence/flow/floors/mr-16-notes.json) L1127, L1329, L1531; [floors/mr-11-notes.json](evidence/flow/floors/mr-11-notes.json) L667; [floors/newgit-mr-3-notes.json](evidence/flow/floors/newgit-mr-3-notes.json) L409 |
| A template change on a server-rendered app is caught by the frontend stage in the Duo sandbox, with one issue per cause | Verified, session 10003926 on NewGit !5 (12 page checks regressed on 4 pages; 3 issues) | `engine/runner/frontend.go`, `tools/fecheck/` | [newgit-mr-5-notes.json](evidence/flow/frontend/newgit-mr-5-notes.json) L151; [issues](evidence/flow/frontend/newgit-issues-5-7.json) L6, L79, L152; [job log](evidence/flow/frontend/session-10003926-newgit-mr5-job-log.txt) L641 (npm ci), L659 (frontend), L751 |
| On real React and Svelte (Vite 8) builds the frontend stage catches every planted kind and passes a clean change | Verified locally (WSL2), not in the Duo sandbox | `fixtures/frontend/`, `scripts/verify_frontend.sh` | [frontend-verify.txt](evidence/frontend-verify.txt) L61, L121, L123 |
| `fix it` works on frontend issues and on app-wide findings: the fix is committed on a new branch, a merge request is opened, and the page check or the backend run is repeated on it | Verified, NewGit: frontend fix !8 (banner removed; 0 regressions, 9 page checks improved) and CPU fix !7 (CPU per journey 32.11 to 9.63 ms, -70%) | `engine/issues/frontend.go`, `engine/cmd/downstream/main.go` (`writeMarkerJourneys`), flow F7 | [newgit-mr-8-notes.json](evidence/flow/fix-it-frontend/newgit-mr-8-notes.json) L5; [newgit-mr-7-notes.json](evidence/flow/fix-it-frontend/newgit-mr-7-notes.json) L33; [issue 15 notes](evidence/flow/fix-it-frontend/newgit-issue-15-notes.json) L235; [frontend fix job](evidence/flow/fix-it-frontend/fix-job-17439455794-issue-15.txt) L573, L588, L639; [CPU fix job](evidence/flow/fix-it-frontend/fix-job-17439456063-issue-12.txt) L585, L603, L668 |
| The Downstream memory service runs on Google Cloud Run with Firestore, refuses projects that have not opted in and commits that do not exist, and the flow adds a Trend across merged merge requests to the report | Verified, live in project `downstream-transcend-26`; NewGit !11 recorded, then !3's trend counted it (CPU per journey +199.5% compounded, flagged) | `engine/memory/`, `engine/history/`, `cloud/`, flow R8H | [probe.txt](evidence/gcp/probe.txt) L5-L12; [cloud-run-downstream-memory.yaml](evidence/gcp/cloud-run-downstream-memory.yaml) L27, L56; [newgit-mr-3-notes.json](evidence/gcp/newgit-mr-3-notes.json) L179; [cloud-logging-run-recorded.json](evidence/gcp/cloud-logging-run-recorded.json) |
| `frontend_node` fetches an exact Node version from the npm registry | Written, not run | `engine/runner/fe_node.go` | none |
| `verify` reruns after merge | Written, not run | `flows/downstream.yml` | none |
| Cost in credits of one run | Not measured | none | none |

## What is mocked or not built

- **The sandbox's noise rate is a small sample.** With the current tolerances the clean change
  ran 4 times in the sandbox and came out clean each time ([C-18](docs/CLAIMS.md),
  [live 1.0.13](evidence/flow/frontend/live-1.0.13-mr-16-notes.json) L151); four runs are not a
  rate.
- **False positives on noisy machines.** Each step's p95 comes from 20 samples, so a short
  stall can pass the 10 ms floor. The clean control was flagged in 6 of 12 local WSL2 runs and
  in 0 of 22 runner runs; the Duo sandbox's noise is not measured ([evidence/runs.md](evidence/runs.md)).
- **`verify` has not run.** The command is in the flow; no session has used it.
- **Latency is advisory.** A slower page with no change in failures or process counters is
  shown in the report but opens no issue. Identical code varied far more in latency than in the
  counters (`docs/details.md`).
- **The gated rule has a small sample.** 3 clean flow runs with the current floors (flow
  1.0.12), and earlier rounds that each needed a floor ([evidence/flow.md](evidence/flow.md)).
  Allocation rises also need 100 objects or 16 KB per journey, bytes written 1 KB per journey,
  write syscalls 1 call per journey and peak memory 10 MB: chosen floors, so a smaller regression of only those kinds passes as
  noise.
- **Static analysis covers Go `net/http` only.** Routes must be registered with string-literal
  patterns on a ServeMux. Other frameworks and languages are not supported. Pages behind a
  catch-all handler show as route `/`, and helpers called from HTML templates through a
  `template.FuncMap` are not linked to routes; a repository's `downstream.md` and the control
  journeys cover those (NewGit, [evidence/flow.md](evidence/flow.md)).
- **The model posts the engine's report by copying it.** In one session it mistyped the last
  paragraph of a long report and posted a correction note with the exact text. In the first
  frontend session it reworded one sentence ("the changes above" became "the changed results
  above"). Figures come from the engine, but a note is only as faithful as that copy.
- **The frontend stage is new and lightly tested.** One sandbox session, on a server-rendered
  app. Static frontend builds (`frontend_build`, `npm ci` of the app) have run only locally.
  `fix it` on a frontend issue has run once in the sandbox (C-21). happy-dom has no layout or paint, so color
  contrast, visual layout and real paint or CPU timing are never checked; every report says so.
- **Fix merge requests show the requester as author.** GitLab runs the session as the user who
  mentioned the bot; the bot requests the review and posts the notes.
- **DB time is not measured.** The report says so.
- **The demo regression is planted.** The demo target is [`examples/shelf`](examples/shelf/), a
  small API written for this project. [`examples/changes/raise-hash-cost.patch`](examples/changes/raise-hash-cost.patch)
  is a change made to look safe in review and to slow every authenticated route; it was not
  found in real history. It was added in the commit "Add the shelf demo API with a planted
  regression and a clean change" (`git log --grep "planted regression"`).
  [`examples/changes/README.md`](examples/changes/README.md) says why.
- **Mock-ups.** Notes and issues in [`docs/userflow.md`](docs/userflow.md) are mock-ups with
  placeholders such as `<p95_base_ms>`.
- **Untested platform capabilities** are T-07 to T-15 in [`docs/requirements.md`](docs/requirements.md).
- **Only project members (Developer or higher) can trigger the flow.** Visitors cannot; `evidence/`
  is the substitute.

## What might fail

Downstream builds the application it checks inside GitLab's Duo sandbox: the engine builds the
base commit and the head commit there and measures both. So the application has to build and
start with what the sandbox has: Go 1.22.12, Node 20.20.2, Python 3.9.25, no docker, no psql,
2 vCPU, about 7.8 GiB of RAM, and a network allowlist ([`docs/details.md`](docs/details.md)
section 2). If an application needs a newer version of its language than those, a tool or
service the sandbox does not have, or a download the allowlist blocks, it will not build or
start. Downstream then stops at the step that failed and reports it, with no verdict.

The engine has built and run the demo app and NewGit inside the sandbox. Getting there showed
what an application can trip over, each now handled ([evidence/flow.md](evidence/flow.md)): the
sandbox's home directory is read-only (the flow puts Go's module cache under `/tmp`), its
`TMPDIR` does not exist (the flow sets its own), and it blocks Unix sockets unless the project's
agent config sets `network_policy.allow_all_unix_sockets: true` (NewGit needs this). A version
mismatch happened once, with the demo app, which imported `crypto/pbkdf2` (Go 1.24 or newer)
and was rewritten to build on Go 1.22 (`git log --grep "crypto/pbkdf2"`). To get past one,
pin the application to the sandbox's versions or install the missing tool in the agent's
`setup_script`.

## Use Downstream on your project

1. Enable the public [`downstream` flow](https://gitlab.com/explore/ai-catalog/flows/1019478) from GitLab's AI Catalog in your project and
   choose the mention trigger. GitLab adds the flow's service account to the project.
2. Add `downstream.yml` at the repository root: how to build and start the app on a loopback
   port (this repo's [`downstream.yml`](downstream.yml) is an example).
3. Add `.gitlab/duo/agent-config.yml` on the default branch to install k6 (copy
   [this one](.gitlab/duo/agent-config.yml)).
4. Optional: `downstream.md` describing the app's test data and key pages, for the journey
   writer. Start the app with its test data in `downstream.yml`'s `start` command.
   For a frontend build (React, Svelte, Vite and similar), add `frontend_build` and
   `frontend_out` to `downstream.yml`; a frontend-only repository needs only those two keys
   ([`fixtures/frontend/react-vite/downstream.yml`](fixtures/frontend/react-vite/downstream.yml)).
   Server-rendered pages need nothing extra.
5. Mention the flow's account on a merge request.

## Built during the hackathon, and what is not

Everything here was written for this entry; the first commit is 2026-10-22 and no earlier code
is reused. The engine and the demo app use the Go standard library only (Go 1.22). Used but not
written by us: GitLab Duo Agent Platform and its default job image, Go, k6 v0.56.0 (downloaded
at job start, not vendored), PyYAML (checks only), and for the frontend stage happy-dom 21.0.4,
axe-core 4.14.0 and html-validate 10.17.0 (pinned in `tools/fecheck`, installed at run time).
The frontend fixtures use React, Svelte and Vite at the versions in their lockfiles.

## Reproduce the day-0 probe

Needs a GitLab.com project where you have Developer role and Duo Agent Platform.

1. Put [`.gitlab/duo/agent-config.yml`](.gitlab/duo/agent-config.yml) on the default branch.
2. Create a custom flow from [`flows/probe.yml`](flows/probe.yml) and add a mention trigger.
3. Mention the flow's service account (`ai-<flow name>-<top-level group>`) on a merge request.
4. The flow posts four notes, one per check.

**Environment.** No keys, tokens or `.env` are needed or present; the flow uses the platform's
own credentials.

**Deployment.** The flow is enabled in the GitLab project through the UI; there is nothing to
deploy from this repo. What the brief means by deployment for a flow entry is unverified (T-12).

**Security.** The model's only output that reaches execution is validated JSON and a number
picking a file from the engine's own list (`downstream show --file <n>`), the engine talks
to 127.0.0.1 only, diffs and issue text are treated as data, and files that steer the agent are
under CODEOWNERS. The engine and `agentnotes.md` come from this repo at a commit pinned in the
flow, and `downstream.yml` is read from the base commit, so a merge request cannot rewrite any
of them for its own run. Known gap: the model's JSON reaches the shell inside a quoted heredoc
([`docs/details.md`](docs/details.md) section 11).

## Roadmap

Done: engine built and logged in CI; the flow runs end to end on GitLab on two projects;
`fix it` opens and re-measures a fix merge request; journeys cover the change, including new
features; tolerances hardened with floors on the live bot; a browser-free frontend stage; a
memory service on Google Cloud Run with a trend across merged merge requests. Next: a clean-change control inside the flow to measure the sandbox's noise, `verify`
after a merge, following `template.FuncMap` in static analysis, and the cost of a session.

## Docs

[`technical_implementation.md`](technical_implementation.md) plain-English technical walkthrough ·
[`docs/details.md`](docs/details.md) design · [`docs/requirements.md`](docs/requirements.md)
requirements, tests T-01 to T-15 · [`docs/userflow.md`](docs/userflow.md) reviewer flow and demo
script · [`docs/CLAIMS.md`](docs/CLAIMS.md) claims with limits · [`evidence/`](evidence/) raw logs ·
[`AGENTS.md`](AGENTS.md) and [`agentnotes.md`](agentnotes.md) agent instructions.

## Licence

MIT. See [LICENSE](LICENSE).

## Hackathon workspace

Transcend Hackathon showcase workspace for @AlphaTechini (Devpost: alphatechini). Onboarding
issue: [work item 1](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase/-/work_items/1).

The platform reads agent configuration from this project's default branch and runs the flow
here.
