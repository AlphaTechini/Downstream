### Downstream report

Candidate `aaaaaaaa` against baseline `11111111`. Run started 2026-10-22T09:00:00Z, ended 2026-10-22T09:00:07Z. 2 VUs, 15 s per pass, k6 v0.56.0.

**Run stopped at build (head)**

````
go build: examples/shelf/store/store.go:41:2: undefined: shelfTable
```
exit status 1
````

No comparison was made.

**Discarded journeys**

none

**Not measured**

- DB time: not measured; the target exposes no database timing.

**Method.** Baseline run twice (B1, B2) and candidate once (C), in the order B1, C, B2, sequentially on one machine. Three runs give no confidence interval; treat verdicts near the thresholds as uncertain.

Thresholds in effect: a metric is a regression when the candidate is worse than the worse baseline run by more than 2 times the noise band (the difference between B1 and B2), by at least 20%, and by at least 10 ms (latency) or 10 MB (memory). A step that passed on both baseline runs and failed on the candidate is a regression. The verdict is withheld when the baseline runs differ by more than 50% of the smaller one. A journey with a latency regression and a latency improvement is reported as a trade-off.
