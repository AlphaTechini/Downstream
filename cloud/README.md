# Google Cloud deployment

Downstream measures merge requests inside GitLab's Duo sandbox, so its core needs no server.
What a sandbox cannot do is remember: every run starts empty, so Downstream could only judge a
merge request against its own target branch. Ten merge requests that each add 8% CPU would all
pass. The **Downstream memory service** on Google Cloud Run keeps a numbers-only record of each
run of an opted-in project, and the flow compounds the records of merged merge requests into a
Trend section on every report.

| | |
|---|---|
| Memory service and dashboard | https://downstream-memory-1083478229816.us-central1.run.app |
| NewGit demo app | https://newgit-1083478229816.us-central1.run.app |
| GCP project, region | `downstream-transcend-26`, `us-central1` |

## Files

- To find the build of the memory service image visit [memory/Dockerfile](memory/Dockerfile)
  (distroless, non-root, standard library only) and [memory/cloudbuild.yaml](memory/cloudbuild.yaml).
- To find the whole deployment (APIs, Firestore database, service account, build, Cloud Run
  services) visit [deploy.sh](deploy.sh).
- The service's code can be found in [`engine/memory/`](../engine/memory/) (API, Firestore
  store, dashboard) and [`engine/cmd/downstream-memory/main.go`](../engine/cmd/downstream-memory/main.go).
- The record and the trend can be found in [`engine/history/history.go`](../engine/history/history.go).
- The flow step that calls the service is R8H in [`flows/downstream.yml`](../flows/downstream.yml).

```sh
PROJECT=my-gcp-project sh cloud/deploy.sh                       # memory service
PROJECT=my-gcp-project NEWGIT_SRC=../newgit sh cloud/deploy.sh  # and the NewGit demo app
```

## Decisions and trade-offs

- **Changes, not absolute figures.** The same branch measured 5.2 ms and 10.4 ms of CPU per
  journey in two sandbox sessions, so absolute figures cannot be compared across runs. A
  record keeps each run's own controlled change (candidate against the worse of two
  baselines in one session), and the trend compounds those.
- **Merged means "the recorded head is an ancestor of the current base".** The engine checks
  this with git in the sandbox, so the service needs no GitLab token. Squash merges create new
  commits and are not traced.
- **Privacy.** A record holds the project path, merge request number, commit SHAs and the
  per-metric numbers: no code, no diffs, no route names. Projects opt in with `history: on` in
  `downstream.yml` (read from the base commit), and this public instance also only accepts the
  projects in its `ALLOWED_PROJECTS` list. Each write must name a real commit of that project,
  checked against GitLab's public API. Records are deleted after 30 days.
- **No SDK, no keys.** Firestore is called over REST with the Cloud Run service account's token
  from the metadata server. The service runs as its own account with `roles/datastore.user` only.
- **Free tier.** Both services scale to zero with at most one instance. Firestore uses the
  `(default)` database, the one with free quota. Old records are pruned with ordinary deletes,
  because Firestore TTL deletes are not covered by the free tier. Artifact Registry keeps the
  two newest images. A 1 USD budget alert is set on the project.
- **The flow never depends on it.** If the service cannot be reached, the report says the trend
  is unavailable and everything else is unchanged.
