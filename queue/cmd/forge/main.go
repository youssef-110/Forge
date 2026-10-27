// Command forge runs the job queue.
//
//	forge api     HTTP API + leader-elected scheduler (+ INLINE_WORKERS in-process workers)
//	forge worker  workers only, with /metrics on METRICS_PORT
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"

	"github.com/youssef-110/forge/queue/internal/api"
	"github.com/youssef-110/forge/queue/internal/backoff"
	"github.com/youssef-110/forge/queue/internal/scheduler"
	"github.com/youssef-110/forge/queue/internal/store"
	"github.com/youssef-110/forge/queue/internal/telemetry"
	"github.com/youssef-110/forge/queue/internal/worker"
)

func main() {
	mode := "api"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode != "api" && mode != "worker" {
		fmt.Fprintln(os.Stderr, "usage: forge [api|worker]")
		os.Exit(2)
	}
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(mode); err != nil {
		slog.Error("exit", "err", err)
		os.Exit(1)
	}
}

func run(mode string) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	shutdown, err := telemetry.InitTracing(ctx, "forge-"+mode)
	if err != nil {
		return err
	}
	defer shutdown(context.Background())

	opt, err := redis.ParseURL(env("REDIS_URL", "redis://localhost:6379/0"))
	if err != nil {
		return err
	}
	st := store.New(redis.NewClient(opt))
	telemetry.RegisterQueueDepth(st.Stats)

	base, cap := dur("BACKOFF_BASE", time.Second), dur("BACKOFF_CAP", time.Minute)
	delay := func(attempt int) time.Duration { return backoff.FullJitter(attempt, base, cap) }
	wcfg := worker.Config{
		Concurrency: num("WORKER_CONCURRENCY", 4), Visibility: dur("VISIBILITY_TIMEOUT", 30*time.Second),
		Poll: dur("WORKER_POLL", 200*time.Millisecond), Backoff: delay,
	}

	var wg sync.WaitGroup
	start := func(f func(context.Context)) { wg.Add(1); go func() { defer wg.Done(); f(ctx) }() }
	var srv *http.Server

	switch mode {
	case "api":
		host, _ := os.Hostname()
		sched := scheduler.New(st, fmt.Sprintf("%s-%d", host, os.Getpid()), scheduler.Config{
			Tick: dur("SCHEDULER_TICK", 100*time.Millisecond), ReadyTarget: num("READY_TARGET", 8),
			LeaseTTL: 5 * time.Second, Backoff: delay,
		})
		start(sched.Run)
		if n := num("INLINE_WORKERS", 0); n > 0 { // single-container deploys such as Cloud Run
			wcfg.Concurrency = n
			start(worker.New(st, wcfg).Run)
		}
		srv = &http.Server{Addr: ":" + env("PORT", "8080"), Handler: api.New(st, os.Getenv("API_TOKEN")), ReadHeaderTimeout: 5 * time.Second}
	case "worker":
		start(worker.New(st, wcfg).Run)
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", promhttp.Handler())
		mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		srv = &http.Server{Addr: ":" + env("METRICS_PORT", "9090"), Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "mode", mode, "addr", srv.Addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()
	select {
	case err := <-errc:
		stop()
		wg.Wait()
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	wg.Wait()
	return nil
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func num(k string, def int) int {
	if n, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return n
	}
	return def
}

func dur(k string, def time.Duration) time.Duration {
	if d, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return d
	}
	return def
}
