# Day-0 platform tests: results

Date: 2026-10-22. Project: GitLab Transcend Showcase (Free tier, role Developer, hosted
runners). Test IDs match [docs/requirements.md](../docs/requirements.md), section 4.

Raw material:

- `day0/run1-probe-v1-job-log.txt`: session 9880588, probe v1.
- `day0/run2-probe-v2-job-log.txt`: session 9881417, probe v2.
- `day0/run3-probe-v3-job-log.txt`: session 9883560, probe v3 (`flows/probe.yml`).
- `day0/probe-notes.txt`: **transcription** of the merge request notes the probe posted
  (owner's copy-paste; not a raw log).

Line numbers refer to the files as committed.

## Results

| ID | Test | Result | Where to look |
|---|---|---|---|
| T-01 | Create and enable a custom flow with a mention trigger | Pass | Three sessions ran from the flow. |
| T-02 | Mention starts a session | Pass | run1 line 65, run2 line 53, run3 line 57 (`Input: ... <message ... author="@AlphaTechini"> @ai-downstream-probe-g...`). The handle format is `ai-<flow>-<topgroup>`. |
| T-03 | Goal contains discussion and IID | Pass | Same lines: the goal begins with the comment `All non-system discussion messages through the triggering message are included.`, then each message with id and author. The IID appears in a `Context:` line at the end of the goal. |
| T-04 | Chain of five or more tool calls with a note after each | Pass in v2; v1 stopped after one action | v1: run1 line 515 (`Tool completed`) then line 531 (`FINISHED`) with no further calls. v2: five steps, notes in `probe-notes.txt`; run2 line 504 and line 529. Cause of the v1 stop is unproven: v2 changed both the inputs (added `context:project_id`) and the prompt. |
| T-05 | Shell, loopback server, 150 s command | Pass | run3 line 545-550 (step C, exit 0, 10 chars), lines 561-586 (step D: `sleep 150`, timeout 200 s, exit 0, 27 chars); `FINISHED` at line 599. Output text in `probe-notes.txt`. |
| T-06 | `setup_script` installs k6; allowlist | Pass | run3 lines 515-520 (step A, exit 0, 198 chars) and 530-535 (step B, exit 0, 148 chars); run3 lines 42-45 (the install commands as executed in the job script, outside the sandbox); run3 line 91 (the sandbox config containing `dl.k6.io`, `release-assets.githubusercontent.com` and the recommended list); step A (k6 version) and step B (HTTP results) in `probe-notes.txt`. |

## Cross-check of the transcribed notes against the job log

The job log records the size of each command's output. The transcription in
`probe-notes.txt` has exactly those sizes, which is evidence it is a faithful copy:

| Step | Log line | Output size in log | Size of transcribed text |
|---|---|---|---|
| A | run3 line 520 | 198 chars | 198 |
| B | run3 line 535 | 148 chars | 148 |
| C | run3 line 550 | 10 chars | 10 |
| D | run3 line 586 | 27 chars | 27 |

The log also shows each command was started with a 200 s timeout (for example run3 line 517).

## Environment facts read from these runs

| Fact | Source |
|---|---|
| Runner `saas-linux-small-amd64`, GitLab Runner 19.6.0 pre-release | run1 lines 1-2 |
| `duo` CLI 9.30.0; `glab` 1.120.0 | run1 lines 79 and 86 |
| Sandbox `srt` 0.0.78 available | run1 line 127; run3 line 118 |
| Image `workflow-generic-image:v0.0.23` | run3 line 39 |
| Default allowlist without config: GitLab hosts only | run1, srt settings (search `allowedDomains`) |
| Allowlist with config: 175 entries | run3 line 91 |
| Sandbox filesystem allows writes to `./` and `/tmp` | run3 line 91 (`allowWrite`) |
| Memory 8145540 kB total, no cgroup limit, 2 CPUs, 18G free in `/tmp` | `probe-notes.txt` |

## Not covered (see requirements.md, T-07 to T-15)

Reading diffs and files, creating issues, issue mentions, committing from the flow, credit
cost, brief rules on visibility and licence, Pages, a narrowed allowlist, `AGENTS.md`
injection. None of these has been tested. Nothing in this repository claims otherwise.

## Redactions

The only alteration to the raw logs is replacing the repository owner's email address with
`<redacted-email>` (run1 line 186, run2 line 173, run3 line 177). The service account's
`noreply.gitlab.com` address remains. No tokens appear in the logs; CI variables are shown
as `${NAME}` references. `scripts/check_evidence.py` flags any other email address.
