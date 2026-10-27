# Tracing a job in Jaeger

Tags: observability, tracing, jaeger

The queue exports OpenTelemetry traces over OTLP/HTTP when
`OTEL_EXPORTER_OTLP_ENDPOINT` is set (compose sets `http://jaeger:4318`). There is no
sampling configuration: every span is exported. With the variable unset, tracing is off.

## Open Jaeger

- Local compose: http://localhost:16686.
- Services: `forge-api` (HTTP API + scheduler) and `forge-worker` (job execution).

## Find one job

Search service `forge-worker` with the tag `job.id=<id>` (the id returned by
`POST /v1/jobs`). A job's trace contains:

- An API span named after the route, for example `POST /v1/jobs`.
- One worker span per attempt named `job <type>` (for example `job synthgen`) with the
  attributes `job.id`, `job.attempt` and `job.priority`.

The API stores the W3C `traceparent` on the job, and the worker continues that trace, so
attempts that run minutes later still join the original submit trace.

## Reading it

- A long gap between `POST /v1/jobs` and the first `job <type>` span means queue backlog:
  scale workers.
- Several `job <type>` spans with error status and no later span means the job went `dead`.
