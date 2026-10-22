# Downstream

**Merged is not safe.** A code review tells you whether a change looks right. It does not
tell you what the change does to the rest of the app while real people are using it.

Downstream is a GitLab Duo Agent Platform flow. Mention it on a merge request and, before the
change reaches users, it runs realistic user journeys through the old and new builds, finds
what else the change quietly broke or slowed, traces the likely cause, and files an issue with
numbered fix options.

> **Status, 2026-10-22: the platform tests are done and logged. The engine is not written yet.**
> Nothing in this repository finds a regression today. The claims table below says exactly
> what has been shown and what has not. Entry for the GitLab Transcend "Life After Code"
> hackathon; deadline Tue 2026-10-27 13:00 UTC.

## What it will do

1. You mention the flow on a merge request. One mention is enough.
2. It works out which routes the change touches and which other routes depend on them.
3. It writes 3 to 6 user journeys (log in, open a list, change something, come back) and
   shows them to you in plain English before anything runs.
4. It runs them with k6 against the old build twice and the new build once, so it knows how
   noisy the machine is, and reports only differences bigger than the noise.
5. Each regression becomes its own issue: repro, likely cause, numbered fix options, and the
   person who started the run is tagged. A real trade-off becomes a question on the merge
   request instead.
6. A maintainer replies `fix option 2` in the issue. The flow applies it and reruns the same
   journey. After merge, `verify` reruns the journeys on the merged build.

This is user-activity simulation, not load testing: a few virtual users for tens of seconds.
Tables and charts are produced by code, not by the model; the model chooses journeys and
writes sentences. Full design: [docs/details.md](docs/details.md). What a reviewer sees, step
by step, and the demo script: [docs/userflow.md](docs/userflow.md). Requirements and tests:
[docs/requirements.md](docs/requirements.md).

## 30-second check

```bash
git clone https://github.com/AlphaTechini/downstream && cd downstream
pip install pyyaml
make verify
```

`make verify` parses the flow and agent-config YAML and checks their structure, checks that
every link in the docs resolves, that every "Verified" row below links to committed evidence,
and that the evidence logs contain no unredacted email address. It does **not** run Downstream
and does not prove the platform behaviour; the logs in `evidence/` do that.

## Claims

| Claim | Status | Evidence |
|---|---|---|
| A custom flow starts when its service account is mentioned on a merge request | Verified | [run1](evidence/day0/run1-probe-v1-job-log.txt) line 65, [run2](evidence/day0/run2-probe-v2-job-log.txt) line 53, [run3](evidence/day0/run3-probe-v3-job-log.txt) line 57 |
| The flow receives the discussion through the triggering message, with authors, and the merge request IID | Verified | same lines; [results](evidence/day0-tests.md) |
| A flow can run shell commands and chain several tool calls, posting a note after each (one earlier version stopped after one action; cause unproven) | Verified | [run2](evidence/day0/run2-probe-v2-job-log.txt), [run3](evidence/day0/run3-probe-v3-job-log.txt), [notes transcription](evidence/day0/probe-notes.txt) |
| k6 v0.56.0 can be installed at job start by `setup_script` and prints its version in the sandbox | Verified (install and version only) | [run3](evidence/day0/run3-probe-v3-job-log.txt) lines 42-45, 515-520 |
| With the network policy in `.gitlab/duo/agent-config.yml`, GitHub, npm, PyPI and the Go proxy are reachable from the sandbox | Verified | [run3](evidence/day0/run3-probe-v3-job-log.txt) lines 91, 530-535; [notes](evidence/day0/probe-notes.txt) |
| A server on 127.0.0.1 inside the sandbox answers requests | Verified | [run3](evidence/day0/run3-probe-v3-job-log.txt) lines 545-550 |
| A 150 second command completes inside the sandbox | Verified | [run3](evidence/day0/run3-probe-v3-job-log.txt) lines 561-586 |
| The engine finds regressions between two builds | Not built | |
| Journeys are generated from the diff and the route map | Not built | |
| k6 has run a journey against an application here | Not done | |
| One issue per regression, starter tagged, with numbered fix options | Not built; issue creation from a custom flow not tested | |
| `fix option N` applies a change and reruns the journey | Not built; committing from a custom flow not tested | |
| `verify` reruns after merge | Not built | |
| Before/after report page on GitLab Pages | Not built; not tested | |
| Cost in credits of one run | Not measured | |

## What is mocked or not built

- **The engine does not exist.** `flows/downstream.draft.yml` is a draft of the flow's
  prompt and structure. It is marked "do not enable" and cannot do its job without the engine.
- **The demo target app is not chosen.** The planned approach and the criteria are in
  [docs/details.md](docs/details.md), section 9. Any regression shown in the final demo will be
  labelled as planted or as found, with the commit linked.
- **Mock-ups.** The notes and issues in [docs/userflow.md](docs/userflow.md) are written
  before the engine and use placeholders such as `<p95_base_ms>`. They are marked as mock-ups
  in that file.
- **Untested platform capabilities** are listed as T-07 to T-15 in
  [docs/requirements.md](docs/requirements.md). Until a test passes, the README does not claim
  the capability.
- **The flow can only be triggered by project members** with Developer role or higher.
  Visitors cannot trigger it; the evidence folder is the substitute.

## Repository layout

```text
README.md              this file
AGENTS.md              rules for agents working in this repo
agentnotes.md          operating notes the flow reads each run
LICENSE                MIT
Makefile               make verify
.gitlab/duo/           agent-config.yml (verified)
.gitlab/CODEOWNERS     protects the files that steer the agent
.gitlab-ci.yml         runs make verify on GitLab
.github/workflows/     runs make verify on GitHub
flows/                 probe.yml (verified), downstream.draft.yml (not enabled)
docs/                  details.md, requirements.md, userflow.md
evidence/              raw job logs and the day-0 results table
scripts/               validate_yaml.py, check_evidence.py
```

GitLab reads flow and agent configuration from the **GitLab** project's default branch, so the
GitLab Showcase project is where the flow runs. This GitHub repository is the staging copy
and public record; the two are synced by hand.

## Reproducing the day-0 probe on your own GitLab project

Requires a GitLab.com project where you have the Developer role with Duo Agent Platform
enabled.

1. Copy `.gitlab/duo/agent-config.yml` to the default branch.
2. In the project, create a custom flow from `flows/probe.yml` and add a mention trigger.
3. Mention the flow's service account on a merge request. The handle format is
   `ai-<flow name>-<top-level group>`.
4. The flow posts four notes on the merge request, one per check.

The job logs in `evidence/day0/` are from exactly this setup.

## Licence

MIT. See [LICENSE](LICENSE).
