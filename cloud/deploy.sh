#!/bin/sh
# Deploys Downstream's Google Cloud part: the memory service (Cloud Run +
# Firestore) and, optionally, the NewGit demo app. Both scale to zero.
#
#   PROJECT=my-gcp-project sh cloud/deploy.sh             # memory service
#   PROJECT=my-gcp-project NEWGIT_SRC=../newgit sh cloud/deploy.sh   # and NewGit
#
# Needs gcloud, signed in, with billing enabled on PROJECT. Everything fits the
# free tier at hackathon load; a Firestore (default) database gets the free quota.
set -eu

: "${PROJECT:?set PROJECT to the GCP project ID}"
REGION=${REGION:-us-central1}
ALLOWED=${ALLOWED_PROJECTS:-gitlab-ai-hackathon/transcend-october-2026/35141659/newgit,gitlab-ai-hackathon/transcend-october-2026/35141659/showcase}
SA=downstream-memory@$PROJECT.iam.gserviceaccount.com
IMAGE=$REGION-docker.pkg.dev/$PROJECT/cloud-run-source-deploy/downstream-memory:latest
cd "$(dirname "$0")/.."

gcloud services enable run.googleapis.com cloudbuild.googleapis.com artifactregistry.googleapis.com \
  firestore.googleapis.com --project "$PROJECT"

# Firestore (default) database: the one database per project with free quota.
gcloud firestore databases describe --project "$PROJECT" >/dev/null 2>&1 ||
  gcloud firestore databases create --location="$REGION" --type=firestore-native --project "$PROJECT"

gcloud artifacts repositories describe cloud-run-source-deploy --location "$REGION" --project "$PROJECT" >/dev/null 2>&1 ||
  gcloud artifacts repositories create cloud-run-source-deploy --repository-format=docker --location "$REGION" --project "$PROJECT"

# The service runs as its own account with Firestore access only.
gcloud iam service-accounts describe "$SA" --project "$PROJECT" >/dev/null 2>&1 ||
  gcloud iam service-accounts create downstream-memory --display-name="Downstream memory" --project "$PROJECT"
gcloud projects add-iam-policy-binding "$PROJECT" --member="serviceAccount:$SA" \
  --role=roles/datastore.user --condition=None --quiet >/dev/null

gcloud builds submit --project "$PROJECT" --config cloud/memory/cloudbuild.yaml \
  --substitutions=_IMAGE="$IMAGE" .

# Scale to zero, one instance at most: requests start an instance, and an idle
# service costs nothing. The ^@^ prefix lets ALLOWED_PROJECTS hold commas.
gcloud run deploy downstream-memory --project "$PROJECT" --region "$REGION" --image "$IMAGE" \
  --service-account "$SA" --allow-unauthenticated \
  --min-instances 0 --max-instances 1 --cpu 1 --memory 256Mi --concurrency 20 --timeout 30 \
  --set-env-vars "^@^ALLOWED_PROJECTS=$ALLOWED@RETENTION_DAYS=30" --quiet

if [ -n "${NEWGIT_SRC:-}" ]; then
  gcloud run deploy newgit --project "$PROJECT" --region "$REGION" --source "$NEWGIT_SRC" \
    --allow-unauthenticated --min-instances 0 --max-instances 1 --cpu 1 --memory 512Mi \
    --concurrency 40 --timeout 60 --args=serve,-seed-demo --quiet
  URL=$(gcloud run services describe newgit --project "$PROJECT" --region "$REGION" --format='value(status.url)')
  gcloud run services update newgit --project "$PROJECT" --region "$REGION" \
    --update-env-vars "NEWGIT_BASE_URL=$URL" --quiet
fi

gcloud run services describe downstream-memory --project "$PROJECT" --region "$REGION" --format='value(status.url)'
