# agentnotes.md

Operating notes the Downstream flow reads at the start of every run (explicit read, the
platform does not auto-load this file name). The last section, "Development notes", is for
agents changing this repository; the flow ignores it at run time. A maintainer edits this
file; the flow never does. If this file and the flow prompt disagree, the flow prompt's
"rules that never bend" win.

## Journeys

A journey is what a real user does, in order, with their session state carried between
steps. It is not a list of endpoints.

- 3 to 6 journeys per run. Fewer, realistic ones beat many shallow ones.
- Each journey must touch the changed route, or a route listed in the engine's blast
  radius for it (callers, shared helpers, shared tables, shared middleware).
- Start from the entry a user would use (sign up, log in, list, open, change, leave).
- Include at least one journey that does not touch the changed route but shares a
  dependency with it. That is where silent breakage hides.
- Use only data a user could create through the API. No fixtures that bypass the app.
- Every journey is validated on the baseline build first. A journey that fails on the
  baseline is discarded and the discard is reported, not hidden.
- Name journeys by the user's goal ("reader opens an article and favourites it").

### Coverage per route

A journey of successful requests only passes on most real regressions. Which regressions a
change breaks shows up with the steps below, so for each route the scan lists under
`changed` or `affected` the journeys together include:

1. The happy path.
2. At least one negative step: a missing or invalid token, another user's resource, or
   invalid input. Expect the 401, 403, 404, 409 or 400 the code returns.
3. At least one edge-input step: a negative, empty, oversized or wrong-type value.
4. A repeated request when the route creates data: the same create twice, or the same
   idempotency key twice. The schema allows only Authorization, Content-Type, Accept and X-*
   headers, so an idempotency key must travel in an X-* header.
5. Enough created data to pass the default page size when the route is a list. A journey is
   capped at 12 steps. If one iteration cannot pass the page size, rely on data accumulating
   over the iterations of a pass, and say so in `why_included`. Do not state how many
   iterations that takes: the run's settings say, and you do not know them in advance.
6. A second user (own sign-up and login, names built with `{{uniq}}`) when the route is
   scoped to an owner: the second user reads or changes what the first user created.

Expected statuses come from the head code, never from a guess. Read the code with
`git show <head>:<path>`, where `<path>` is a file in `scan.json` `changed_files` or a
route's `file`. The route's `file` is where the route is registered, so a handler in another
file is not readable this way: leave out a step whose status the files you may read do not
settle. Name the file the statuses came from in `why_included`. A step that expects a status
the baseline does not return discards its whole journey, so a guess costs the journey.

Extract a variable only from a step that expects a 2xx status; a negative step has no
value to extract. `downstream validate` prints a `warning:` line for a changed route covered
only by 2xx steps or by no step, and for an affected route no step reaches. A warning does
not fail validation. Fix the journeys when the code you read settles a status for it,
otherwise say in the "Journeys added" note that the gap remains. A worked example with all
five additions is `examples/shelf/journeys.thorough.json`.

## Reviewer summary (the "Journeys added" note)

For each journey: one line for the goal, then a numbered list of steps in plain English,
then one line saying why it is in the run ("changed route", "calls the changed route",
"shares table X"). No jargon, no k6 syntax. A reviewer should be able to say "yes, that
is a thing users do" or "no, add one for Y" in under a minute.

## Classification

Metrics come in two kinds. Gated metrics decide whether a run has a regression. Latency is
advisory: it never counts toward a verdict, never opens an issue and is never the only
reason for one.

Gated metrics, per app process: write bytes and write syscalls per completed journey, open
file descriptors, threads (kind "counter"), CPU per completed journey (kind "cpu"), peak
memory (kind "memory"); and step failures (kind "failures"). Advisory ("latency"): p50, p95
and mean per step, and the app's total CPU seconds, which depends on how many journeys
completed.

Rule for a counter, CPU or memory metric. Let worse = max(B1, B2), better = min(B1, B2),
band = |B1 - B2| and allowed = max(relative tolerance x worse, absolute tolerance). The
tolerances are in `settings.thresholds.tolerances` of `run.json`.

- regression: C - worse is more than allowed and more than band.
- improved: the mirror, against better.
- withheld: neither of those, and band is more than allowed. This run's baselines disagree by
  more than the tolerance, so a change of the tolerance's size could not be seen.
- noise: otherwise.
- not-measured: a counter that could not be read in some pass. Never a regression.

Smallest detectable change for a finding is max(allowed, band), also given as a percent of
worse. Every finding carries it. A change beyond it is a regression even when the baselines
are noisy, so a large regression is never hidden. Say "not detectable in this run" about
anything below it; do not call a finding below it clean.

- failures: a step that passed on both baseline runs and failed on the candidate.
- trade-off: a gated app metric that is a regression while another gated app metric improved
  by at least as large a fraction of its own baseline. Ask a question in the merge request. Do
  not open an issue. Latency is never re-classed as a trade-off.
- latency (advisory): keeps the older rule so the report can say "slower" or "faster": worse
  than the worse baseline by more than twice the noise band, by at least 20 percent and by at
  least a minimum in ms. A step with no requests in some pass is "not-reached".

## Issues (one per cause)

Only gated findings open issues (see "Classification"). Steps that fail on the candidate
but passed on both baselines are grouped by the changed functions on the routes they call,
one issue per group; failing steps on routes the diff does not reach share one more issue.
Regressions of the app process (CPU per journey, bytes or write syscalls per journey, file
descriptors, threads, peak memory) form one separate issue. Latency is advisory: it is shown
beside a gated finding but never opens an issue by itself. Titles,
figures, the repro and the hidden marker come from the engine and are never edited. Post each
title and body exactly as `downstream issues` prints them.

What you write, in `notes.json`, per finding ID the engine lists in `needs_notes`:
- `likely_cause`: one or two sentences starting with "Likely", naming the changed function
  and how it reaches these steps. If the steps are on routes the diff does not reach, say what
  they share with the changed code (CPU, a lock, a table) and that this is a hypothesis.
- `fix_options`: one to five concrete code changes, each one sentence, most targeted first.
  Say what each costs (for example, a security property it weakens).
- No figures, units, tables or markup. The engine rejects them.

## Tone

Plain, specific, short. State what was measured and what was not. Say "likely" when it is
a hypothesis. No marketing language, no exclamation marks.

---

## Development notes (for agents changing this repository, not used by the flow at run time)

### Keeping the README honest

Take the time for this. It is part of the feature, not an afterthought, and a README that
claims more than the code does is worse than a short one. In the **same commit** as any
feature or any platform test:

1. **Claims table.** Change a row from Not built to Verified only when a committed file under
   `evidence/` shows it working (a job log, `run.json`, a report, an issue export). The Proof
   column must link to that file with line numbers. If the evidence shows it failing, record
   that too and say so in the row.
2. **`docs/CLAIMS.md`.** Add or update the entry: what is claimed, the file, the exact proof,
   a command to check it, and what the proof does not show.
3. **"How it works" and "Duo Agent Platform features used".** Update the status cells. Every
   file path named in the README must exist, or be labelled planned.
4. **"What is mocked or not built".** Remove what is now built. Add anything new that is
   stubbed, mocked or planted. A planted regression is labelled as planted with its commit.
5. **"Measured results".** Add numbers only by reading them from engine output or a log. Never
   type a figure you did not read from a file. Say how many runs it came from.
6. **Status, counts and dates.** Update the status banner, "3 probe sessions, 0 engine runs",
   the roadmap, and any "not written yet" sentence.
7. **`docs/requirements.md` and `docs/userflow.md`.** Record the result of any T-xx test, with
   its evidence. Replace mock-up placeholders with real excerpts as soon as a real run exists,
   and remove the mock-up label only from what is now real.
8. **First ~1,500 words.** They must stand alone: name and one-liner, brief table, problem,
   flow, features used, claims, what is not built. Detail goes lower or into `docs/`.
9. **`make verify`.** Run it. If its output changed, paste the new expected output into the
   README's "30-second check".

### Rules that apply to every commit

- No ghost features: nothing is described that the code does not do, and there are no empty
  stubs, TODO markers or hard-coded results presented as measured ones.
- Nothing in the repository addresses a reviewer, judge or scoring tool. It describes the
  product and the evidence.
- Small, incremental commits. No tool-attribution lines in commit messages.
- A failing test is evidence too. Commit the failing log and update the README to say so.
