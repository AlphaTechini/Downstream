# Frontend stage evidence

Raw outputs of `downstream frontend` run end to end. All runs here were on a local WSL2
machine (Linux, Node 22.23.3, Go 1.22.12), not in the GitLab sandbox. The flow's own
frontend runs are recorded in [../flow.md](../flow.md) once they exist.

## Fixture runs: `make verify-frontend`

[../frontend-verify.txt](../frontend-verify.txt) is the full output of
[scripts/verify_frontend.sh](../../scripts/verify_frontend.sh) on the two fixtures in
[fixtures/frontend/](../../fixtures/frontend/). Each is a real Vite 8.3.5 build: React 19.3.1
and Svelte 5.57.2.

| What | Result | Line |
|---|---|---|
| React, planted change: regressions | 6: bundle bytes, missing asset, DOM size, accessibility, markup, JS error | L14, L61 |
| React, planted change: bundle growth and culprit | 68814 to 73929 gzip bytes (+7.4%), `/assets/Reports-CMCplyA2.js` +5112 | L23 |
| React, clean change: regressions | 0 | L47 |
| Svelte, planted change: regressions | 6, the same kinds | L74, L121 |
| Svelte, planted change: bundle growth and culprit | 14518 to 20268 gzip bytes (+39.6%), `/assets/Reports-Duhjbbda.js` +5184 | L83 |
| Svelte, clean change: regressions | 0 | L107 |
| Overall | `verify-frontend: PASS` | L123 |

## Static app with no dependencies

[fe-e2e.sh](fe-e2e.sh) builds a tiny app whose head commit grows the bundle, renders 300 rows,
drops an alt text and references a missing image. Output: [e2e.log](e2e.log),
[report.md](report.md), [frontend.json](frontend.json), [issues.json](issues.json).

- 6 regressions on one journey ([e2e.log](e2e.log) L11, [report.md](report.md) L9-L14).
- The validator dropped the three bad steps of a journey that left the origin and used a
  selector that does not parse ([report.md](report.md) L18-L21).
- A README-only change is skipped ([e2e.log](e2e.log) L44, [skip.json](skip.json) L3).

## Server-rendered app: NewGit

[fe-ng.sh](fe-ng.sh) clones NewGit, adds `<img src="/static/promo-banner.png">` to the top bar
template and checks three pages. NewGit renders its own HTML, so no frontend build is declared
and the engine points the checker at the running app. Output: [ng.log](ng.log),
[ng-report.md](ng-report.md), [ng-frontend.json](ng-frontend.json), [ng-issues.json](ng-issues.json).

- 9 regressions: missing asset, image-alt and markup on each of the three pages
  ([ng.log](ng.log) L9, [ng-report.md](ng-report.md) L9-L17).
- They are grouped into 3 issues, one per cause ([ng-issues.json](ng-issues.json)).
- Base against base gave the same result twice ([ng-report.md](ng-report.md) L19).
