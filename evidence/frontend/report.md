### Downstream report

#### Frontend

**Frontend (static build, 1 journeys, Node 22.23.3, base 25d36ca5, head 8518b39f):** regressions 6, improved 0, withheld 0.

| Journey | Check | Verdict | Detail |
|---|---|---|---|
| home | js bytes (gzip) | regression | 147 to 40397 gzip bytes (+27381.0%); /app-BBBBBBBB.js changed +40250 |
| home | missing assets | regression | /banner.png (404) |
| home | DOM size | regression | 15 to 311 elements (+295 li, +1 img) |
| home | accessibility | regression | image-alt at img[src$="logo.png"] |
| home | markup validity | regression | wcag/h37 at html > body > main > img:nth-child(2) |
| home | journey steps | regression | expected 5 x #list li, got 300 |

Base against base: every page check gave the same result twice, so the changes above are not noise.

Journey steps the validator refused:
- journey bad step 1: visit must be a path starting with "/" on the app origin
- journey bad step 2: selector does not parse: Failed to execute 'querySelectorAll' on 'Element': 'a[[' is not a valid selector.
- journey bad: must start with a valid visit step

Frontend files changed: `src/app-AAAAAAAA.js`, `src/app-BBBBBBBB.js`, `src/index.html`.

Not checked: color-contrast, visual layout, real paint/CPU timing. Pages are rendered in happy-dom, which runs the app's JavaScript but has no layout or paint.
