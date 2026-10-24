# fecheck

The browser-free page checker behind Downstream's frontend stage. It renders each page in
[happy-dom](https://github.com/capricorn86/happy-dom), running the app's own production
JavaScript, and compares what two builds send and render. There is no browser binary, so it
installs in seconds and runs in the GitLab sandbox.

```
node fecheck.mjs validate --journeys journeys.json --out checked.json
node fecheck.mjs collect  --url http://127.0.0.1:4101 --journeys checked.json --out base.json
node fecheck.mjs compare  --base base.json --head head.json [--base2 base2.json] [--out verdict.json]
```

The engine runs these commands itself (`downstream frontend`); a person rarely needs to.

## Files

- To find the page run (requests and their raw, gzip and brotli sizes, DOM size, axe-core,
  html-validate, uncaught errors, journey steps) visit [collect.mjs](collect.mjs).
- To find the verdict rules and the base-against-base noise check visit [compare.mjs](compare.mjs).
- To find journey validation (step format, limits, same-origin paths, selectors that parse)
  visit [validate.mjs](validate.mjs).
- To find the `import.meta` rewrite, content-hash stripping and the settle cap visit
  [helpers.mjs](helpers.mjs).
- The command line can be found in [fecheck.mjs](fecheck.mjs).
- Tests: [test/](test/), run with `npm test` (Node's built-in test runner).

## Rules

| Check | Regression when |
|---|---|
| JS, CSS, image, font, HTML bytes (gzip) | more than 5% **and** more than 1024 bytes |
| DOM elements | more than 20% **and** more than 100 elements |
| Request count, missing assets (4xx/5xx), axe violations, markup errors, uncaught JS errors, failing journey steps | any new one |

These checks are deterministic, so there is no noise band. Instead the base build is collected
twice; a check that differs between the two base runs is withheld, not reported. A journey
whose steps already fail on the base build is reported as broken and not compared.

## Decisions and trade-offs

- **happy-dom, not a real browser.** No layout or paint, so color contrast, visual layout and
  real paint or CPU timing are never checked, and every report says so. The render time is
  recorded as advisory only. In exchange there is no browser download of over 100 MB, and the result
  is the same on every run.
- **`import.meta` is rewritten.** happy-dom's module loader cannot evaluate `import.meta`,
  which Vite and Angular use for preload URLs, so each module's `import.meta` becomes a literal
  of its own URL.
- **Other origins answer 204.** Only the app's own loopback origin is fetched; analytics,
  fonts and CDNs never leave the machine.
- **The app's JavaScript runs in a Node VM.** This is not a security boundary. It is the same
  trust the sandbox already gives the build command, which also runs merge request code.
- **Bytes are attributed to assets** by hash-stripped name (`index-BQx1u_j8.js` to
  `index.js`); repeated names such as Angular's `chunk-HASH.js` are paired by nearest size.

## Dependencies

Pinned in [package.json](package.json) and [package-lock.json](package-lock.json): happy-dom
21.0.4, axe-core 4.14.0, html-validate 10.17.0. The flow installs them with `npm ci
--legacy-peer-deps` in the sandbox.
