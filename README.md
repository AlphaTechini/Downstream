# Downstream

**Before a reviewed change reaches users, Downstream runs realistic user traffic through the
old and new builds, finds what else the change quietly broke or slowed, traces the likely
cause, and files a fix.** It is a GitLab Duo Agent Platform flow. *Merged is not safe.*

For: maintainers and reviewers of web/API services on GitLab, especially small teams with no
staging environment that replays real user behaviour.

> **Status, 2026-10-24: the flow runs end to end on GitLab.** A mention on a merge request
> builds both versions in the Duo sandbox, runs model-written journeys with k6, posts the
> engine's report and opens one issue per cause; `fix it` on an issue commits the fix on a new
> branch, opens a merge request for the requester and re-measures it. Shown on the demo app in
> this project and on [NewGit](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/newgit),
> a separate project, with the same flow ([evidence/flow.md](evidence/flow.md)). In CI the engine
> also passes the clean control in 22 of 22 runs ([evidence/runs.md](evidence/runs.md)); the
> sandbox's own noise is not measured yet. The claims table says exactly what has been shown.

## Built for GitLab Transcend: Life After Code

Requirements below come from the Devpost Rules as read during research on 2026-10-21/22. They
have not been re-read against the live page since and are to be re-checked before submission.

| Brief requirement | Where in this repo | Status |
|---|---|---|
| Genuine use of Duo Agent Platform (agents, flows, MCP) | [`flows/downstream.yml`](flows/downstream.yml), a public custom flow in the AI Catalog with a mention trigger; [`flows/probe.yml`](flows/probe.yml) was the day-0 feasibility flow | Verified: full sessions on two projects ([evidence/flow.md](evidence/flow.md)) |
| Public MIT-licensed GitLab project | [`LICENSE`](LICENSE); this is the [GitLab showcase project](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase) where the flow runs | Public per the GitLab API on 2026-10-22 ([record](evidence/gitlab-project-2026-10-22.json)). MIT `LICENSE` committed |
| Visible CI/CD pipeline history | GitLab agent-session pipelines; copies in [`evidence/day0/`](evidence/day0/). [`.gitlab-ci.yml`](.gitlab-ci.yml) runs `make verify`, the Go tests, and `engine-e2e`, which runs the engine end to end on the demo app for the planted and the clean change; `engine-noise` repeats the clean change on the `measure-noise` branch | 3 probe sessions; engine CI runs from pipelines 63 and 66 in [`evidence/runs/`](evidence/runs/); flow sessions in [`evidence/flow/`](evidence/flow/) |
| Demo video under 3 minutes | script in [`docs/userflow.md`](docs/userflow.md) section 7 | Not recorded |
| One declared autonomy tier: **Supervised** | the flow never merges; a human picks a fix option, the flow opens a merge request for it, and a human reviews and merges | Verified: `fix it` opened !13 with the requester as reviewer and did not merge ([evidence/flow.md](evidence/flow.md)) |
| Path A (new project) | first commit 2026-10-22 (`git log`) | Intended path |

Not claimed: coverage of all nine lifecycle stages, the Google Cloud bonus (no deployment code
here), the environmental prize.

## The problem

Review is the last routine human check before a change ships, and changes are where outages
come from. Google's SRE book: "SRE has found that roughly 70% of outages are due to changes in
a live system" ([ch. 1, Change Management](https://sre.google/sre-book/introduction/)). In
Google's postmortem sample for 2010-2017, binary pushes were 37% and configuration pushes 31%
of outage triggers ([SRE Workbook, Table C-1](https://sre.google/workbook/postmortem-analysis/)).
Both are Google-specific.

Our argument, which is not measured here: a diff shows what changed, not what the change does
to routes the author never opened. That gap is what Downstream targets.

## How it works

| Step | What happens | Status |
|---|---|---|
| 1 | One mention on a merge request starts the flow | Verified in flow sessions ([evidence](evidence/flow.md)) |
| 2 | It maps changed routes, routes that share code with them (blast radius) and routes the merge request adds | Verified: `downstream scan` ([`engine/scan`](engine/scan/)), Go `net/http` only ([evidence](evidence/flow.md)) |
| 3 | The model writes journeys that cover the change, up to 20: existing journeys reaching every changed and affected route, new journeys for every new route, and control journeys elsewhere; the engine rejects a set that leaves a route out. They are shown to the reviewer in plain English first | Verified: coverage rules in [`engine/journey`](engine/journey/coverage.go), model-written journeys in every full session ([evidence](evidence/flow.md)) |
| 4 | k6 runs them on the old build twice and the new build once, so noise is measured; new-feature journeys run in the new build's pass alongside the others, so existing pages are measured while the feature is in use | Verified in the sandbox ([`engine/runner`](engine/runner/), [`engine/k6gen`](engine/k6gen/)) |
| 5 | Only differences beyond the noise are reported: one issue per cause (repro, likely cause, numbered fix options, starter tagged); trade-offs become a question on the MR | Verified: issues opened by the flow; the planted change gives 2 issues for 1 cause (one for the routes that reach the changed function, one for slowed routes outside them) |
| 6 | `@handle fix it` (option 1) or `fix option N` commits the option on a new branch, opens a merge request mentioning the requester and re-runs the issue's journey on it; `verify` reruns after merge | `fix it` verified (!13, [evidence](evidence/flow.md)); `verify` written, not run |

User-activity simulation, not load testing: a few virtual users for tens of seconds. The model
chooses journeys and writes sentences; code produces every number, table and chart.
Design: [`docs/details.md`](docs/details.md). What reviewers see: [`docs/userflow.md`](docs/userflow.md).

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
| The engine finds a planted regression between two builds and traces it to the changed function | Verified in GitLab CI and inside the flow on the demo app | `engine/` | [CI job log](evidence/runs/ci-63-engine-e2e/raise-hash-cost-job-log.txt) L77; [report](evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) L5, L12, L53, L90-96; [flow session 9999905](evidence/flow/mr-11-notes.json) L701 |
| A mention on a merge request runs the whole flow in the Duo sandbox: build both versions, model-written journeys, k6, the engine's report, issues | Verified, sessions 9999905 and 10000162 | `flows/downstream.yml` | [mr-11-notes.json](evidence/flow/mr-11-notes.json) L701, L785, L987, L1071; [job log](evidence/flow/session-9999905/job-log.txt) L808 |
| `fix it` on an issue commits the fix on a new branch, opens a merge request for the requester, re-measures it and does not merge | Verified, session 10000200 (!13: 16 metrics improved, 0 regressed) | `flows/downstream.yml` | [issue-6-notes.json](evidence/flow/session-10000200/issue-6-notes.json) L151, L235; [mr-13.json](evidence/flow/session-10000200/mr-13.json) L16, L33; [mr-13-notes.json](evidence/flow/session-10000200/mr-13-notes.json) L33 |
| The same flow runs on another project, NewGit, with app notes and control journeys | Verified, session 10000842 (10 steps regressed; control pages slowed too) | NewGit `downstream.yml`, `downstream.md` | [mr-3-notes.json](evidence/flow/newgit-session-10000842/mr-3-notes.json) L757, L785; [job log](evidence/flow/newgit-session-10000842/job-log.txt) L697 |
| A clean change comes out clean on the GitLab runner | Verified, 22 of 22 runs on `saas-linux-small-amd64`; 6 of 12 local WSL2 runs were false positives; the Duo sandbox is not measured | `engine/classify` | [pipeline 63](evidence/runs/ci-63-engine-e2e/clean-change-job-log.txt) L77; [pipeline 66](evidence/runs/ci-66-engine-noise/) 20 runs; [summary](evidence/runs.md) |
| A new feature's journeys run on the new build alongside the compared ones, so its cost to existing pages shows | Verified, session 10001024 on !14 (8 existing steps regressed while the new route was in use; the same MR with a separate pass showed 0) | `engine/runner` | [mr-14-notes.json](evidence/flow/mr-14-notes.json) L151, L381 |
| Journeys are written by the model from the scan and run by k6 against an app | Verified in flow sessions | `engine/journey`, `engine/k6gen` | [mr-11-notes.json](evidence/flow/mr-11-notes.json) L701; [mr-3-notes.json](evidence/flow/newgit-session-10000842/mr-3-notes.json) L757 |
| One issue per cause, starter tagged | Verified that the flow opens the issues; the planted change gives 2 issues for 1 cause | `engine/issues` | [issue-4.json](evidence/flow/session-9999905/issue-4.json) L5; [mr-11-notes.json](evidence/flow/mr-11-notes.json) L785 |
| `verify` reruns after merge | Written, not run | `flows/downstream.yml` | none |
| Cost in credits of one run | Not measured | none | none |

## What is mocked or not built

- **The sandbox's noise is not measured.** No clean-change control has run inside the flow yet,
  so how often a harmless change is flagged there is unknown.
- **False positives on noisy machines.** Each step's p95 comes from 20 samples, so a short
  stall can pass the 10 ms floor. The clean control was flagged in 6 of 12 local WSL2 runs and
  in 0 of 22 runner runs; the Duo sandbox's noise is not measured ([evidence/runs.md](evidence/runs.md)).
- **`verify` has not run.** The command is in the flow; no session has used it.
- **Static analysis covers Go `net/http` only.** Routes must be registered with string-literal
  patterns on a ServeMux. Other frameworks and languages are not supported. Pages behind a
  catch-all handler show as route `/`, and helpers called from HTML templates through a
  `template.FuncMap` are not linked to routes; a repository's `downstream.md` and the control
  journeys cover those (NewGit, [evidence/flow.md](evidence/flow.md)).
- **The model posts the engine's report by copying it.** In one session it mistyped the last
  paragraph of a long report and posted a correction note with the exact text. Figures come
  from the engine, but a note is only as faithful as that copy.
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

1. Enable the public `downstream` flow from GitLab's AI Catalog in your project and choose the
   mention trigger. GitLab adds the flow's service account to the project.
2. Add `downstream.yml` at the repository root: how to build and start the app on a loopback
   port (this repo's [`downstream.yml`](downstream.yml) is an example).
3. Add `.gitlab/duo/agent-config.yml` on the default branch to install k6 (copy
   [this one](.gitlab/duo/agent-config.yml)).
4. Optional: `downstream.md` describing the app's test data and key pages, for the journey
   writer. Start the app with its test data in `downstream.yml`'s `start` command.
5. Mention the flow's account on a merge request.

## 30-second check

```bash
git clone https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase.git && cd showcase
pip install pyyaml
make verify
```

Expected output:

```text
python3 scripts/validate_yaml.py
ok: 2 flow file(s), 1 agent config file(s)
python3 scripts/check_evidence.py
ok: links resolve, verified claims cite evidence, evidence is clean, checksums match
```

With Go 1.22 or newer and k6 on PATH, `make test` also runs gofmt, go vet and the engine's tests,
including the integration tests that start an app and run k6 (they skip without k6).

`make verify` validates the flow and config YAML, checks that docs links resolve, that every Verified
row above links to committed evidence, that logs hold no unredacted email, and that the
evidence files match the checksums in `evidence/SHA256SUMS`. It does not run
Downstream; the logs in `evidence/` show the platform behaviour. Flow runs happen in this
project; their pipelines are under CI/CD > Pipelines.

## Architecture and file paths

```text
mention on an MR
 -> flows/downstream.yml              one AgentComponent, one prompt (verified in flow sessions)
     fetches a pinned commit          of this repo (GitHub mirror tarball), builds the engine,
                                      reads agentnotes.md; one flow serves any project
     calls fixed engine commands      engine/cmd/downstream: scan, schema, validate, run, report,
                                      summary, issues, marker, fixcheck (CI runs in
                                      evidence/runs.md, flow runs in evidence/flow.md)
       builds base and head from git, runs the app on 127.0.0.1
       k6 v0.56.0                     installed by .gitlab/duo/agent-config.yml (verified)
       Go 1.22.12                     already in the sandbox image (verified), nothing to install
 -> MR notes and issues; each issue ends with a "Downstream state" block so a reply
    (fix it, verify) rebuilds its state from the issue
 -> fix it: create_commit on downstream/fix-*, create_merge_request, re-measure on that MR
```

The model never writes shell for the engine and never writes a number. Details and the
engine's interface: [`docs/details.md`](docs/details.md) sections 3 to 8.

## Measured results

Flow sessions on GitLab, from [`evidence/flow/`](evidence/flow/) (details in
[evidence/flow.md](evidence/flow.md)):

| Measurement | Value | Source |
|---|---|---|
| Planted `hashSecret` change (!11): regressed steps, app CPU time B1 / C / B2 | 16 of 16; 0.11 / 17.69 / 0.12 s | [mr-11-notes.json](evidence/flow/mr-11-notes.json) L701 |
| `fix it` re-measure (!13 against !11): logged-in steps p95, app CPU time | about 66 ms to 0.2-0.6 ms; 8.80 / 2.37 / 8.86 s, 0 regressions | [mr-13-notes.json](evidence/flow/session-10000200/mr-13-notes.json) L33 |
| New similar-books route (!14): existing steps regressed while it is in use; app CPU time | 8 (`shelf_list` 2.1 ms to 129.4 ms p95); 0.10 / 1.72 / 0.11 s | [mr-14-notes.json](evidence/flow/mr-14-notes.json) L381 |
| NewGit avatar change (NewGit !3): regressed steps, app CPU time | 10, controls included; 0.32 / 1.50 / 0.37 s | [mr-3-notes.json](evidence/flow/newgit-session-10000842/mr-3-notes.json) L785 |

Engine runs in GitLab CI on the demo app, from [`evidence/runs/`](evidence/runs/) (one run per
row unless stated; details in [evidence/runs.md](evidence/runs.md)):

| Measurement | Value | Source |
|---|---|---|
| Planted change: regressed steps | 22 of 22, in 2 issue groups | [report](evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) L5 |
| Planted change: `log_in` p95, B1 / B2 / candidate | 2.8 / 1.6 / 537.9 ms | [report](evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) L12 |
| Planted change: app CPU time, B1 / candidate / B2 | 0.16 / 27.10 / 0.16 s | [report](evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) L53 |
| Clean change: runs flagged as regression | 0 of 22 (pipelines 63 and 66) | [pipeline 63](evidence/runs/ci-63-engine-e2e/clean-change-job-log.txt) L77; [pipeline 66](evidence/runs/ci-66-engine-noise/) |
| Clean change: largest step p95 increase over 20 runs | 4.6 ms (regression floor 10 ms) | `evidence/runs/ci-66-engine-noise/noise-*/report-*.md` |

The platform, from the logs in `evidence/day0/`:

| Measurement | Value | Source |
|---|---|---|
| Runner | `saas-linux-small-amd64`, 2 CPUs, 8145540 kB memory, 18G free in `/tmp` | [run1](evidence/day0/run1-probe-v1-job-log.txt) L1-2; [probe-notes](evidence/day0/probe-notes.txt) |
| Probe v1 (stopped after one action), whole CI job | 45.4 s | [run1](evidence/day0/run1-probe-v1-job-log.txt) L1, L564 |
| Probe v2, sandbox start to FINISHED | 21.5 s | [run2](evidence/day0/run2-probe-v2-job-log.txt) L114, L529 |
| Probe v3 (four commands, one a deliberate 150 s sleep), sandbox start to FINISHED | 221.8 s | [run3](evidence/day0/run3-probe-v3-job-log.txt) L118, L599 |
| Sandbox network allowlist with our config | 175 entries (the default list has 5) | [run3](evidence/day0/run3-probe-v3-job-log.txt) L91 |

## Built during the hackathon, and what is not

Everything here was written for this entry; the first commit is 2026-10-22 and no earlier code
is reused. The engine and the demo app use the Go standard library only (Go 1.22). Used but not
written by us: GitLab Duo Agent Platform and its default job image, Go, k6 v0.56.0 (downloaded
at job start, not vendored), PyYAML (checks only).

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

**Security.** The model's only output that reaches execution is validated JSON, the engine talks
to 127.0.0.1 only, diffs and issue text are treated as data, and files that steer the agent are
under CODEOWNERS. The engine and `agentnotes.md` come from this repo at a commit pinned in the
flow, and `downstream.yml` is read from the base commit, so a merge request cannot rewrite any
of them for its own run. Known gap: the model's JSON reaches the shell inside a quoted heredoc
([`docs/details.md`](docs/details.md) section 11).

## Roadmap

Done: engine built and logged in CI; the flow runs end to end on GitLab on two projects;
`fix it` opens and re-measures a fix merge request; journeys cover the change, including new
features. Next: a clean-change control inside the flow to measure the sandbox's noise, `verify`
after a merge, following `template.FuncMap` in static analysis, and the cost of a session.

## Docs

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
