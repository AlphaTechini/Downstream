# agentnotes.md

Operating notes the Downstream flow reads at the start of every run (explicit read, the
platform does not auto-load this file name). A maintainer edits this file; the agent never
does. If this file and the flow prompt disagree, the flow prompt's "rules that never bend"
win.

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

## Issues (one per regression)

Title: `[Downstream] <journey>: <metric> <direction> <amount>` (amount from the engine).
Body, in order: who started the run (tag them), repro (the journey steps and the command
to rerun it), baseline and candidate figures with the noise band, likely cause (one or two
sentences, labelled "likely"), numbered fix options, how to reply (`@<handle> fix option N`).
The hidden marker comes from the engine and is never edited.

## Tone

Plain, specific, short. State what was measured and what was not. Say "likely" when it is
a hypothesis. No marketing language, no exclamation marks.
