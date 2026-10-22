# Downstream

**Before a reviewed change reaches users, Downstream runs realistic user traffic through the
old and new builds, finds what else the change quietly broke or slowed, traces the likely
cause, and files a fix.** It is a GitLab Duo Agent Platform flow. *Merged is not safe.*

For: maintainers and reviewers of web/API services on GitLab, especially small teams with no
staging environment that replays real user behaviour.

> **Status, 2026-10-22: platform tests are done and logged; the engine is built but has no
> logged run yet.** The engine (`engine/`) scans a diff, runs journeys on two builds with k6
> and renders the report and issues; its unit and integration tests pass with `make test`.
> No run of it is committed under `evidence/`, so nothing about it is marked Verified. The
> claims table says exactly what has been shown.

## Built for GitLab Transcend: Life After Code

Requirements below come from the Devpost Rules as read during research on 2026-10-21/22. They
have not been re-read against the live page since and are to be re-checked before submission.

| Brief requirement | Where in this repo | Status |
|---|---|---|
| Genuine use of Duo Agent Platform (agents, flows, MCP) | [`flows/probe.yml`](flows/probe.yml) ran as a real flow; [`flows/downstream.yml`](flows/downstream.yml) is the product flow | Probe verified. Product flow written, not yet run on the platform |
| Public MIT-licensed GitLab project | [`LICENSE`](LICENSE); this is the [GitLab showcase project](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/showcase) where the flow runs | Public per the GitLab API on 2026-10-22 ([record](evidence/gitlab-project-2026-10-22.json)). MIT `LICENSE` committed |
| Visible CI/CD pipeline history | GitLab agent-session pipelines; copies in [`evidence/day0/`](evidence/day0/). [`.gitlab-ci.yml`](.gitlab-ci.yml) runs `make verify`, the Go tests, and `engine-e2e`, which runs the engine end to end on the demo app for the planted and the clean change | 3 probe sessions, 0 engine runs; no `engine-e2e` log committed yet |
| Demo video under 3 minutes | script in [`docs/userflow.md`](docs/userflow.md) section 7 | Not recorded |
| One declared autonomy tier: **Supervised** | the flow never merges; a human picks a fix option and merges ([`docs/userflow.md`](docs/userflow.md)) | Design only |
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
| 1 | One mention on a merge request starts the flow | Verified (probe) |
| 2 | It maps changed routes and routes that depend on them (blast radius) | Built: `downstream scan` ([`engine/scan`](engine/scan/)), Go `net/http` only; no logged run |
| 3 | It writes 3 to 6 user journeys and shows them to the reviewer in plain English first | Engine side built: schema and validation ([`engine/journey`](engine/journey/)); the model writing them is in the flow prompt, not run |
| 4 | k6 runs them on the old build twice and the new build once, so noise is measured | Built: `downstream run` ([`engine/runner`](engine/runner/), [`engine/k6gen`](engine/k6gen/)); no logged run |
| 5 | Only differences beyond the noise are reported: one issue per cause (repro, likely cause, numbered fix options, starter tagged); trade-offs become a question on the MR | Engine side built: [`engine/classify`](engine/classify/), [`engine/report`](engine/report/), [`engine/issues`](engine/issues/); creating issues from the flow untested (T-08) |
| 6 | `@handle fix option N` applies an option and reruns that journey; `verify` reruns after merge | Engine side built: marker parsing and `fixcheck`; committing a fix from the flow untested (T-10), so a fix is posted as a diff for a human |

User-activity simulation, not load testing: a few virtual users for tens of seconds. The model
chooses journeys and writes sentences; code produces every number, table and chart.
Design: [`docs/details.md`](docs/details.md). What reviewers see: [`docs/userflow.md`](docs/userflow.md).

## Duo Agent Platform features used

| Feature | File | What breaks without it |
|---|---|---|
| Custom flow (YAML) | [`flows/`](flows/) | There is no product; the flow is the entry point |
| Mention trigger on the flow's service account | set in the GitLab UI; logs show it firing | Nothing starts without a human typing a command |
| `run_command` in the sandbox | `toolset` in [`flows/probe.yml`](flows/probe.yml) (verified) and [`flows/downstream.yml`](flows/downstream.yml) | The engine and k6 cannot run |
| `setup_script` and `network_policy` | [`.gitlab/duo/agent-config.yml`](.gitlab/duo/agent-config.yml) | k6 and Go 1.26 are not installed; only GitLab hosts are reachable |
| MR notes (`create_merge_request_note`) | `toolset` in [`flows/probe.yml`](flows/probe.yml) | The session UI shows only "Ran tool", so notes are the only visible output |
| Issues and issue replies (`create_issue`, `get_issue`, `create_issue_note`) | `toolset` in [`flows/downstream.yml`](flows/downstream.yml); not tested (T-08, T-09) | No per-cause issues; fall back to MR threads |

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
| The engine finds regressions between two builds | Built, not verified: no run committed | `engine/` | none |
| Journeys are generated from the diff and run by k6 against an app | Engine side built, not verified; the model writing journeys has not run | `engine/journey`, `engine/k6gen` | none |
| One issue per cause, starter tagged | Issue bodies built, not verified; issue creation from a flow untested | `engine/issues` | none |
| `fix option N` applies a change; `verify` reruns after merge | Not built in the flow; engine checks the option number only; committing from a flow untested | `engine/issues` | none |
| Cost in credits of one run | Not measured | none | none |

## What is mocked or not built

- **The engine has no logged run.** It is built and tested (`make test`), but no `run.json` or
  report from it is committed, so its claims are not Verified.
- **The product flow has not run.** [`flows/downstream.yml`](flows/downstream.yml) uses tools
  not yet exercised from a custom flow (`create_issue`, `get_issue`, `create_issue_note`;
  T-08, T-09) and Go 1.26 lines in `setup_script` that have not run (see
  [`.gitlab/duo/agent-config.yml`](.gitlab/duo/agent-config.yml)).
- **Static analysis covers Go `net/http` only.** Routes must be registered with string-literal
  patterns on a ServeMux. Other frameworks and languages are not supported.
- **Fixes are not applied.** `fix option N` checks the option exists; the change itself would
  be posted as a diff for a human to apply.
- **DB time is not measured.** The report says so.
- **The demo regression is planted.** The demo target is [`examples/shelf`](examples/shelf/), a
  small API written for this project. [`examples/changes/raise-hash-cost.patch`](examples/changes/raise-hash-cost.patch)
  is a change made to look safe in review and to slow every authenticated route; it was not
  found in real history. [`examples/changes/README.md`](examples/changes/README.md) says why.
- **Mock-ups.** Notes and issues in [`docs/userflow.md`](docs/userflow.md) are mock-ups with
  placeholders such as `<p95_base_ms>`.
- **Untested platform capabilities** are T-07 to T-15 in [`docs/requirements.md`](docs/requirements.md).
- **Only project members (Developer or higher) can trigger the flow.** Visitors cannot; `evidence/`
  is the substitute.

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

With Go 1.26 and k6 on PATH, `make test` also runs gofmt, go vet and the engine's tests,
including the integration tests that start an app and run k6 (they skip without k6).

`make verify` validates the flow and config YAML, checks that docs links resolve, that every Verified
row above links to committed evidence, that logs hold no unredacted email, and that the
evidence files match the checksums in `evidence/SHA256SUMS`. It does not run
Downstream; the logs in `evidence/` show the platform behaviour. Flow runs happen in this
project; their pipelines are under CI/CD > Pipelines.

## Architecture and file paths

```text
mention on an MR
 -> flows/downstream.yml              one AgentComponent, one prompt (written, not yet run)
     reads agentnotes.md              from the base commit: journey style, issue format
     calls fixed engine commands      engine/cmd/downstream: scan, schema, validate, run, report,
                                      summary, issues, marker, fixcheck (built, no logged run)
       builds base and head from git, runs the app on 127.0.0.1
       k6 v0.56.0                     installed by .gitlab/duo/agent-config.yml (verified)
       Go 1.26 toolchain              fetched by .gitlab/duo/agent-config.yml (not yet run)
 -> MR notes and issues with a hidden marker so any reply rebuilds state from the issue
```

The model never writes shell for the engine and never writes a number. Details and the
engine's interface: [`docs/details.md`](docs/details.md) sections 3 to 8.

## Measured results

No engine measurements exist yet; the first will go in `evidence/runs/`. What has been measured
is the platform, from the logs in `evidence/day0/`:

| Measurement | Value | Source |
|---|---|---|
| Runner | `saas-linux-small-amd64`, 2 CPUs, 8145540 kB memory, 18G free in `/tmp` | [run1](evidence/day0/run1-probe-v1-job-log.txt) L1-2; [probe-notes](evidence/day0/probe-notes.txt) |
| Probe v1 (stopped after one action), whole CI job | 45.4 s | [run1](evidence/day0/run1-probe-v1-job-log.txt) L1, L564 |
| Probe v2, sandbox start to FINISHED | 21.5 s | [run2](evidence/day0/run2-probe-v2-job-log.txt) L114, L529 |
| Probe v3 (four commands, one a deliberate 150 s sleep), sandbox start to FINISHED | 221.8 s | [run3](evidence/day0/run3-probe-v3-job-log.txt) L118, L599 |
| Sandbox network allowlist with our config | 175 entries (the default list has 5) | [run3](evidence/day0/run3-probe-v3-job-log.txt) L91 |

## Built during the hackathon, and what is not

Everything here was written for this entry; the first commit is 2026-10-22 and no earlier code
is reused. The engine and the demo app use the Go standard library only (Go 1.26). Used but not
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
under CODEOWNERS. `agentnotes.md` and `downstream.yml` are read from the base commit, so a
merge request cannot rewrite them for its own run. Known gaps: the model's JSON reaches the
shell inside a quoted heredoc, and the engine is built from the merge request's own checkout
([`docs/details.md`](docs/details.md) section 11).

## Roadmap

Done: demo target chosen (planted regression, labelled), engine built. Next: the engine's
first logged run, then T-07 to T-10 with the product flow. Sun 25: at least five logged end-to-end runs; if there are fewer, submit the smaller version
(report in the MR only) and say so. Mon 26: video. Tue 27: submit before 13:00 UTC.

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
