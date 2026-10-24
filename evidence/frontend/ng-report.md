### Downstream report

#### Frontend

**Frontend (server build, 3 journeys, Node 22.23.3, base 7f247327, head e85e350d):** regressions 9, improved 0, withheld 0.

| Journey | Check | Verdict | Detail |
|---|---|---|---|
| home | missing assets | regression | /static/promo-banner.png (404) |
| home | accessibility | regression | image-alt at img |
| home | markup validity | regression | wcag/h37 at html > body > header > img |
| repo | missing assets | regression | /static/promo-banner.png (404) |
| repo | accessibility | regression | image-alt at img |
| repo | markup validity | regression | wcag/h37 at html > body > header > img |
| issues | missing assets | regression | /static/promo-banner.png (404) |
| issues | accessibility | regression | image-alt at img |
| issues | markup validity | regression | wcag/h37 at html > body > header > img |

Base against base: every page check gave the same result twice, so the changes above are not noise.

Frontend files changed: `internal/web/templates/layout.html`.

Not checked: color-contrast, visual layout, real paint/CPU timing. Pages are rendered in happy-dom, which runs the app's JavaScript but has no layout or paint.
