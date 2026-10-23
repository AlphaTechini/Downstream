### Downstream report

Candidate `aadf1e2e` against baseline `baacc58f`. Run started 2026-10-23T03:50:27Z, ended 2026-10-23T03:50:30Z. 2 VUs, 10 iterations per VU per pass (cap 60 s), k6 v0.56.0 (commit/50afb99947, go1.23.4, linux/amd64).

**Findings:** regressed steps 0, trade-off journeys 0, improved metrics 0, withheld metrics 0.

#### A new reader signs up, logs in, adds a book and puts it on their shelf

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `sign_up` | 4.4 / 6.5 ms | 1.4 ms | -5.1 ms (-78%) | 2.1 ms | noise |
| `log_in` | 4.4 / 1.6 ms | 3.4 ms | -1.0 ms (-23%) | 2.8 ms | noise |
| `add_book` | 4.4 / 1.2 ms | 3.1 ms | -1.3 ms (-30%) | 3.1 ms | noise |
| `shelf_add` | 3.9 / 1.3 ms | 2.7 ms | -1.2 ms (-31%) | 2.6 ms | noise |
| `shelf_list` | 1.3 / 1.4 ms | 3.0 ms | +1.6 ms (+115%) | 0.1 ms | noise |

#### A reader logs in, reviews a book and reads the reviews

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `sign_up` | 3.9 / 6.2 ms | 1.7 ms | -4.5 ms (-73%) | 2.3 ms | noise |
| `log_in` | 2.3 / 3.8 ms | 3.1 ms | -0.7 ms (-19%) | 1.5 ms | noise |
| `add_book` | 3.8 / 1.9 ms | 5.1 ms | +1.4 ms (+36%) | 1.9 ms | noise |
| `post_review` | 2.7 / 2.7 ms | 1.7 ms | -1.0 ms (-37%) | 0.0 ms | noise |
| `read_reviews` | 3.4 / 0.8 ms | 1.5 ms | -1.9 ms (-57%) | 2.6 ms | noise |

#### A visitor browses books by tag and opens one

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `sign_up` | 4.9 / 2.5 ms | 2.1 ms | -2.8 ms (-57%) | 2.4 ms | noise |
| `log_in` | 2.1 / 2.0 ms | 1.3 ms | -0.7 ms (-35%) | 0.1 ms | noise |
| `add_book` | 2.8 / 2.6 ms | 1.1 ms | -1.7 ms (-61%) | 0.2 ms | noise |
| `browse_tag` | 4.7 / 1.5 ms | 0.7 ms | -4.0 ms (-85%) | 3.1 ms | noise |
| `open_book` | 1.5 / 0.7 ms | 0.9 ms | -0.6 ms (-42%) | 0.8 ms | noise |

#### A reader views their shelf and removes a book

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `sign_up` | 4.5 / 2.0 ms | 2.2 ms | -2.3 ms (-51%) | 2.5 ms | noise |
| `log_in` | 3.5 / 1.3 ms | 2.2 ms | -1.2 ms (-36%) | 2.1 ms | noise |
| `add_book` | 3.3 / 1.7 ms | 2.4 ms | -0.8 ms (-26%) | 1.6 ms | noise |
| `shelf_add` | 1.4 / 0.9 ms | 2.9 ms | +1.5 ms (+102%) | 0.6 ms | noise |
| `shelf_list` | 1.6 / 1.5 ms | 1.1 ms | -0.5 ms (-33%) | 0.2 ms | noise |
| `shelf_remove` | 1.0 / 1.2 ms | 1.0 ms | -0.3 ms (-23%) | 0.2 ms | noise |
| `shelf_list_again` | 1.1 / 1.7 ms | 0.8 ms | -0.8 ms (-50%) | 0.6 ms | noise |

**App process**

| Metric | B1 | C | B2 | Change | Noise band | Verdict |
|---|---|---|---|---|---|---|
| CPU time | 0.16 s | 0.16 s | 0.16 s | +0.00 s (+0%) | 0.00 s | noise |
| Peak RSS | 9.2 MB | 8.5 MB | 9.2 MB | -0.7 MB (-7%) | 0.0 MB | noise |

**p95 per step**

```mermaid
xychart-beta
    title "p95 per step: candidate against the worse baseline run"
    x-axis ["j1.log_in", "j1.add_book", "j1.shelf_add", "j1.shelf_list", "j2.log_in", "j2.add_book", "j2.post_review", "j2.read_reviews", "j3.sign_up", "j3.log_in", "j3.add_book", "j3.browse_tag", "j3.open_book", "j4.sign_up", "j4.log_in", "j4.add_book", "j4.shelf_add", "j4.shelf_list", "j4.shelf_remove", "j4.shelf_list_again"]
    y-axis "p95 (ms)"
    bar [4.4, 4.4, 3.9, 1.4, 3.8, 3.8, 2.7, 3.4, 4.9, 2.1, 2.8, 4.7, 1.5, 4.5, 3.5, 3.3, 1.4, 1.6, 1.2, 1.7]
    line [3.4, 3.1, 2.7, 3.0, 3.1, 5.1, 1.7, 1.5, 2.1, 1.3, 1.1, 0.7, 0.9, 2.2, 2.2, 2.4, 2.9, 1.1, 1.0, 0.8]
```

Bars: worse of the two baseline runs. Line: candidate.
The chart shows the 20 steps with the largest change, out of 22.

- j1: A new reader signs up, logs in, adds a book and puts it on their shelf
- j2: A reader logs in, reviews a book and reads the reviews
- j3: A visitor browses books by tag and opens one
- j4: A reader views their shelf and removes a book

**Issues to open**

none

**Discarded journeys**

none

**Blast radius**

Changed routes:

- `GET /health`: handler (Server).handleHealth itself changed

Affected routes:

- none

*static analysis with go/ast, calls resolved by name, depth 2 from handlers and middleware, a function reached by more than half of all routes is ignored when looking for shared functions; misses dynamic registration, reflection, interface dispatch by other names, and cross-service calls*

**Not measured**

- DB time: not measured; the target exposes no database timing.

**Method.** Baseline run twice (B1, B2) and candidate once (C), in the order B1, C, B2, sequentially on one machine. Three runs give no confidence interval; treat verdicts near the thresholds as uncertain.

Thresholds in effect: a metric is a regression when the candidate is worse than the worse baseline run by more than 2 times the noise band (the difference between B1 and B2), by at least 20%, and by at least 10 ms (latency), 10 MB (memory) or 0.5 s (CPU). A step that passed on both baseline runs and failed on the candidate is a regression. The verdict is withheld when the baseline runs differ by more than 50% of the smaller one and by at least the same minimum, unless the candidate is clearly worse or better than both. A journey with a latency regression and a latency improvement is reported as a trade-off.
