// Package scheduler decides what runs next. One replica holds a Redis lease and keeps two
// in-memory heaps: a ready queue (priority, then FIFO) and a delay queue of retries ordered
// by due time. It keeps the shared Redis ready list short, so priority is honoured at
// dispatch time instead of being lost in a long FIFO list.
package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/youssef-110/forge/queue/internal/pq"
	"github.com/youssef-110/forge/queue/internal/store"
	"github.com/youssef-110/forge/queue/internal/telemetry"
)

type Config struct {
	Tick        time.Duration // how often the leader dispatches
	ReadyTarget int           // jobs kept in the shared ready list for workers to claim
	LeaseTTL    time.Duration
	Backoff     func(attempt int) time.Duration
}

type Scheduler struct {
	st      *store.Store
	id      string
	cfg     Config
	ready   *pq.Heap[pq.Item]
	delay   *pq.Heap[pq.Item]
	tracked map[string]bool // IDs currently in a heap; dedupes repeated inbox entries
	leader  bool
}

func New(st *store.Store, id string, cfg Config) *Scheduler {
	return &Scheduler{st: st, id: id, cfg: cfg, ready: pq.NewReady(), delay: pq.NewDelay(), tracked: map[string]bool{}}
}

func (s *Scheduler) Run(ctx context.Context) {
	t := time.NewTicker(s.cfg.Tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		ok, err := s.st.AcquireLeader(ctx, s.id, s.cfg.LeaseTTL)
		if err != nil {
			slog.Warn("scheduler lease", "err", err)
			continue
		}
		if ok && !s.leader {
			slog.Info("scheduler became leader", "id", s.id)
			if err := s.Rebuild(ctx); err != nil {
				slog.Error("scheduler rebuild", "err", err)
				continue
			}
		}
		s.leader = ok
		if !ok {
			telemetry.IsLeader.Set(0)
			continue
		}
		telemetry.IsLeader.Set(1)
		if err := s.Tick(ctx); err != nil {
			slog.Warn("scheduler tick", "err", err)
		}
	}
}

// Rebuild reloads the heaps from Redis after this replica takes the lease. Redis is the
// source of truth, so a crashed leader loses nothing. A brief overlap of two leaders can
// push an ID to the ready list twice; the claim script only runs queued jobs, so that is safe.
func (s *Scheduler) Rebuild(ctx context.Context) error {
	s.ready.Clear()
	s.delay.Clear()
	s.tracked = map[string]bool{}
	inReady, err := s.st.ReadyIDs(ctx)
	if err != nil {
		return err
	}
	skip := map[string]bool{}
	for _, id := range inReady {
		skip[id] = true
	}
	for _, state := range []string{store.Queued, store.Retrying} {
		ids, err := s.st.IDsInState(ctx, state)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if !skip[id] {
				if err := s.track(ctx, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// Tick runs one scheduling round: take new jobs, promote due retries, top up the ready
// list in priority order, and reap jobs whose worker died.
func (s *Scheduler) Tick(ctx context.Context) error {
	ids, err := s.st.PopIncoming(ctx, 500)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err := s.track(ctx, id); err != nil {
			return err
		}
	}

	now := time.Now()
	for {
		it, ok := s.delay.Peek()
		if !ok || it.RunAt.After(now) {
			break
		}
		s.delay.Pop()
		switch err := s.st.Promote(ctx, it.ID); {
		case err == nil:
			s.ready.Push(it)
		case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNotFound):
			delete(s.tracked, it.ID) // canceled while waiting
		default:
			s.delay.Push(it)
			return err
		}
	}

	n, err := s.st.ReadyLen(ctx)
	if err != nil {
		return err
	}
	var batch []string
	for int(n)+len(batch) < s.cfg.ReadyTarget {
		it, ok := s.ready.Pop()
		if !ok {
			break
		}
		delete(s.tracked, it.ID)
		batch = append(batch, it.ID)
	}
	if len(batch) > 0 {
		if err := s.st.PushReady(ctx, batch...); err != nil {
			return err
		}
	}

	if err := s.reap(ctx, now); err != nil {
		return err
	}
	telemetry.HeapSize.WithLabelValues("ready").Set(float64(s.ready.Len()))
	telemetry.HeapSize.WithLabelValues("delay").Set(float64(s.delay.Len()))
	return nil
}

func (s *Scheduler) track(ctx context.Context, id string) error {
	if s.tracked[id] {
		return nil
	}
	j, err := s.st.Get(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	it := pq.Item{ID: j.ID, Priority: j.Priority, Seq: j.CreatedAt.UnixMilli()}
	switch j.State {
	case store.Queued:
		s.ready.Push(it)
	case store.Retrying:
		if j.NextRunAt != nil {
			it.RunAt = *j.NextRunAt
		}
		s.delay.Push(it)
	default:
		return nil
	}
	s.tracked[id] = true
	return nil
}

func (s *Scheduler) reap(ctx context.Context, now time.Time) error {
	ids, err := s.st.Expired(ctx, now)
	if err != nil {
		return err
	}
	for _, id := range ids {
		state, err := s.st.Fail(ctx, id, "visibility timeout: worker stopped heartbeating", s.cfg.Backoff)
		switch {
		case err == nil:
			telemetry.Reaped.Inc()
			slog.Warn("reaped job", "id", id, "state", state)
		case errors.Is(err, store.ErrConflict), errors.Is(err, store.ErrNotFound):
			_ = s.st.DropRunning(ctx, id)
		default:
			return err
		}
	}
	return nil
}
