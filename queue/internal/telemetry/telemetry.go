// Package telemetry wires OpenTelemetry tracing and the Prometheus metrics the dashboard reads.
package telemetry

import (
	"context"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// InitTracing exports spans over OTLP/HTTP when OTEL_EXPORTER_OTLP_ENDPOINT is set,
// and is a no-op otherwise. The propagator is always set so trace context still flows.
func InitTracing(ctx context.Context, service string) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))),
	)
	otel.SetTracerProvider(tp)
	return tp.Shutdown, nil
}

var durationBuckets = []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60}

var (
	JobsEnqueued = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "forge_jobs_enqueued_total", Help: "Jobs accepted by the API.",
	}, []string{"type"})
	JobsFinished = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "forge_jobs_finished_total", Help: "Job attempts by outcome: succeeded, retried, dead, canceled.",
	}, []string{"type", "outcome"})
	JobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "forge_job_duration_seconds", Help: "Handler run time per attempt.", Buckets: durationBuckets,
	}, []string{"type"})
	QueueWait = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "forge_job_queue_wait_seconds", Help: "Time from submit to first claim.", Buckets: durationBuckets,
	}, []string{"type"})
	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name: "forge_http_request_duration_seconds", Help: "API latency by route and status.",
	}, []string{"route", "code"})
	HeapSize = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "forge_scheduler_heap_size", Help: "Items in the scheduler's in-memory heaps.",
	}, []string{"heap"})
	IsLeader = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "forge_scheduler_leader", Help: "1 if this replica holds the scheduler lease.",
	})
	Reaped = promauto.NewCounter(prometheus.CounterOpts{
		Name: "forge_jobs_reaped_total", Help: "Running jobs whose worker stopped heartbeating.",
	})
)

// RegisterQueueDepth exposes job counts per state, read from Redis at scrape time so
// every replica reports the same, current numbers.
func RegisterQueueDepth(stats func(context.Context) (map[string]int64, error)) {
	prometheus.MustRegister(depthCollector{stats})
}

var depthDesc = prometheus.NewDesc("forge_jobs", "Jobs currently in each state.", []string{"state"}, nil)

type depthCollector struct {
	stats func(context.Context) (map[string]int64, error)
}

func (depthCollector) Describe(ch chan<- *prometheus.Desc) { ch <- depthDesc }

func (d depthCollector) Collect(ch chan<- prometheus.Metric) {
	m, err := d.stats(context.Background())
	if err != nil {
		ch <- prometheus.NewInvalidMetric(depthDesc, err)
		return
	}
	for state, n := range m {
		ch <- prometheus.MustNewConstMetric(depthDesc, prometheus.GaugeValue, float64(n), state)
	}
}
