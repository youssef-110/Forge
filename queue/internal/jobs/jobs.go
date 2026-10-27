// Package jobs holds the job handlers workers can run and validates payloads at submit time.
package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

type Handler func(ctx context.Context, payload json.RawMessage) (any, error)

var Handlers = map[string]Handler{
	"echo":     run(parseEcho, echo),
	"sleep":    run(parseSleep, sleep),
	"flaky":    run(parseFlaky, flaky),
	"synthgen": run(parseSynthgen, synthgen),
}

var parsers = map[string]func(json.RawMessage) error{
	"echo":     check(parseEcho),
	"sleep":    check(parseSleep),
	"flaky":    check(parseFlaky),
	"synthgen": check(parseSynthgen),
}

// Validate rejects unknown types and bad payloads before a job is queued.
func Validate(typ string, payload json.RawMessage) error {
	p, ok := parsers[typ]
	if !ok {
		return fmt.Errorf("unknown job type %q", typ)
	}
	return p(payload)
}

func run[A any](parse func(json.RawMessage) (A, error), h func(context.Context, A) (any, error)) Handler {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		a, err := parse(raw)
		if err != nil {
			return nil, err
		}
		return h(ctx, a)
	}
}

func check[A any](parse func(json.RawMessage) (A, error)) func(json.RawMessage) error {
	return func(raw json.RawMessage) error { _, err := parse(raw); return err }
}

func decode[A any](raw json.RawMessage) (A, error) {
	var a A
	if len(raw) == 0 {
		raw = json.RawMessage("{}")
	}
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, fmt.Errorf("bad payload: %w", err)
	}
	return a, nil
}

type echoArgs struct {
	Message string `json:"message"`
}

func parseEcho(raw json.RawMessage) (echoArgs, error) { return decode[echoArgs](raw) }

func echo(_ context.Context, a echoArgs) (any, error) {
	return map[string]string{"message": a.Message}, nil
}

type sleepArgs struct {
	Ms int `json:"ms"`
}

func parseSleep(raw json.RawMessage) (sleepArgs, error) {
	a, err := decode[sleepArgs](raw)
	if err == nil && (a.Ms < 0 || a.Ms > 600_000) {
		err = errors.New("ms must be between 0 and 600000")
	}
	return a, err
}

func sleep(ctx context.Context, a sleepArgs) (any, error) {
	select {
	case <-time.After(time.Duration(a.Ms) * time.Millisecond):
		return map[string]int{"slept_ms": a.Ms}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type flakyArgs struct {
	FailRate float64 `json:"fail_rate"`
}

func parseFlaky(raw json.RawMessage) (flakyArgs, error) {
	a, err := decode[flakyArgs](raw)
	if err == nil && (a.FailRate < 0 || a.FailRate > 1) {
		err = errors.New("fail_rate must be between 0 and 1")
	}
	return a, err
}

func flaky(_ context.Context, a flakyArgs) (any, error) {
	if rand.Float64() < a.FailRate {
		return nil, errors.New("flaky: simulated failure")
	}
	return map[string]bool{"ok": true}, nil
}

type synthArgs struct {
	Rows int    `json:"rows"`
	Seed uint64 `json:"seed"`
}

func parseSynthgen(raw json.RawMessage) (synthArgs, error) {
	a, err := decode[synthArgs](raw)
	if err == nil && (a.Rows < 1 || a.Rows > 100_000) {
		err = errors.New("rows must be between 1 and 100000")
	}
	return a, err
}

// synthgen generates log-normal "latency" samples (a stand-in for a synthetic-data
// job) and summarizes them. Same seed, same output.
func synthgen(ctx context.Context, a synthArgs) (any, error) {
	r := rand.New(rand.NewPCG(a.Seed, a.Seed^0x9e3779b97f4a7c15))
	xs := make([]float64, a.Rows)
	var sum float64
	for i := range xs {
		if i%10_000 == 0 && ctx.Err() != nil {
			return nil, ctx.Err()
		}
		xs[i] = math.Exp(3 + 0.5*r.NormFloat64())
		sum += xs[i]
	}
	mean := sum / float64(len(xs))
	var sq float64
	for _, x := range xs {
		sq += (x - mean) * (x - mean)
	}
	p95 := Select(xs, int(math.Ceil(0.95*float64(len(xs))))-1)
	return map[string]any{
		"rows": a.Rows, "mean": round(mean), "stddev": round(math.Sqrt(sq / float64(len(xs)))), "p95": round(p95),
	}, nil
}

func round(x float64) float64 { return math.Round(x*1000) / 1000 }
