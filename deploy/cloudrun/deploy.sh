#!/usr/bin/env bash
# Deploy an already-pushed queue image to Cloud Run. Usage: deploy.sh <image>
set -euo pipefail
image=${1:?usage: deploy.sh <image>}

# --no-cpu-throttling keeps CPU on between requests so the scheduler and inline workers
# keep running; --max-instances 1 keeps it inside the free tier and gives one scheduler.
# Instances still scale to zero when idle, which pauses background work until the next request.
gcloud run deploy "${SERVICE:-forge-queue}" \
  --image "$image" \
  --region "${REGION:-europe-west1}" \
  --args api \
  --port 8080 \
  --cpu 1 --memory 256Mi \
  --min-instances 0 --max-instances 1 \
  --no-cpu-throttling \
  --set-env-vars INLINE_WORKERS=2 \
  --set-secrets REDIS_URL=forge-redis-url:latest,API_TOKEN=forge-api-token:latest \
  --allow-unauthenticated \
  --quiet

url=$(gcloud run services describe "${SERVICE:-forge-queue}" --region "${REGION:-europe-west1}" --format 'value(status.url)')
echo "deployed: $url"
curl -fs "$url/healthz" >/dev/null && echo "healthz ok"
