#!/usr/bin/env bash
# Local Kubernetes: 3-node kind cluster, Forge image loaded, chart installed, smoke test.
# Needs: docker, kind, helm, kubectl, curl. Set SKIP_BUILD=1 when the image is already loaded.
set -euo pipefail
cd "$(dirname "$0")/../.."

kind get clusters | grep -qx forge || kind create cluster --config deploy/kind/kind-config.yaml
if [ -z "${SKIP_BUILD:-}" ]; then
  docker build -t forge-queue:dev queue
  kind load docker-image forge-queue:dev --name forge
fi
helm upgrade --install forge deploy/helm/forge --namespace forge --create-namespace \
  --set image.repository=forge-queue --set image.tag=dev --set image.pullPolicy=Never \
  --wait --timeout 3m
kubectl -n forge get pods -o wide

kubectl -n forge port-forward svc/forge-api 8080:80 >/dev/null 2>&1 &
pf=$!
trap 'kill $pf' EXIT
sleep 3

body='{"type":"synthgen","payload":{"rows":5000,"seed":7}}'
id=$(curl -fs -X POST localhost:8080/v1/jobs -d "$body" | sed -E 's/.*"id":"([^"]+)".*/\1/')
state=unknown
for _ in $(seq 1 30); do
  state=$(curl -fs "localhost:8080/v1/jobs/$id" | sed -E 's/.*"state":"([^"]+)".*/\1/')
  if [ "$state" = succeeded ]; then
    echo "smoke test passed: job $id succeeded"
    exit 0
  fi
  sleep 1
done
echo "smoke test failed: job $id is $state" >&2
exit 1
