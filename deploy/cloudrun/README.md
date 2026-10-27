# Cloud Run deploy (GCP free tier)

The queue API runs as one Cloud Run service. The scheduler and 2 worker goroutines run in the
same container (`INLINE_WORKERS=2`). Redis is a free Upstash database reached over TLS (`rediss://`).
`/v1` routes need `Authorization: Bearer $API_TOKEN` because the URL is public.

Why this shape: a separate always-on worker service would use up the free CPU allowance. A
single instance with CPU kept on between requests stays inside the free tier and scales to zero
when idle.

## One-time setup

1. Create a free Redis database at upstash.com and copy its `rediss://...` URL.
2. Install the gcloud CLI, then run:

```bash
PROJECT=your-project-id
REPO=youssef-110/forge          # GitHub owner/repo
gcloud config set project $PROJECT
gcloud services enable run.googleapis.com artifactregistry.googleapis.com secretmanager.googleapis.com iamcredentials.googleapis.com
gcloud artifacts repositories create forge --repository-format docker --location europe-west1

# secrets
printf '%s' 'rediss://default:...@....upstash.io:6379' | gcloud secrets create forge-redis-url --data-file=-
openssl rand -hex 24 | tr -d '\n' | gcloud secrets create forge-api-token --data-file=-

# deploy identity used by GitHub Actions (no JSON keys)
gcloud iam service-accounts create forge-deployer
SA=forge-deployer@$PROJECT.iam.gserviceaccount.com
for role in run.admin artifactregistry.writer iam.serviceAccountUser secretmanager.secretAccessor; do
  gcloud projects add-iam-policy-binding $PROJECT --member serviceAccount:$SA --role roles/$role
done
# Cloud Run's runtime identity also needs to read the secrets
PROJECT_NUMBER=$(gcloud projects describe $PROJECT --format 'value(projectNumber)')
gcloud projects add-iam-policy-binding $PROJECT \
  --member serviceAccount:$PROJECT_NUMBER-compute@developer.gserviceaccount.com --role roles/secretmanager.secretAccessor

gcloud iam workload-identity-pools create github --location global
gcloud iam workload-identity-pools providers create-oidc github --location global --workload-identity-pool github \
  --issuer-uri https://token.actions.githubusercontent.com \
  --attribute-mapping google.subject=assertion.sub,attribute.repository=assertion.repository \
  --attribute-condition "assertion.repository=='$REPO'"
gcloud iam service-accounts add-iam-policy-binding $SA --role roles/iam.workloadIdentityUser \
  --member "principalSet://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github/attribute.repository/$REPO"
```

3. In the GitHub repo, go to Settings > Secrets and variables > Actions > Variables and add:
   - `GCP_PROJECT` = your project id
   - `GCP_DEPLOY_SA` = `forge-deployer@<project>.iam.gserviceaccount.com`
   - `GCP_WIF_PROVIDER` = `projects/<number>/locations/global/workloadIdentityPools/github/providers/github`
4. Go to Actions > deploy-cloudrun > Run workflow.

## Try it

```bash
URL=https://forge-queue-xxxx.a.run.app
TOKEN=$(gcloud secrets versions access latest --secret forge-api-token)
curl -X POST $URL/v1/jobs -H "Authorization: Bearer $TOKEN" -d '{"type":"synthgen","payload":{"rows":5000,"seed":1}}'
curl $URL/v1/stats -H "Authorization: Bearer $TOKEN"
```

Deploying by hand instead of from Actions:
`docker build -t <image> queue && docker push <image> && bash deploy/cloudrun/deploy.sh <image>`.
