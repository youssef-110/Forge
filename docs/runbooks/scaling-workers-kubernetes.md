# Scaling queue workers on Kubernetes

Tags: kubernetes, scaling, workers

The Helm release `forge` in namespace `forge` creates two deployments:

- `forge-api` (default **2** replicas): the HTTP API plus the scheduler. Only the replica
  holding the Redis lease `forge:leader` runs the scheduler.
- `forge-worker` (default **2** replicas): workers that claim and run jobs.

## Manual scale

```
kubectl -n forge get deploy forge-worker
kubectl -n forge scale deployment/forge-worker --replicas=6
kubectl -n forge rollout status deployment/forge-worker
```

Each worker pod runs `WORKER_CONCURRENCY` goroutines (default **4**). Total parallel
jobs = replicas x `WORKER_CONCURRENCY`.

Never scale `forge-api` to 0: with no API replica there is no scheduler, so nothing is
dispatched or reaped (see the 2026-07-21 incident).

## Autoscaling

Autoscaling is optional: set `worker.autoscaling.enabled=true` in the chart (needs
metrics-server). The HPA keeps `forge-worker` between **2** and **10** replicas at
**70%** average CPU. It does not scale on queue depth, so a deep queue of `sleep` jobs
(low CPU) will not trigger a scale-up. Scale manually in that case.

## When to scale up

- `ForgeQueueBacklog`: more than **1000** queued jobs for **10 minutes**
  (`max(forge_jobs{state="queued"}) > 1000`).
- The "Queue wait (submit to first claim)" panel keeps rising.

## Scale down safely

Worker pods get `terminationGracePeriodSeconds: 40`. On SIGTERM a worker stops claiming
and marks its in-flight jobs failed, so they retry on another pod right away instead of
waiting for the 30s visibility timeout.
