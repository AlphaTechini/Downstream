# Downstream: technical implementation

This document explains how Downstream works and why it is built the way it is, in plain
English. It is written for a judge, a reviewer or an AI assistant reading the repository for the
first time. Every number below links to a committed file in [`evidence/`](evidence/), and
every limit is stated where it applies. The short version is in the [README](README.md); the
full design is in [`docs/details.md`](docs/details.md); each claim with its proof and its limits
is in [`docs/CLAIMS.md`](docs/CLAIMS.md).

## Contents

1. [The idea in one paragraph](#1-the-idea-in-one-paragraph)
2. [What happens after one mention](#2-what-happens-after-one-mention)
3. [Real projects it is built for](#3-real-projects-it-is-built-for)
4. [The engineering, piece by piece](#4-the-engineering-piece-by-piece)
5. [Safety: the model never holds the keys](#5-safety-the-model-never-holds-the-keys)
6. [Running inside GitLab's sandbox](#6-running-inside-gitlabs-sandbox)
7. [How the claims were tested](#7-how-the-claims-were-tested)
8. [Limits](#8-limits)
9. [Where the code is](#9-where-the-code-is)

---

## 1. The idea in one paragraph

Code review shows what a change says. It does not show what the change does to the parts of the
application nobody opened in the diff. Downstream closes that gap inside GitLab. A reviewer
mentions the Downstream bot on a merge request. The bot builds the old version (the target
branch) and the new version (the merge request) side by side in GitLab's Duo sandbox, replays
realistic user journeys against both, and compares how each build behaved: how much CPU each
journey cost, how much it wrote, how much memory it held, whether any step broke, and, when
pages changed, what the browser would receive and render. It reports only differences larger
than the noise it measured in the same run, opens one issue per cause with fix options, and,
when asked with `fix it`, commits a fix on a new branch and measures that too. It never merges.

## 2. What happens after one mention

```text
@downstream on a merge request
  |
  |  1. Setup        download the engine at a pinned commit, build it (Go, standard library)
  |  2. Scan         read the diff, find changed routes and the routes that share code with them
  |  3. Journeys     the model writes user journeys as JSON; the engine validates them
  |  4. Measure      build both versions; run the journeys on old, new, old again (k6)
  |  5. Decide       compare process counters against the noise of the two old runs
  |  6. Frontend     if pages changed: render old and new pages without a browser, compare
  |  7. Report       the engine renders the note; the model posts it unchanged
  |  8. Issues       one issue per cause, with numbered fix options, the requester tagged
  v
"fix it" on an issue  ->  commit on a new branch, open a merge request, re-measure, never merge
```

The division of labour is strict. The language model does what it is good at: reading code,
choosing realistic journeys, explaining a likely cause in a sentence, and suggesting fixes. The
engine, a Go program in [`engine/`](engine/), does everything that must be exact: building,
running, measuring, deciding, and rendering every number, table and chart. The model never
writes a figure and never writes a shell command.

## 3. Real projects it is built for

A demo that only works on its own sample app is not much use. Downstream was designed around the
question a judge or an engineering team would ask first: "will it work on my repository?" Real
teams do not change only backend code. A typical week has API changes, template changes,
frontend bundle changes and full-stack changes. Each shape below is handled, and the table says
which ones have been shown and where.

| Your project looks like | What you add | What Downstream checks | Shown |
|---|---|---|---|
| A Go API (JSON over HTTP) | `downstream.yml`: how to build and start it | User journeys through the API: CPU, writes, memory, allocations, failures | In the Duo sandbox on the demo API ([evidence/flow.md](evidence/flow.md)) |
| A server-rendered web app (Go templates) | the same `downstream.yml` | The API journeys, plus every page the change can alter, rendered and compared | In the Duo sandbox on [NewGit](https://gitlab.com/gitlab-ai-hackathon/transcend-october-2026/35141659/newgit), a GitLab-like app ([C-19](docs/CLAIMS.md)) |
| A single-page app (React, Svelte, Vite) | `frontend_build` and `frontend_out` in `downstream.yml` | Bundle size by asset, missing files, DOM size, accessibility, markup, JavaScript errors, broken page steps | Locally on real React 19 and Svelte 5 builds ([C-20](docs/CLAIMS.md)) |
| Full stack in one repository (SPA plus Go API) | both sets of keys | Both stages; the engine serves the built frontend and proxies `/api/` to the backend | Built and unit-tested ([`fe_static_test.go`](engine/runner/fe_static_test.go)); not yet run end to end |
| Frontend only, no backend | only the `frontend_*` keys | The frontend stage alone | Locally, the fixtures above are frontend-only |
| A change that touches no page | nothing | The frontend stage is skipped: the report has no Frontend section, and the engine's own result says `skipped` | In the Duo sandbox, showcase !16 on flow 1.0.13 ([live-1.0.13-mr-16-notes.json](evidence/flow/frontend/live-1.0.13-mr-16-notes.json) line 151); the `skipped` result locally ([skip.json](evidence/frontend/skip.json)) |

### Scenario A: a backend change that slows something else

A developer changes how API tokens are hashed. The diff is small and looks safe. Downstream's
scan finds every route that calls the changed function, and also the routes that share code with
those routes, then has the model write journeys that sign up, log in and use the API. Running
those journeys on both builds shows CPU per completed journey rising from 2.87 ms to 424.64 ms
on the same journeys ([floors/mr-11-notes.json](evidence/flow/floors/mr-11-notes.json) line 667).
One issue is opened, naming the changed function, with fix options. `fix it` on that issue
committed a fix on a new branch, opened a merge request for the requester, re-measured it and
showed the cost gone (merge request !13, [C-13 in docs/CLAIMS.md](docs/CLAIMS.md)).

### Scenario B: a new feature that costs the rest of the app

A new endpoint can be fast on its own and still make every other page slower, for example by
holding a shared lock. Downstream runs the new feature's journeys in the same pass as the
existing ones, so existing pages are measured while the feature is in use. On the demo API a new
"similar books" route left 8 existing steps slower while it was in use; measured in a separate
pass, the same change looked clean ([mr-14-notes.json](evidence/flow/mr-14-notes.json) lines
151 and 381). That is exactly the regression a reviewer cannot see in a diff.

### Scenario C: a template change on a server-rendered app

On NewGit a test merge request added a banner image to the shared top bar template, with no alt
text and no image file behind it. Nothing in Go changed, so the backend part correctly found
nothing. The frontend stage rendered 4 pages on both builds and reported 12 regressions: the
missing file, the missing alt text and the markup error, on every page that uses the layout. It
grouped them into 3 issues, one per cause, not 12
([newgit-mr-5-notes.json](evidence/flow/frontend/newgit-mr-5-notes.json) line 151).

### Scenario D: a frontend bundle change in React or Svelte

In the fixtures a change makes the lazily loaded "reports" chunk import a 30 KB data module,
render 600 rows, add an image with no alt text that is not in the build, and throw an
uncaught error. On real Vite builds Downstream reported all six kinds of regression and named
the chunk responsible, for example `/assets/Reports-CMCplyA2.js` growing by 5112 bytes
(gzip) in React. A harmless text change on the same app reported nothing
([frontend-verify.txt](evidence/frontend-verify.txt) lines 23, 47, 61, 121).

### Scenario E: things go wrong

Real repositories fail to build, time out, or need tools the sandbox lacks. Downstream never
turns a failure into a verdict. The backend run writes `run.json` even when it stops, naming
the stage and the error, and the report says where it stopped. The frontend stage always writes
its result and exits cleanly: if any part fails, its status is **withheld**, with the stage and
the error, and nothing is claimed ([evidence/frontend/README.md](evidence/frontend/README.md)
shows a withheld and a skipped result). A frontend failure never blocks the backend report.

### Trying it on your own repository

1. Enable the public [`downstream` flow](https://gitlab.com/explore/ai-catalog/flows/1019478)
   from GitLab's AI Catalog in your project and choose the mention trigger.
2. Add `downstream.yml` at the repository root. A backend needs five lines:

   ```yaml
   root: .
   build: go build -o {out} ./cmd/app
   start: "{out}"
   port_env: PORT
   ready_path: /health
   ```

   A frontend build adds two lines (or is the whole file for a frontend-only repository):

   ```yaml
   frontend_build: npm run build
   frontend_out: dist
   ```

3. Copy [`.gitlab/duo/agent-config.yml`](.gitlab/duo/agent-config.yml) to install k6.
4. Optionally add `downstream.md` describing test data and key pages, for the journey writer.
5. Mention the bot on a merge request.

The same bot serves every project. The engine is fetched at a pinned commit, not copied into
your repository, so there is nothing else to install.

## 4. The engineering, piece by piece

### 4.1 Measuring against noise measured in the same run

Performance numbers are noisy. A run that compares one old measurement with one new measurement
will cry wolf. Downstream runs the old build twice, before and after the new one (B1, then C,
then B2). The two old runs bracket the new one, so any drift on the machine during the run
shows up as disagreement between B1 and B2, and is counted as noise instead of being blamed on
the change. The builds run one after the other, never at the same time, because on a 2-CPU
sandbox two builds running together would slow each other and call it a regression.

### 4.2 Deciding on counters, not latency

The first version decided on latency (p95 per step). Identical code run against itself (an
"A/A" test) showed why that is fragile: the two baseline runs differed by 32.5% at the median
and 105.8% at the 90th percentile ([evidence/aa/before/summary.txt](evidence/aa/before/summary.txt)
line 10). Process counters were far steadier on the same code: over 20 runs the new run never
exceeded the worse baseline by more than 0.35% in bytes written or 1.51% in write syscalls
([evidence/aa/counters/summary.txt](evidence/aa/counters/summary.txt) lines 15 and 16).

So the verdict is gated on what the process actually did:

- **CPU per completed journey**, from the process's CPU ticks over the k6 window.
- **Bytes written per journey**, from `/proc/<pid>/io`, with the bytes sent back to k6
  subtracted, so a larger response is not mistaken for disk writes.
- **Write syscalls**, **peak memory**, **open file descriptors**, **threads**.
- **Go runtime counters**: allocations, bytes allocated, GC cycles and goroutines. These are
  read from inside the app without changing its code: the engine adds one extra file at build
  time through Go's `-overlay` build flag, which exposes the runtime's counters on a separate
  loopback port. The repository is never edited.
- **Step failures**: a step that passed on both old runs and fails on the new one.

Latency is still measured and shown, as advisory. It never opens an issue on its own.

### 4.3 Tolerances with floors, tuned on real false alarms

Each gated metric has a tolerance with two parts, a percentage and an absolute floor:
`allowed = max(percentage x worse baseline, floor)`. A change counts only if it exceeds both the
tolerance and the gap between the two old runs. The percentages came from A/A runs, at about
two to three times the largest difference seen on identical code. The floors came from running
the live bot and fixing each false alarm at its root: a rise of 0.3 MB on a 6 MB app is over 5%
but means nothing, so peak memory also needs 10 MB; write syscalls need 1 call per journey;
bytes written 1 KB; allocations 100 objects or 16 KB. Each floor went in through its own merge
request with the evidence that motivated it ([evidence/flow.md](evidence/flow.md)). On the
current version the clean change came out clean in 3 of 3 live runs while the planted change and
the NewGit feature were still caught ([C-18](docs/CLAIMS.md)).

Every finding in the report prints its tolerance, the noise band and the smallest change the
run could have detected, so a reader can see why something was or was not flagged.

### 4.4 Finding what a change can affect

The scanner ([`engine/scan`](engine/scan/)) parses the Go source with `go/ast`, builds the
route table from `net/http` registrations, maps each route to its handler and middleware, and
walks calls to depth 2. A route is **changed** when it reaches a changed function, and
**affected** when it shares a function, a type or a package variable with a changed route.
Changing a constant marks every function that uses it. Functions used by more than half of all
routes (a JSON writer, say) are ignored for sharing, because they carry no signal. The scan also
finds routes the merge request adds, so the new feature gets its own journeys.

### 4.5 Journeys the engine can trust

The model writes journeys as JSON against a schema the engine prints. The engine then checks
them before anything runs:

- **Structure**: allowed methods, paths that exist in the route table, no outside URLs, size
  caps.
- **Coverage**: every changed and affected route must be reached, every new route must have a
  new-feature journey, and at least one control journey must stay away from the change, to
  catch app-wide effects. A set that leaves a route out is rejected with the reason.
- **Depth**: for each changed route the model is asked for a negative step, an edge input, a
  repeated request and a second user where they apply, with each expected status read from the
  head code. The engine warns when a changed route is covered only by happy paths.
- **Baseline check**: each journey runs once on the old build first. A journey that is already
  broken there is discarded and listed, so the change is never blamed for an existing bug.

The model's journeys are not turned into code. They are embedded as data in a script the engine
generates around one fixed interpreter, so no model text becomes script source.

### 4.6 The frontend stage

Real changes are not only backend changes, so Downstream also checks pages. It does this
without a browser. A browser download is over 100 MB, slow to start in a sandbox, and its
timings are noisy. Instead, the pages are rendered with [happy-dom](https://github.com/capricorn86/happy-dom),
a browser environment written in JavaScript that runs the app's own production JavaScript.

- **Detection.** `downstream frontend --detect` lists the changed files that can alter a page:
  markup, styles, scripts, components, images, fonts, templates and the frontend's package
  files. If there are none, the stage is skipped.
- **Two modes.** With `frontend_build` declared (static mode), the engine runs `npm ci` and the
  build for both versions and serves the output itself with single-page-app rules: real files
  are served, a missing file with an extension is a 404 (so a broken asset shows up), every
  other path gets `index.html` (so client-side routes load), and `/api/` is proxied to the
  backend when there is one. When the second build has the same lockfile, it reuses the first
  install through hard links instead of installing again. Without frontend keys (server mode),
  the backend is built and started and its own pages are visited.
- **Page journeys.** The model writes short page journeys (visit, click, type, expect text,
  expect a count). The checker validates them: paths stay on the app, selectors must parse,
  limits apply, and a visit to the home page is always included. A journey that already fails
  on the old build is reported as broken and not compared.
- **What is measured.** Every request with its raw, gzip and brotli size; files referenced by
  the page that do not exist; DOM size; [axe-core](https://github.com/dequelabs/axe-core)
  accessibility violations; [html-validate](https://html-validate.org/) markup errors; uncaught
  JavaScript errors; failing page steps.
- **Making modern bundles run.** Vite and Angular builds use `import.meta`, which happy-dom
  cannot evaluate, so the checker rewrites it to each module's own URL as it loads. Apps that
  poll forever never go idle, so waiting for a page to settle is capped. Requests to any other
  origin (analytics, fonts, CDNs) are answered locally with an empty response and never leave
  the machine.
- **Deciding.** These checks are deterministic, so there is no fuzzy noise band. Bundle growth
  must exceed both 5% and 1 KB; DOM growth both 20% and 100 elements; anything else counts when
  it is new. The old build is checked twice, and any check that differs between those two runs
  is withheld rather than reported.
- **Explaining.** Bundle growth is traced to the asset responsible: content hashes are stripped
  from file names (`index-BQx1u_j8.js` becomes `index.js`) so old and new files can be matched,
  and repeated names are paired by size. Findings across pages are grouped into one issue per
  root cause: one missing image on 4 pages is one issue, not 4.
- **Honesty.** happy-dom has no layout or paint engine, so color contrast, visual layout and
  real paint or CPU timing are never checked, and every frontend report says so in its last
  line.

The checker is [`tools/fecheck/`](tools/fecheck/README.md) (Node, 18 unit tests); the
orchestration is [`engine/runner/frontend.go`](engine/runner/frontend.go).

### 4.7 Issues and the fix loop

Findings are grouped by cause, using the changed functions each regressed journey passes
through, so one slow function gives one issue rather than one per route. Each issue holds the
repro (journeys and steps), the figures, a likely cause and numbered fix options written by the
model, and tags the person who asked. The issue ends with a small "Downstream state" block, so a
later reply such as `fix it` or `fix option 2` can rebuild everything it needs from the issue
alone, with no database.

`fix it` reads the files named in the issue, writes the smallest change that does what the
option says, commits it on a new branch, opens a merge request with the requester as reviewer,
and re-runs the issue's journeys on it. A human reviews and merges. Downstream never merges or
approves, which is the "Supervised" autonomy tier.

The state block carries exactly what the re-measure needs. A step finding carries its journey.
An app-wide finding, such as CPU per journey, carries all of the run's journeys, because the
cost was measured across all of them. A frontend finding carries the page journeys that showed
it and the changed frontend files, and its re-measure runs the frontend stage instead of k6.
On NewGit, `fix it` on a missing-banner issue removed the banner and the re-measure showed 0
regressions and 9 page checks improved; on a CPU issue it replaced a repeated hash and CPU per
journey fell from 32.11 ms to 9.63 ms ([C-21](docs/CLAIMS.md)).

### 4.8 Memory on Google Cloud: catching slow creep

A sandbox starts empty on every run, so on its own Downstream can only compare a merge request
with its target branch. That misses the most common way apps get slow: many small changes,
each under its tolerance. Ten merge requests that each add 8% CPU per journey all pass, and the
app ends up more than twice as expensive.

The **Downstream memory service** runs on Google Cloud Run with Firestore
([`cloud/`](cloud/README.md), live at https://downstream-memory-1083478229816.us-central1.run.app). For a project that opts in with
`history: on` in `downstream.yml`:

1. After the run, the engine writes a numbers-only record: for each of six continuous metrics
   (CPU, bytes written and write syscalls per journey, heap objects and bytes per journey, peak
   memory), the new build's change against the worse of the two old-build runs. Absolute figures
   are not kept, because the same branch measured 5.2 ms and 10.4 ms of CPU per journey in two
   sandbox sessions; a change measured within one session is comparable across sessions.
2. The flow posts the record to Cloud Run. The service checks the project is on its allowlist
   and that the commit exists on GitLab, stores the record, deletes records older than 30 days,
   logs one line, and answers with the project's earlier records.
3. The engine, back in the sandbox, uses git to keep only records whose head is an ancestor of
   the current base (merge requests that really merged), compounds their changes with this
   merge request's, and the report gains a **Trend** table. A metric whose compounded change
   would reach 25% is flagged as creep.

On NewGit, the run on !11 (a README change) was recorded and !11 was merged; the run on the
demo merge request !3 then counted !11 in its trend and flagged CPU per journey at +199.5%
compounded ([evidence/gcp/](evidence/gcp/README.md)). If Cloud Run cannot be reached, the
report says the trend is unavailable and nothing else changes. The service keeps no code, no
diffs and no route names, and runs on Google Cloud's free tier: both services scale to zero.

## 5. Safety: the model never holds the keys

An agent that runs code from merge requests must assume the merge request is hostile.

- **No model-written shell.** Every command the flow runs is written in the flow file with
  fixed arguments. The model fills in commit SHAs read from GitLab, numbers, and one-line JSON
  inside a quoted heredoc. The engine validates that JSON before using it.
- **No model-written numbers.** Every figure, table and chart comes from the engine.
- **Text is data.** Diffs, issues, notes and file contents are treated as data, never as
  instructions.
- **Loopback only.** The engine sends traffic only to `127.0.0.1`. The frontend checker answers
  every other origin itself.
- **A merge request cannot change its own judge.** The engine and its agent notes come from this
  repository at a commit pinned in the flow. `downstream.yml` is read from the old build's
  commit, so a merge request cannot change how it is measured. The files that steer the agent
  are protected by CODEOWNERS.
- **Pinned, audited dependencies.** The Go engine uses the standard library only. The frontend
  checker pins exact versions in a committed lockfile; the fixtures' lockfiles reported 0
  vulnerabilities in `npm audit` ([fixtures/frontend/README.md](fixtures/frontend/README.md)).

The known gap is stated in [`docs/details.md`](docs/details.md) section 11: the model's JSON
reaches the shell inside a quoted heredoc.

## 6. Running inside GitLab's sandbox

The Duo sandbox is not a normal CI machine, and getting a full build-and-measure engine to run
there meant working through its constraints one at a time. Each is handled in code and recorded
in [evidence/flow.md](evidence/flow.md):

- **No authenticated git clone.** The flow downloads the engine as a tarball of a pinned commit
  from the public GitHub mirror, and builds it there.
- **Read-only home, missing temp directory.** Go's caches and `TMPDIR` are moved under `/tmp`.
- **Its own process namespace.** The sandbox shows `/proc` from outside its PID namespace, so
  the app's own PID does not point at its `/proc` entry. The engine finds the right entry by
  matching the parent PID and the namespace PID ([`engine/runner/proc.go`](engine/runner/proc.go)).
- **Blocked Unix sockets.** Apps that need them (NewGit does) set one switch in their agent
  config.
- **A 200 second limit per command.** The three measured passes share a 120 second budget and
  the frontend stage a 190 second budget, so a run fits.

## 7. How the claims were tested

Nothing in the README is marked verified without a committed file behind it, and
`make verify` checks that mechanically: every link resolves, every verified row cites evidence,
logs hold no unredacted email, and evidence files match their recorded checksums.

| What was tested | Result | Evidence |
|---|---|---|
| Identical code against itself, gated rule, 20 runs | 0 false regressions | [aa/after/summary.txt](evidence/aa/after/summary.txt) |
| Clean change in GitLab CI, 22 runs | 0 flagged | [evidence/runs.md](evidence/runs.md) |
| Planted regressions, 5 runs each, two kinds | caught 5 of 5 and 5 of 5; clean control 0 of 10 | [evidence/plants/README.md](evidence/plants/README.md) |
| Live bot, current floors: clean change, 3 runs | 0 flagged | [floors/mr-16-notes.json](evidence/flow/floors/mr-16-notes.json) lines 1127, 1329, 1531 |
| Live bot: planted change; NewGit feature | caught (CPU per journey 2.87 to 424.64 ms; 10.38 to 32.28 ms) | [floors/](evidence/flow/floors/) |
| `fix it` end to end | fix merge request opened and re-measured, not merged | [C-13](docs/CLAIMS.md) |
| `fix it` on a frontend issue and on an app-wide CPU issue | banner removed: 0 regressions, 9 improved; CPU per journey 32.11 to 9.63 ms | [C-21](docs/CLAIMS.md) |
| Frontend stage in the Duo sandbox, template change | 12 regressions on 4 pages, 3 issues; on the live bot after release, 15 on 5 pages, 3 issues | [C-19](docs/CLAIMS.md); [live-1.0.13-newgit-mr-5-notes.json](evidence/flow/frontend/live-1.0.13-newgit-mr-5-notes.json) line 151 |
| Frontend stage on React and Svelte builds | every planted kind caught, clean change 0 | [frontend-verify.txt](evidence/frontend-verify.txt) |
| Engine unit and integration tests | run in every pipeline (`go-test`, `engine-e2e`) | [`.gitlab-ci.yml`](.gitlab-ci.yml) |

## 8. Limits

These are stated in full in the README's "What is mocked or not built" and in
[`docs/CLAIMS.md`](docs/CLAIMS.md). The main ones:

- Backend route analysis covers Go `net/http` only. Other languages can still use the frontend
  stage and the control journeys, but get no blast radius.
- The sandbox's false-alarm rate is a small sample: 3 clean runs on the current version.
- Static frontend builds have run end to end only on a local machine, not yet in the sandbox.
  `fix it` on frontend issues has one sandbox run so far.
- Color contrast, layout, paint and database time are not measured.
- The model posts the engine's report by copying it, and has twice altered a sentence while
  doing so. The figures come from the engine, but the note is only as faithful as that copy.

## 9. Where the code is

| To find | Visit |
|---|---|
| The flow: every step the agent takes | [`flows/downstream.yml`](flows/downstream.yml) |
| The engine's commands | [`engine/cmd/downstream/main.go`](engine/cmd/downstream/main.go) |
| Route scanning and blast radius | [`engine/scan/`](engine/scan/) |
| Journey schema, validation, coverage and lint | [`engine/journey/`](engine/journey/) |
| k6 script generation | [`engine/k6gen/`](engine/k6gen/) |
| Builds, passes, `/proc` and runtime counters | [`engine/runner/`](engine/runner/) |
| The gated rule and tolerances | [`engine/classify/classify.go`](engine/classify/classify.go) |
| Report rendering | [`engine/report/`](engine/report/) |
| Issue grouping, fix options and the state block | [`engine/issues/`](engine/issues/) |
| The frontend stage | [`engine/runner/frontend.go`](engine/runner/frontend.go), [`tools/fecheck/`](tools/fecheck/README.md) |
| Frontend fixtures | [`fixtures/frontend/`](fixtures/frontend/README.md) |
| Target config parser | [`engine/target/target.go`](engine/target/target.go) |
| Evidence | [`evidence/`](evidence/README.md) |
