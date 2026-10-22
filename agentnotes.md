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

## Reviewer summary (the "Journeys added" note)

For each journey: one line for the goal, then a numbered list of steps in plain English,
then one line saying why it is in the run ("changed route", "calls the changed route",
"shares table X"). No jargon, no k6 syntax. A reviewer should be able to say "yes, that
is a thing users do" or "no, add one for Y" in under a minute.

## Classification (initial defaults, to be tuned against logged runs)

- noise: within the baseline noise band (two baseline runs differ by this much on their own).
- regression: a step that passed on baseline now fails; or a latency, CPU, memory or DB
  time metric that is worse than the worse baseline run by more than twice the noise band
  and by at least 20 percent and at least 10 ms (latency) or 10 MB (memory).
- trade-off: one metric clearly better and another clearly worse on the same journey.
  Ask a question in the merge request. Do not open an issue.

## Issues (one per cause)

The engine groups regressed steps by the changed functions on the routes they call and writes
one issue per group; steps on routes the diff does not reach share one more issue. Titles,
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
