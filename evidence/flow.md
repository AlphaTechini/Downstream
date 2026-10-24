# Flow runs on GitLab

Sessions of the `downstream` flow on 2026-10-24, started by a mention of
`@ai-downstream-gitlab-ai-hackathon`. Raw files are under [flow/](flow/): the CI job log of each
session (the account owner's email replaced by `<redacted-email>`) and the merge request notes,
issues and merge requests as returned by the GitLab API, indented for line references. Figures
below are copied from those notes, which hold the engine's report text.

## Showcase, test MR !11 (planted `hashSecret` regression, `examples/changes/raise-hash-cost.patch`)

The first three sessions each stopped at one difference between the Duo sandbox and CI, and
the flow posted the error as instructed. Each was fixed before the next session.

| Session | Result | Fix | Source |
|---|---|---|---|
| 9999625 | Stopped at S1: `git clone` of this repo from gitlab.com refused without credentials | The flow downloads the pinned engine as a tarball from the GitHub mirror | [mr-11-notes.json](flow/mr-11-notes.json) L123 |
| 9999714 | Stopped at S1: `go: creating work dir: stat /tmp/claude` (the sandbox sets `TMPDIR` to a folder that does not exist) | The flow sets `TMPDIR=/tmp/ds/tmp` | [mr-11-notes.json](flow/mr-11-notes.json) L297 |
| 9999781 | Journeys written and validated, blast radius traced; stopped at the baseline check: `open /proc/1399/stat: no such file or directory` (the sandbox gives commands their own pid namespace but keeps the outer `/proc`) | `procDir` in `engine/runner/proc.go` finds the app by parent and `NSpid` | [mr-11-notes.json](flow/mr-11-notes.json) L499 |
| 9999905 | Full run: 16 of 16 steps regressed, app CPU time 0.11 / 17.69 / 0.12 s (B1 / C / B2); 2 issues opened (#4, #5) | none needed | [mr-11-notes.json](flow/mr-11-notes.json) L701, L785; [issue-4.json](flow/session-9999905/issue-4.json) L5; [job log](flow/session-9999905/job-log.txt) L808 |
| 10000079 | `fix it` on #4 stopped safely at F1: the issue had no marker. GitLab's issue tools drop HTML comments, and the marker was one | The marker moved to a fenced line in a collapsed "Downstream state" section | [issue-4-notes.json](flow/session-10000079/issue-4-notes.json) L151 |
| 10000162 | Full run: 21 steps regressed, app CPU time 0.15 / 24.83 / 0.15 s; issues #6 and #7 opened with the new marker | none needed | [mr-11-notes.json](flow/mr-11-notes.json) L987, L1071; [issue-6.json](flow/session-10000162/issue-6.json) L5-6; [job log](flow/session-10000162/job-log.txt) L767 |

## Showcase, `fix it` on issue #6 (session 10000200)

The flow read the marker, posted "Applying option 1 for @AlphaTechini", committed the change on
`downstream/fix-6-option-1`, opened !13 into `test/raise-hash-cost` with the requester as
reviewer, re-ran the issue's journey on !11's head against the fix and posted the report on !13.
It did not merge.

| What | Value | Source |
|---|---|---|
| Notes on the issue | "Applying option 1 …", then "the fix is in !13 …" | [issue-6-notes.json](flow/session-10000200/issue-6-notes.json) L151, L235 |
| Fix merge request | branch `downstream/fix-6-option-1`, reviewer `AlphaTechini` | [mr-13.json](flow/session-10000200/mr-13.json) L16, L33-36 |
| The change | `hashTokenSecret` (one SHA-256) for API token secrets in `examples/shelf/auth.go`; passwords keep `hashSecret` | [mr-13-diffs.json](flow/session-10000200/mr-13-diffs.json) L3 |
| Re-measure on !13 | 16 metrics improved, 0 regressed; logged-in steps about 66 ms to 0.2-0.6 ms p95; app CPU time 8.80 / 2.37 / 8.86 s | [mr-13-notes.json](flow/session-10000200/mr-13-notes.json) L33 |
| Session ended | job succeeded | [job log](flow/session-10000200/job-log.txt) L690 |

GitLab records the merge request's author as the user who mentioned the bot; the bot requested
the review and posted the notes.

## Showcase, test MR !14 (new route `GET /books/{id}/similar`, scored under the store's write lock)

The scan found the new route and five affected routes; the model wrote existing, new-feature
and control journeys; every route in the change was covered.

| Session | How the new-feature journey ran | Result | Source |
|---|---|---|---|
| 10000857 | In its own pass after B2 | 0 regressions: existing pages were never measured while the feature was in use | [mr-14-notes.json](flow/mr-14-notes.json) L151 |
| 10001024 | In pass C, alongside the compared journeys | 8 steps regressed (`shelf_list` 1.2 / 2.1 ms to 129.4 ms p95, `open_book` 1.0 / 1.5 ms to 64.7 ms); app CPU time 0.10 / 1.72 / 0.11 s; `similar_books` 146.3 ms p95, 0 failures; issue #8 opened | [mr-14-notes.json](flow/mr-14-notes.json) L381, L437; [job log](flow/session-10001024/job-log.txt) L770 |

The engine was changed between the two sessions so that new-feature journeys run in pass C.

## Showcase, refusals and a clean change

| Session | What | Result | Source |
|---|---|---|---|
| 10001522 | `fix option 9` on issue #6, which has options 1 to 4 | Refused at F1 with the engine's text: "option 9 is out of range: this issue has options 1 to 4". No branch or merge request was made | [issue-6-notes.json](flow/session-10001522/issue-6-notes.json) L409; [job log](flow/session-10001522/job-log.txt) L584 |
| 10001525 | Clean change !16 (`examples/changes/clean-change.patch`: `GET /health` also returns the service name) | 0 regressed steps; app CPU time 0.09 / 0.09 / 0.08 s; the `/health` p95 moved by 1 to 2 ms and was classed as noise (below the 10 ms floor) | [mr-16-notes.json](flow/session-10001525/mr-16-notes.json) L151; [job log](flow/session-10001525/job-log.txt) L705 |

One clean-change run in the sandbox is not a false-positive rate; CI's 22 of 22 is.

## NewGit, test MR !3 (`avatarHue` hashes each name 20,000 times)

[NewGit](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/newgit) is a
separate project with third-party Go modules. The same flow is enabled there.

| Session | Result | Source |
|---|---|---|
| 10000341, 10000371 | Stopped at build: `go: could not create module cache: mkdir /root/go: read-only file system`. Fixed in the flow: `GOMODCACHE` and `GOPATH` under `/tmp` | [mr-3-notes.json](flow/newgit-session-10000842/mr-3-notes.json) L151 |
| 10000625 | Built (sandbox git 2.52.0); stopped at app start: `listen unix …/hooks.sock: socket: operation not permitted`. Fixed in NewGit's agent config: `network_policy.allow_all_unix_sockets: true` | [mr-3-notes.json](flow/newgit-session-10000842/mr-3-notes.json) L555 |
| 10000842 | Full run with journeys grouped as existing and control: 10 steps regressed, 2 withheld; app CPU time 0.32 / 1.50 / 0.37 s; control pages slowed too (raw file 25.3 to 56.1 ms, health 10.8 to 28.7 ms p95); issue #1 opened | [mr-3-notes.json](flow/newgit-session-10000842/mr-3-notes.json) L757, L785; [issue-1.json](flow/newgit-session-10000842/issue-1.json) L5; [job log](flow/newgit-session-10000842/job-log.txt) L697 |

The engine's static analysis did not link `avatarHue` to any route: NewGit calls it from HTML
templates through a `template.FuncMap`, which the call graph does not follow. The journeys came
from NewGit's `downstream.md` and the control journeys.

## Not shown here

- Noise of the Duo sandbox as a rate: one clean-change run (10001525) came out clean.
- `verify` after a merge: not run.
- Cost in credits of a session: not measured.
