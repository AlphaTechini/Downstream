### Downstream report

Candidate `aaaaaaaa` against baseline `11111111`. Run started 2026-10-22T09:00:00Z, ended 2026-10-22T09:01:05Z. 2 VUs, 15 s per pass, k6 v0.56.0.

**Findings:** regressed steps 2, trade-off journeys 1, improved metrics 0, withheld metrics 3. App process regressions: 2.

#### Reader adds a book to their shelf

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `signup` | 14.2 / 14.9 ms | 14.6 ms | -0.3 ms (-2%) | 0.7 ms | noise |
| `add_book` | 46.3 / 48.1 ms | 458.6 ms | +410.5 ms (+853%) | 1.8 ms | regression |
| `add_book` (p50) | 12.0 / 12.6 ms | 120.0 ms | +107.4 ms (+852%) | 0.6 ms | regression |
| `add_book` (mean) | 15.1 / 15.9 ms | 160.0 ms | +144.1 ms (+906%) | 0.8 ms | regression |
| `list_shelf` | 21.0 / 20.2 ms | 21.8 ms | +0.8 ms (+4%) | 0.8 ms | noise |

#### Reader searches the catalog and opens a book

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `search` | 120.5 / 123.0 ms | 60.1 ms | -62.9 ms (-51%) | 2.5 ms | trade-off |
| `open_book` | 90.0 / 93.4 ms | 260.0 ms | +166.6 ms (+178%) | 3.4 ms | trade-off |
| `open_book` (p50) | 25.0 / 26.0 ms | 70.0 ms | +44.0 ms (+169%) | 1.0 ms | trade-off |
| `open_book` (mean) | 33.0 / 34.0 ms | 90.0 ms | +56.0 ms (+165%) | 1.0 ms | trade-off |

#### Returning reader signs in and opens their profile

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `login` | 15.0 / 15.9 ms | 15.4 ms | -0.5 ms (-3%) | 0.9 ms | noise |
| `login` (failures) | 0 / 0 | 30 | +30 | 0 | regression |
| `profile` | 9.0 / 9.1 ms | 9.3 ms | +0.2 ms (+2%) | 0.1 ms | noise |

#### Health check while idle

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `ping` | 20.0 / 40.0 ms | 55.0 ms | +15.0 ms (+38%) | 20.0 ms | withheld |
| `ping` (p50) | 5.0 / 9.0 ms | 6.0 ms | -3.0 ms (-33%) | 4.0 ms | withheld |
| `ping` (mean) | 7.0 / 12.0 ms | 9.0 ms | -3.0 ms (-25%) | 5.0 ms | withheld |

**App process**

| Metric | B1 | C | B2 | Change | Noise band | Verdict |
|---|---|---|---|---|---|---|
| CPU time | 2.31 s | 4.87 s | 2.35 s | +2.52 s (+107%) | 0.04 s | regression |
| Peak RSS | 48.2 MB | 71.9 MB | 48.9 MB | +23.0 MB (+47%) | 0.7 MB | regression |

**p95 per step**

```mermaid
xychart-beta
    title "p95 per step: candidate against the worse baseline run"
    x-axis ["j1.signup", "j1.add_book", "j1.list_shelf", "j2.search", "j2.open_book", "j3.login", "j3.profile", "j4.ping"]
    y-axis "p95 (ms)"
    bar [14.9, 48.1, 21.0, 123.0, 93.4, 15.9, 9.1, 40.0]
    line [14.6, 458.6, 21.8, 60.1, 260.0, 15.4, 9.3, 55.0]
```

Bars: worse of the two baseline runs. Line: candidate.

- j1: Reader adds a book to their shelf
- j2: Reader searches the catalog and opens a book
- j3: Returning reader signs in and opens their profile
- j4: Health check while idle

**Issues to open**

- `shelf.add_book.p95_ms`
- `signin.login.failures`

**Discarded journeys**

- `export`, step `download`: expected status 200, got 404 on the baseline

**Blast radius**

Changed routes:

- `POST /shelf/books`: handler addBook calls changed function store.AddBook

Affected routes:

- `GET /shelf`: reads the shelf table that store.AddBook writes

*Go AST route table and static call graph, depth 2; calls through interfaces and reflection are not followed.*

**Not measured**

- DB time: not measured; the target exposes no database timing.

**Method.** Baseline run twice (B1, B2) and candidate once (C), in the order B1, C, B2, sequentially on one machine. Three runs give no confidence interval; treat verdicts near the thresholds as uncertain.

Thresholds in effect: a metric is a regression when the candidate is worse than the worse baseline run by more than 2 times the noise band (the difference between B1 and B2), by at least 20%, and by at least 10 ms (latency) or 10 MB (memory). A step that passed on both baseline runs and failed on the candidate is a regression. The verdict is withheld when the baseline runs differ by more than 50% of the smaller one. A journey with a latency regression and a latency improvement is reported as a trade-off.
