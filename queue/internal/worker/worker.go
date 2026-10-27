// Package worker claims jobs from Redis and runs their handlers.
package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/youssef-110/forge/queue/internal/jobs"
	"github.com/youssef-110/forge/queue/internal/store"
	"github.com/youssef-110/forge/queue/internal/telemetry"
)

type Config struct {
	Concurrency int
	Visibility  time.Duration // a job with no heartbeat for this long is reaped and retried
	Poll        time.Duration // wait between claims when the ready list is empty
	Backoff     func(attempt int) time.Duration
}

type Worker struct {
	st  *store.Store
	cfg Config
}

func New(st *store.Store, cfg Config) *Worker { return &Worker{st: st, cfg: cfg} }

var tracer = otel.Tracer("forge/worker")

func (w *Worker) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for i := 0; i < w.cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w.loop(ctx)
		}()
	}
	wg.Wait()
}

func (w *Worker) loop(ctx context.Context) {
	for ctx.Err() == nil {
		processed, err := w.RunOnce(ctx)
		if err != nil {
			slog.Warn("claim", "err", err)
		}
		if !processed {
			select {
			case <-ctx.Done():
			case <-time.After(w.cfg.Poll):
			}
		}
	}
}

// RunOnce claims and runs at most one job. It reports whether a job was run.
func (w *Worker) RunOnce(ctx context.Context) (bool, error) {
	j, err := w.st.Claim(ctx, w.cfg.Visibility)
	if err != nil || j == nil {
		return false, err
	}
	w.process(ctx, j)
	return true, nil
}

func (w *Worker) process(ctx context.Context, j *store.Job) {
	// Continue the trace started by the API request that submitted the job.
	parent := otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": j.TraceParent})
	jctx, span := tracer.Start(parent, "job "+j.Type)
	defer span.End()
	span.SetAttributes(attribute.String("job.id", j.ID), attribute.Int("job.attempt", j.Attempts),
		attribute.Int("job.priority", j.Priority))
	if j.Attempts == 1 {
		telemetry.QueueWait.WithLabelValues(j.Type).Observe(time.Since(j.CreatedAt).Seconds())
	}

	jctx, cancel := context.WithCancel(jctx)
	defer cancel()
	canceled := w.heartbeat(jctx, j.ID, cancel)

	start := time.Now()
	res, err := run(jctx, j)
	cancel()
	telemetry.JobDuration.WithLabelValues(j.Type).Observe(time.Since(start).Seconds())

	// Store writes must land even when the worker is shutting down.
	bg := context.WithoutCancel(ctx)
	if canceled() {
		telemetry.JobsFinished.WithLabelValues(j.Type, "canceled").Inc()
		span.SetStatus(codes.Error, "canceled")
		return
	}
	if err == nil {
		out, mErr := json.Marshal(res)
		if mErr == nil {
			if cErr := w.st.Complete(bg, j.ID, out); cErr != nil && !errors.Is(cErr, store.ErrConflict) {
				slog.Error("complete", "id", j.ID, "err", cErr)
			}
			telemetry.JobsFinished.WithLabelValues(j.Type, "succeeded").Inc()
			return
		}
		err = mErr
	}
	if ctx.Err() != nil {
		err = fmt.Errorf("worker shutdown: %w", err)
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	state, fErr := w.st.Fail(bg, j.ID, err.Error(), w.cfg.Backoff)
	if fErr != nil {
		slog.Error("fail", "id", j.ID, "err", fErr)
		return
	}
	outcome := "retried"
	if state == store.Dead {
		outcome = "dead"
	}
	telemetry.JobsFinished.WithLabelValues(j.Type, outcome).Inc()
	slog.Info("job failed", "id", j.ID, "attempt", j.Attempts, "state", state, "err", err)
}

// heartbeat keeps the job's visibility deadline fresh and cancels the handler if the
// job was canceled through the API. The returned func reports whether that happened.
func (w *Worker) heartbeat(ctx context.Context, id string, cancel context.CancelFunc) func() bool {
	var mu sync.Mutex
	stopped := false
	go func() {
		t := time.NewTicker(w.cfg.Visibility / 3)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				state, err := w.st.Heartbeat(context.WithoutCancel(ctx), id, w.cfg.Visibility)
				if err == nil && state != store.Running {
					mu.Lock()
					stopped = true
					mu.Unlock()
					cancel()
					return
				}
			}
		}
	}()
	return func() bool { mu.Lock(); defer mu.Unlock(); return stopped }
}

func run(ctx context.Context, j *store.Job) (res any, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	h, ok := jobs.Handlers[j.Type]
	if !ok {
		return nil, fmt.Errorf("unknown job type %q", j.Type)
	}
	return h(ctx, j.Payload)
}
