### Downstream report

Candidate `105e7945` against baseline `baacc58f`. Run started 2026-10-23T03:50:37Z, ended 2026-10-23T03:50:41Z. 2 VUs, 10 iterations per VU per pass (cap 60 s), k6 v0.56.0 (commit/50afb99947, go1.23.4, linux/amd64).

**Findings:** regressed steps 0, trade-off journeys 0, improved metrics 0, withheld metrics 0.

#### A new reader signs up, logs in, adds a book and puts it on their shelf

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `sign_up` | 1.9 / 4.1 ms | 2.7 ms | -1.4 ms (-34%) | 2.2 ms | noise |
| `log_in` | 2.1 / 2.1 ms | 2.5 ms | +0.4 ms (+18%) | 0.1 ms | noise |
| `add_book` | 2.6 / 3.3 ms | 2.2 ms | -1.2 ms (-35%) | 0.7 ms | noise |
| `shelf_add` | 3.6 / 3.2 ms | 8.1 ms | +4.6 ms (+129%) | 0.3 ms | noise |
| `shelf_list` | 2.6 / 1.6 ms | 6.2 ms | +3.7 ms (+143%) | 0.9 ms | noise |

#### A reader logs in, reviews a book and reads the reviews

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `sign_up` | 1.5 / 2.0 ms | 1.6 ms | -0.3 ms (-18%) | 0.4 ms | noise |
| `log_in` | 1.6 / 4.7 ms | 2.0 ms | -2.8 ms (-58%) | 3.2 ms | noise |
| `add_book` | 1.8 / 1.7 ms | 1.1 ms | -0.7 ms (-39%) | 0.1 ms | noise |
| `post_review` | 3.9 / 1.9 ms | 1.3 ms | -2.6 ms (-68%) | 2.0 ms | noise |
| `read_reviews` | 3.4 / 1.8 ms | 2.4 ms | -0.9 ms (-27%) | 1.5 ms | noise |

#### A visitor browses books by tag and opens one

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `sign_up` | 4.9 / 1.8 ms | 1.3 ms | -3.6 ms (-74%) | 3.1 ms | noise |
| `log_in` | 4.8 / 3.3 ms | 7.0 ms | +2.2 ms (+45%) | 1.6 ms | noise |
| `add_book` | 2.1 / 3.1 ms | 1.0 ms | -2.1 ms (-68%) | 1.0 ms | noise |
| `browse_tag` | 2.6 / 1.6 ms | 1.3 ms | -1.3 ms (-49%) | 1.0 ms | noise |
| `open_book` | 1.8 / 2.6 ms | 0.7 ms | -1.8 ms (-71%) | 0.8 ms | noise |

#### A reader views their shelf and removes a book

| Step | Baseline p95 (B1 / B2) | Candidate p95 | Change | Noise band | Verdict |
|---|---|---|---|---|---|
| `sign_up` | 5.8 / 2.1 ms | 3.2 ms | -2.7 ms (-46%) | 3.7 ms | noise |
| `log_in` | 5.9 / 2.9 ms | 2.8 ms | -3.1 ms (-52%) | 3.0 ms | noise |
| `add_book` | 1.5 / 3.7 ms | 4.6 ms | +0.9 ms (+25%) | 2.2 ms | noise |
| `shelf_add` | 4.7 / 2.0 ms | 1.3 ms | -3.4 ms (-72%) | 2.7 ms | noise |
| `shelf_list` | 2.0 / 3.8 ms | 1.4 ms | -2.5 ms (-65%) | 1.8 ms | noise |
| `shelf_remove` | 0.7 / 1.1 ms | 1.2 ms | +0.2 ms (+16%) | 0.4 ms | noise |
| `shelf_list_again` | 0.9 / 1.4 ms | 0.5 ms | -0.9 ms (-67%) | 0.5 ms | noise |

**App process**

| Metric | B1 | C | B2 | Change | Noise band | Verdict |
|---|---|---|---|---|---|---|
| CPU time | 0.17 s | 0.18 s | 0.17 s | +0.01 s (+6%) | 0.00 s | noise |
| Peak RSS | 8.8 MB | 8.4 MB | 8.7 MB | -0.4 MB (-4%) | 0.1 MB | noise |

**p95 per step**

```mermaid
xychart-beta
    title "p95 per step: candidate against the worse baseline run"
    x-axis ["j1.sign_up", "j1.log_in", "j1.add_book", "j1.shelf_add", "j1.shelf_list", "j2.sign_up", "j2.log_in", "j2.add_book", "j2.post_review", "j2.read_reviews", "j3.log_in", "j3.add_book", "j3.browse_tag", "j3.open_book", "j4.sign_up", "j4.log_in", "j4.add_book", "j4.shelf_list", "j4.shelf_remove", "j4.shelf_list_again"]
    y-axis "p95 (ms)"
    bar [4.1, 2.1, 3.3, 3.6, 2.6, 2.0, 4.7, 1.8, 3.9, 3.4, 4.8, 3.1, 2.6, 2.6, 5.8, 5.9, 3.7, 3.8, 1.1, 1.4]
    line [2.7, 2.5, 2.2, 8.1, 6.2, 1.6, 2.0, 1.1, 1.3, 2.4, 7.0, 1.0, 1.3, 0.7, 3.2, 2.8, 4.6, 1.4, 1.2, 0.5]
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
