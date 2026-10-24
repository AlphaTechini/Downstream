# Frontend fixtures

Two small shops used by `make verify-frontend`
([scripts/verify_frontend.sh](../../scripts/verify_frontend.sh)). Each is a frontend-only
target: its [downstream.yml](react-vite/downstream.yml) declares `frontend_build` and
`frontend_out` and no backend.

| Folder | Stack |
|---|---|
| [react-vite/](react-vite/) | React 19.3.1, Vite 8.3.5, @vitejs/plugin-react 6.1.2 |
| [svelte-vite/](svelte-vite/) | Svelte 5.57.2, Vite 8.3.5, @sveltejs/vite-plugin-svelte 7.3.1 |

The exact versions are pinned and `npm audit` reported 0 vulnerabilities for both lockfiles on
2026-10-24.

## Layout of each fixture

- The base app: a product list and a "Show reports" button that lazy-loads a reports chunk,
  so the check runs Vite's code-split preload path.
- `plant/` is copied over the base for the planted head. To find the planted regressions
  visit [react-vite/plant/src/Reports.jsx](react-vite/plant/src/Reports.jsx): a 30 KB data
  module ([catalog.js](react-vite/plant/src/catalog.js)), one table row per entry, an image
  with no alt text that is not in the build, and an uncaught error thrown from a timer.
- `clean/` is copied over the base for the clean head: a text change that must report no
  regression.
- [journeys.json](react-vite/journeys.json) holds the two page journeys both runs use.

The error is thrown from a timer on purpose: thrown inside a React effect, React unmounts the
whole tree, and the other planted problems would disappear with it.
