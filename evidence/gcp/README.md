# Google Cloud evidence

The Downstream memory service and the NewGit demo app on Cloud Run, project
`downstream-transcend-26`, region `us-central1`, recorded on 2026-10-25.

| File | What it shows |
|---|---|
| [cloud-run-downstream-memory.yaml](cloud-run-downstream-memory.yaml) | The memory service as deployed: at most one instance (L27), `ALLOWED_PROJECTS` (L38), its own service account (L56). Scale to zero is the default (no minimum set) |
| [cloud-run-newgit.yaml](cloud-run-newgit.yaml) | NewGit as deployed: at most one instance (L32), 1 CPU and 512 MiB (L54-L55) |
| [firestore-databases.yaml](firestore-databases.yaml) | The `(default)` Firestore database in us-central1 with `freeTier: true` (L9-L10) |
| [probe.txt](probe.txt) | The live API: health 200, refusal of a project that has not opted in and of a commit that is not in NewGit (HTTP 403), dashboard 200 |
| [newgit-mr-11-notes.json](newgit-mr-11-notes.json) | NewGit !11's run, recorded in the service; its report's Trend has no merged merge requests yet (L151) |
| [newgit-mr-3-notes.json](newgit-mr-3-notes.json) | After !11 merged, the demo merge request !3's report: Trend counts !11, CPU per journey +199.5% compounded and flagged (L179) |
| [cloud-logging-run-recorded.json](cloud-logging-run-recorded.json) | The service's Cloud Logging lines for the records. The 00:07 line is a manual test write, deleted afterwards |

Your email is redacted to `<redacted-email>` where the Cloud Run metadata named the deployer.
