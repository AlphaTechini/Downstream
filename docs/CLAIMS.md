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
- **Does not show:** that someone without Developer role can trigger it (they cannot, per
  platform rules; not tested). A mention inside an issue is shown later, in C-13.

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

## C-12. A mention runs the whole flow in the Duo sandbox and opens issues

- **File:** `flows/downstream.yml`, the engine at the commit pinned as `ENGINE_SHA`
- **Proof:** session 9999905 on showcase !11:
  [mr-11-notes.json](../evidence/flow/mr-11-notes.json) line 701 (report: 16 of 16 steps
  regressed, app CPU time 0.11 / 17.69 / 0.12 s), line 785 (issues #4 and #5 opened),
  [issue-4.json](../evidence/flow/session-9999905/issue-4.json) line 5, and the
  [job log](../evidence/flow/session-9999905/job-log.txt) line 808 (`Job succeeded`). Session
  10000162 repeats it: lines 987 and 1071.
- **Does not show:** the sandbox's noise on a clean change; a regression that was not planted.
  The planted change still gives two issues for one cause.

## C-13. `fix it` commits the fix, opens a merge request for the requester and re-measures it

- **File:** `flows/downstream.yml` (FIX steps F1 to F8), `engine/issues` (marker)
- **Proof:** session 10000200 on issue #6:
  [issue-6-notes.json](../evidence/flow/session-10000200/issue-6-notes.json) lines 151 and 235,
  [mr-13.json](../evidence/flow/session-10000200/mr-13.json) line 16 (branch
  `downstream/fix-6-option-1`) and lines 33-36 (reviewer), the change in
  [mr-13-diffs.json](../evidence/flow/session-10000200/mr-13-diffs.json) line 3, and the
  re-measure in [mr-13-notes.json](../evidence/flow/session-10000200/mr-13-notes.json) line 33
  (16 metrics improved, 0 regressed).
- **Does not show:** that every option compiles; the re-measure is the check, and a failed
  build is posted on the fix merge request instead. The fix merge request's author is the
  requester, because GitLab runs the session as the user who mentioned the bot.

## C-14. The same flow runs on another project

- **File:** NewGit's `downstream.yml`, `downstream.md` and `.gitlab/duo/agent-config.yml`
- **Proof:** session 10000842 on NewGit !3:
  [mr-3-notes.json](../evidence/flow/newgit-session-10000842/mr-3-notes.json) line 757
  (journeys grouped as existing and control, from `downstream.md`), line 785 (10 steps
  regressed, app CPU time 0.32 / 1.50 / 0.37 s, control pages slower too), and the
  [job log](../evidence/flow/newgit-session-10000842/job-log.txt) line 697.
- **Does not show:** that static analysis links template helpers to routes; it found no
  changed routes for `avatarHue`, which NewGit calls from templates.

## C-15. A new feature's journeys show its cost to existing pages

- **File:** `engine/runner/runner.go` (new journeys run in pass C), `engine/journey/coverage.go`
- **Proof:** test MR !14 adds `GET /books/{id}/similar`.
  [mr-14-notes.json](../evidence/flow/mr-14-notes.json) line 151: session 10000857, with the
  new journey in a separate pass, reports 0 regressions. Line 381: session 10001024, with it in
  pass C, reports 8 regressed steps and app CPU time 0.10 / 1.72 / 0.11 s; line 437: issue #8.
- **Does not show:** how the cost splits between CPU and lock waiting; C's CPU time includes
  the new feature's own work, as the report says.

## C-17. Under the gated rule a clean change opens no issue, and real regressions are caught

- **File:** `engine/classify` (tolerances), `engine/runner` (process counters). The proof runs used
  engine `84e61cb`; the pinned engine adds a 1 KB per journey floor on bytes written, after CI
  pipeline 2984592625 flagged the clean change on a near-zero baseline
- **Proof:** [gated/mr-16-notes.json](../evidence/flow/gated/mr-16-notes.json) lines 1273,
  1475 and 1677: three clean-change runs with 0 regressed metrics.
  [gated/mr-11-notes.json](../evidence/flow/gated/mr-11-notes.json) line 1991: the planted
  change, CPU per completed journey 2.87 / 2.91 / 451.25 ms (B1 / B2 / C).
  [gated/mr-14-notes.json](../evidence/flow/gated/mr-14-notes.json) line 1211: the new feature,
  3.09 / 3.04 / 94.96 ms. Earlier rounds, with their false alarms, are in `evidence/flow.md`.
- **Does not show:** a false-alarm rate (three runs); that an allocation-only rise under 100
  objects or 16 KB per journey is caught (it is treated as noise by design).

## C-16. GitLab shows the MIT licence, and the flow is public in the AI Catalog

- **File:** `LICENSE`; the catalog item built from `flows/downstream.yml`
- **Proof:** [gitlab-project-2026-10-24.json](../evidence/gitlab-project-2026-10-24.json) lines
  8-11 (`"key": "mit"`), and
  [ai-catalog-downstream-2026-10-24.json](../evidence/ai-catalog-downstream-2026-10-24.json)
  lines 6-7 (`"visibility": "PUBLIC"`, web path `/explore/ai-catalog/flows/1019478`), both
  from the GitLab API on 2026-10-24.
- **Does not show:** that a project outside this group has enabled the flow.

## C-18. With the floors (flow 1.0.12), the live bot passes the clean change and catches the rest

- **File:** `engine/classify/classify.go` (floors: peak memory 10 MB, write syscalls 1 call per
  journey, bytes written 1 KB, allocations 100 objects or 16 KB)
- **Proof:** [floors/mr-16-notes.json](../evidence/flow/floors/mr-16-notes.json) lines 1127, 1329
  and 1531: three clean-change runs with 0 regressed metrics.
  [floors/mr-11-notes.json](../evidence/flow/floors/mr-11-notes.json) line 667: planted change,
  CPU per completed journey 2.87 / 2.79 / 424.64 ms.
  [floors/newgit-mr-3-notes.json](../evidence/flow/floors/newgit-mr-3-notes.json) line 409: the
  NewGit avatar change, 10.38 / 9.75 / 32.28 ms. The earlier rounds that each needed a floor are
  in the same files (lines 151, 437, 639, 897 of mr-16) and in `evidence/flow.md`.
- **Does not show:** a false-alarm rate (three runs). A regression smaller than a floor in only
  that metric is noise by design; the new feature on !14 raised peak memory by 0.7 MB under
  1.0.10, which the 10 MB floor now hides (CPU per journey still flags it).

## C-19. A template change on a server-rendered app is caught by the frontend stage in the sandbox

- **File:** `engine/runner/frontend.go`, `engine/report/frontend.go`, `engine/issues/frontend.go`,
  `tools/fecheck/`, flow step R8F. Engine `831a73e`, flow released privately to NewGit for the test.
- **Proof:** [newgit-mr-5-notes.json](../evidence/flow/frontend/newgit-mr-5-notes.json) line 151:
  session 10003926 on NewGit !5, Frontend section with 12 regressions on 4 model-written page
  journeys (missing asset, image-alt, markup on each page), base against base identical.
  [newgit-issues-5-7.json](../evidence/flow/frontend/newgit-issues-5-7.json) lines 6, 79, 152:
  one issue per cause. [Job log](../evidence/flow/frontend/session-10003926-newgit-mr5-job-log.txt)
  line 641 (`npm ci` of fecheck in the sandbox), line 659 (`downstream frontend`), line 751
  (`Job succeeded`).
- **Check:** `grep -n "Frontend (server build" evidence/flow/frontend/newgit-mr-5-notes.json`
- **Does not show:** a static frontend build in the sandbox (C-20 is local only); a clean
  frontend change in the sandbox; color contrast, layout or paint, which are never checked. The
  note was retyped by the model and one sentence was reworded (`evidence/flow.md`).

## C-20. On real React and Svelte builds, every planted frontend regression is caught and a clean change passes

- **File:** `fixtures/frontend/`, `scripts/verify_frontend.sh` (`make verify-frontend`)
- **Proof:** [frontend-verify.txt](../evidence/frontend-verify.txt) line 61 (React 19.3.1) and
  line 121 (Svelte 5.57.2), both Vite 8.3.5: bundle bytes, missing asset, DOM size,
  accessibility, markup and JS error all regress on the planted change, and the clean change
  has none; line 123 `verify-frontend: PASS`. Lines 23 and 83 name the grown chunk.
- **Check:** `make verify-frontend` on Linux with Go, Node 20.19+ and npm.
- **Does not show:** a run in the Duo sandbox (this ran on a local WSL2 machine, Node 22.23.3);
  Angular, Next.js or server-side rendering frameworks; `frontend_node`, which has not run.

## C-22. The Downstream memory service runs on Google Cloud Run and adds a trend across merged merge requests

- **File:** `engine/memory/` and `engine/cmd/downstream-memory/main.go` (service),
  `engine/history/history.go` (record and trend), `engine/report/trend.go`, `cloud/` (Dockerfile,
  Cloud Build config, `deploy.sh`), flow step R8H. Engine `a5cce4a` on a private experiment flow.
- **Proof:** [cloud-run-downstream-memory.yaml](../evidence/gcp/cloud-run-downstream-memory.yaml)
  line 27 (at most one instance) and line 56 (own service account);
  [firestore-databases.yaml](../evidence/gcp/firestore-databases.yaml) line 9 (`freeTier: true`);
  [probe.txt](../evidence/gcp/probe.txt) lines 5 to 12: a project that has not opted in and a
  commit that is not in NewGit are refused with HTTP 403.
  [newgit-mr-11-notes.json](../evidence/gcp/newgit-mr-11-notes.json) line 151: NewGit !11's
  report with a Trend section and no merged merge requests yet;
  [newgit-mr-3-notes.json](../evidence/gcp/newgit-mr-3-notes.json) line 179: after !11 merged,
  the demo merge request's trend counts it, with CPU per journey +199.5% compounded and flagged.
  [cloud-logging-run-recorded.json](../evidence/gcp/cloud-logging-run-recorded.json): the
  service's log lines for both records.
- **Does not show:** creep built from many small merged changes (only one merged merge request
  is recorded so far, so the flag on !3 comes from !3's own rise); squash merges, which are not
  traced; the live bot using it (the proof ran on the experiment flow). The first log line, at
  00:07, is a manual test write that was deleted afterwards.

## C-21. `fix it` works on frontend issues and on app-wide findings

- **File:** `engine/issues/frontend.go` (notes and state block for frontend issues),
  `engine/issues/issues.go` (app-wide markers carry the run's journeys),
  `engine/cmd/downstream/main.go` (`writeMarkerJourneys`), flow steps R11, R11F and F7. Engine
  `af542dc`, run by a private experiment flow on NewGit.
- **Proof:** [newgit-issues-13-15.json](../evidence/flow/fix-it-frontend/newgit-issues-13-15.json) lines 6, 79 and
  152: three frontend issues with model-written fix options and a state block of kind
  `frontend`. [Job log](../evidence/flow/fix-it-frontend/fix-job-17439455794-issue-15.txt) lines 573 and 588: `fix it`
  on #15 parsed the marker and ran the frontend stage on the fix;
  [newgit-mr-8-notes.json](../evidence/flow/fix-it-frontend/newgit-mr-8-notes.json) line 5: the fix (banner removed,
  [diff](../evidence/flow/fix-it-frontend/newgit-mr-8-diffs.json)) has 0 regressions and 9 page checks improved.
  [Job log](../evidence/flow/fix-it-frontend/fix-job-17439456063-issue-12.txt) lines 585 and 603: `fix it` on the
  app-wide CPU issue #12 parsed the marker's journeys and ran them;
  [newgit-mr-7-notes.json](../evidence/flow/fix-it-frontend/newgit-mr-7-notes.json) line 33: CPU per completed
  journey 32.11 / 32.22 ms on the MR to 9.63 ms on the fix (-70%), improved.
  On the live bot (flow 1.0.14) the CPU path ran again on the demo MR:
  [live-1.0.14-mr-9-notes.json](../evidence/flow/fix-it-frontend/live-1.0.14-mr-9-notes.json)
  line 33, 27.76 / 27.95 ms to 6.84 ms.
- **Does not show:** more than one frontend fix; a frontend fix on a static (React or Svelte)
  build in the sandbox. The model set the requester as reviewer on !7 and !9 but not on !8.

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

## C-A2. With the gated rule, identical code gave no false regression in 20 fresh runs

- **Files:** `engine/classify/classify.go` (`DefaultThresholds`, gated rule), `scripts/aa.sh`
- **Proof:** [evidence/aa/after/summary.txt](../evidence/aa/after/summary.txt) line 2 (0 of
  20 runs) and lines 6-16 (classes per metric); the tolerances were picked from the earlier
  `counters` set, so these runs are out of sample. `TestReplayAARuns` in
  `engine/classify` replays the `before` and `counters` sets and requires no gated
  regression.
- **Check:** `scripts/aa.sh 20 /tmp/aa && python3 scripts/aa_summary.py /tmp/aa`
- **Does not show:** behaviour on the GitLab runner or in the Duo sandbox; the margin for CPU
  per journey is small (largest A/A excess 13.96% against a 20% tolerance,
  after/summary.txt line 19).

---

## C-A3. The gated rule reports both planted regressions and no clean change

- **Files:** `engine/classify/classify.go`, `examples/changes/`, `scripts/plant.sh`
- **Proof:** [evidence/plants/README.md](../evidence/plants/README.md);
  `evidence/plants/final/*/summary-NN.txt` with the final engine: `raise-hash-cost` and
  `remove-list-limit` regression=1 in 3 of 3 runs each, `clean-change` regression=0 in 6 of 6
  runs over two journey sets. Earlier sets (5 runs each) agree, except one clean run under a
  2% peak-memory tolerance (`evidence/plants/rss-2pct/`), which is why the tolerance is 5%.
- **Check:** `scripts/plant.sh remove-list-limit examples/shelf/journeys.thorough.json 3 /tmp/p && cat /tmp/p/summary-*.txt`
- **Does not show:** anything about real, unplanted changes or how often they would be caught;
  behaviour on the GitLab runner or in the Duo sandbox (the `engine-e2e` CI job runs these
  changes, but no log of it under the gated rule is committed).

---

## Not built, not measured (no proof exists)

| ID | Claim | Planned file | Blocking test |
|---|---|---|---|
| N-5 | `verify` reruns after merge | `flows/downstream.yml` (written, not run) | T-09 |
| N-6 | Cost in credits of one run | none | T-11 |
| N-8 | A clean change comes out clean inside the Duo sandbox, as a rate | `flows/downstream.yml` | repeated clean-change sessions (one so far, clean: [mr-16-notes.json](../evidence/flow/session-10001525/mr-16-notes.json) line 151) |

N-1 to N-4 (regressions found in a flow run, model-written journeys, issues opened by the
flow, fixes applied) moved to C-12 to C-15; N-7 (licence shown by GitLab) moved to C-16. N-9 (negative and edge-input journey steps) is shown in session 10002586
([gated/mr-11-notes.json](../evidence/flow/gated/mr-11-notes.json) line 1705).

When one of these gets proof, move it to a numbered claim above, add the evidence file under
`evidence/`, and flip its README row to Verified in the same commit.
