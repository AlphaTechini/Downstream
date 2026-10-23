# Claims

Long form of the claims table in the [README](../README.md). One entry per claim: what is
claimed, the exact proof, how to check it, and what the proof does **not** show. Entries
marked "Not built" or "Not measured" have no proof and make no claim.

Line numbers refer to the files in `evidence/` as committed. Test IDs (T-xx) are in
[requirements.md](requirements.md).

Run-to-session map: run1 = session 9880588 (probe v1), run2 = session 9881417 (probe v2),
run3 = session 9883560 (probe v3, `flows/probe.yml`).

---

## C-1. Mentioning the flow's service account on a merge request starts a session

- **File:** `flows/probe.yml`
- **Proof:** [run1](../evidence/day0/run1-probe-v1-job-log.txt) line 65,
  [run2](../evidence/day0/run2-probe-v2-job-log.txt) line 53,
  [run3](../evidence/day0/run3-probe-v3-job-log.txt) line 57. Each is the goal the flow
  received, beginning `Input: <!-- All non-system discussion messages through the triggering
  message are included. --> <message id=... author="@AlphaTechini"> @ai-downstream-probe-...`.
  Each log ends `Job succeeded`.
- **Check:** `grep -n "^Input:" evidence/day0/run2-probe-v2-job-log.txt`
- **Does not show:** that a mention inside an **issue** works (T-09), or that someone without
  Developer role can trigger it (they cannot, per platform rules; not tested).

## C-2. The goal contains the thread through the trigger, with authors, and the MR IID

- **File:** `flows/probe.yml`
- **Proof:** [run3](../evidence/day0/run3-probe-v3-job-log.txt) line 57 ends
  `Context: {MergeRequest IID: 1}`; the message ids and `author` attributes are on the same
  line.
- **Does not show:** the goal format for issues (documented as `Context: {Issue IID: n}`; not
  seen in a log).

## C-3. The flow runs four shell commands in sequence, each with a 200 s timeout, and gets exit codes back

- **File:** `flows/probe.yml` (`toolset` includes `run_command`)
- **Proof:** [run3](../evidence/day0/run3-probe-v3-job-log.txt) lines 515-520 (step A), 530-535
  (B), 545-550 (C), 561-586 (D). Each shows `Action received`, `Timeout signal created:
  command will be aborted after 200s`, and `Shell command exited with code 0 (<n> chars)`.
- **Check:** `grep -n "Shell command exited" evidence/day0/run3-probe-v3-job-log.txt`
- **Does not show:** commands longer than 200 s, or commands that fail (error handling is
  untested).

## C-4. Each command's output was posted as an MR note

- **File:** `flows/probe.yml`
- **Proof:** [probe-notes.txt](../evidence/day0/probe-notes.txt) is the owner's transcription of
  the notes. Its text has the same length as the output sizes in the log: A 198, B 148, C 10,
  D 27 characters ([cross-check table](../evidence/day0-tests.md)).
- **Does not show:** that the notes were posted by the flow rather than typed by someone; the
  matching sizes make a faithful copy likely, but the notes themselves are not a machine
  capture. A screenshot would be a stronger source; none is committed.

## C-5. `setup_script` installs k6 v0.56.0 and it prints its version in the sandbox

- **File:** `.gitlab/duo/agent-config.yml`
- **Proof:** [run3](../evidence/day0/run3-probe-v3-job-log.txt) lines 42-45 (the four install
  commands executed in the job script) and lines 515-520 (step A, exit 0, 198 chars; the text
  `k6 v0.56.0 (commit/50afb99947, go1.23.4, linux/amd64)` is in `probe-notes.txt`).
- **Does not show:** that k6 can execute a script against a server. That has not been run.

## C-6. With that network policy, GitHub, npm, PyPI and the Go proxy answer from the sandbox

- **File:** `.gitlab/duo/agent-config.yml`
- **Proof:** [run3](../evidence/day0/run3-probe-v3-job-log.txt) line 91 (sandbox config,
  175 allowed entries including `dl.k6.io`) and lines 530-535 (step B, exit 0, 148 chars). The
  results (`github.com 200`, `dl.k6.io 200`, `registry.npmjs.org 200`, `proxy.golang.org 200`,
  `pypi.org 200`, `gitlab.com 301`) are in `probe-notes.txt`.
- **Does not show:** that a narrower allowlist is enough (T-14).

## C-7. A server on 127.0.0.1 inside the sandbox answers

- **File:** `flows/probe.yml`
- **Proof:** [run3](../evidence/day0/run3-probe-v3-job-log.txt) lines 545-550 (step C, exit 0,
  10 chars = `local 200\n`).

## C-8. A 150 s command completes

- **File:** `flows/probe.yml`
- **Proof:** [run3](../evidence/day0/run3-probe-v3-job-log.txt) lines 561-586 (step D, exit 0, 27
  chars); timestamps in `probe-notes.txt` are 11:31:48 and 11:34:18.
- **Does not show:** anything about commands near or over 200 s.

---

## C-9. This GitLab project is public

- **File:** GitLab project settings (not a repo file)
- **Proof:** [gitlab-project-2026-10-22.json](../evidence/gitlab-project-2026-10-22.json) is the
  GitLab API response for project 87948026 on 2026-10-22: `"visibility":"public"`,
  `"default_branch":"main"`.
- **Does not show:** that it stays public; visibility can be changed in settings.

---

## C-10. The engine finds a planted regression between two builds and traces it to the changed function

- **File:** `engine/` (CLI in `engine/cmd/downstream`), run by the `engine-e2e` job in
  `.gitlab-ci.yml`
- **Proof:** pipeline 63 (commit `0fa756e`), copied to `evidence/runs/ci-63-engine-e2e/`.
  [Job log](../evidence/runs/ci-63-engine-e2e/raise-hash-cost-job-log.txt) line 77
  (`regression=2`) and line 206 (`Job succeeded`).
  [report.md](../evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md) line 5 (22 regressed
  steps), line 12 (`log_in` p95 2.8 / 1.6 ms to 537.9 ms), line 53 (app CPU time 0.16 s to
  27.10 s), lines 90-96 (the 7 routes that reach `hashSecret`).
- **Check:** `grep -n "regressed steps\|CPU time" evidence/runs/ci-63-engine-e2e/raise-hash-cost/report.md`
- **Does not show:** a run inside the Duo flow; a regression that was not planted; journeys
  written by the model (the run uses `examples/shelf/journeys.example.json`); "one issue per
  cause" (this change gives two issue groups, see
  [evidence/runs.md](../evidence/runs.md)).

## C-11. A clean change comes out clean on the GitLab runner

- **File:** `engine/classify`, run by `engine-e2e` and `engine-noise` in `.gitlab-ci.yml`
- **Proof:** 22 of 22 runs with `regression=0` on `saas-linux-small-amd64`: pipeline 63
  [job log](../evidence/runs/ci-63-engine-e2e/clean-change-job-log.txt) line 77, and pipeline 66
  (commit `baacc58`, branch `measure-noise`), lines 72, 84, 96 and 108 of each
  `noise-<k>-job-log.txt` in [ci-66-engine-noise](../evidence/runs/ci-66-engine-noise/). The
  largest step p95 increase across pipeline 66's 20 runs is 4.6 ms; the regression floor is
  10 ms.
- **Check:** `grep -h "noise ./5 run" evidence/runs/ci-66-engine-noise/*-job-log.txt | grep -v echo`
- **Does not show:** the Duo sandbox's noise (not measured). On a slower machine the same
  thresholds give false positives: 6 of 12 local WSL2 runs flagged the clean change
  ([evidence/runs.md](../evidence/runs.md), "Local WSL2 runs").

---

## C-A1. On identical code the rule reports no regression, and the floor is why

- **Files:** `engine/classify/classify.go`, `scripts/aa.sh`, `scripts/aa_summary.py`
- **Proof:** [evidence/aa/before/summary.txt](../evidence/aa/before/summary.txt) lines 2-3
  and 8-10: 20 A/A runs on a 4-CPU machine, 0 with a false regression; with the 10 ms
  absolute floor removed, the same band and 20% rules would flag 8.6% to 11.8% of latency
  findings and 19 of 20 runs. Raw figures: `evidence/aa/before/passes.tsv`.
- **Check:** `scripts/aa.sh 20 /tmp/aa && python3 scripts/aa_summary.py /tmp/aa`
- **Does not show:** behaviour on the GitLab runner or in the Duo sandbox; behaviour on a
  slower machine (the laptop runs in `evidence/runs.md` had false regressions); that a real
  latency regression under 10 ms would be reported (it would not).

---

## Not built, not measured (no proof exists)

| ID | Claim | Planned file | Blocking test |
|---|---|---|---|
| N-1 | The engine finds regressions inside a flow run | `engine/` (CI runs: C-10, C-11); `flows/downstream.yml` | T-07 |
| N-2 | Journeys are generated from the diff by the model | the model step is in the flow prompt; k6 running hand-written journeys is shown in C-10 | T-07 |
| N-3 | One issue per cause, starter tagged | `engine/issues` (bodies produced in CI; the planted change gives 2 groups for 1 cause); `flows/downstream.yml` | T-08 |
| N-4 | `fix option N` applies a change and reruns the journey | `engine/issues` checks the option number only; applying is not built | T-09, T-10 |
| N-5 | `verify` reruns after merge | `flows/downstream.yml` (written, not run) | T-09 |
| N-6 | Cost in credits of one run | none | T-11 |
| N-8 | The flow's model writes negative, edge-input, repeated, page-size and cross-user steps | `flows/downstream.yml` R6, `agentnotes.md` | no flow session has run; `downstream validate` warnings are the only check |
| N-7 | The GitLab project shows an MIT licence in its sidebar | `LICENSE` | T-12 (the file is committed; GitLab's detection not yet checked) |

The engine's CI runs (C-10, C-11) are runs of the CLI, not of the flow, so N-1 to N-3 stay
unverified until a flow run's output is committed under `evidence/`.

When one of these gets proof, move it to a numbered claim above, add the evidence file under
`evidence/`, and flip its README row to Verified in the same commit.
