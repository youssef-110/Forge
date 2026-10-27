// End-to-end tests of API, scheduler and worker against an in-memory Redis.
package internal_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/youssef-110/forge/queue/internal/api"
	"github.com/youssef-110/forge/queue/internal/scheduler"
	"github.com/youssef-110/forge/queue/internal/store"
	"github.com/youssef-110/forge/queue/internal/worker"
)

type env struct {
	st    *store.Store
	srv   *httptest.Server
	sched *scheduler.Scheduler
	wk    *worker.Worker
	mr    *miniredis.Miniredis
}

func setup(t *testing.T, readyTarget int) *env {
	t.Helper()
	mr := miniredis.RunT(t)
	st := store.New(redis.NewClient(&redis.Options{Addr: mr.Addr()}))
	noDelay := func(int) time.Duration { return 0 }
	e := &env{
		st: st, mr: mr,
		srv:   httptest.NewServer(api.New(st, "")),
		sched: scheduler.New(st, "test", scheduler.Config{ReadyTarget: readyTarget, LeaseTTL: time.Second, Backoff: noDelay}),
		wk:    worker.New(st, worker.Config{Concurrency: 1, Visibility: 30 * time.Second, Backoff: noDelay}),
	}
	t.Cleanup(e.srv.Close)
	return e
}

func (e *env) submit(t *testing.T, body string) store.Job {
	t.Helper()
	res, err := http.Post(e.srv.URL+"/v1/jobs", "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("submit %s: status %d", body, res.StatusCode)
	}
	var j store.Job
	_ = json.NewDecoder(res.Body).Decode(&j)
	return j
}

func (e *env) get(t *testing.T, id string) store.Job {
	t.Helper()
	j, err := e.st.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return *j
}

func TestPriorityOrder(t *testing.T) {
	e := setup(t, 1) // ready list of one forces the scheduler to pick strictly by priority
	ctx := context.Background()
	low := e.submit(t, `{"type":"echo","payload":{"message":"low"},"priority":1}`)
	high := e.submit(t, `{"type":"echo","payload":{"message":"high"},"priority":9}`)
	mid := e.submit(t, `{"type":"echo","payload":{"message":"mid"},"priority":5}`)

	var order []string
	for range 3 {
		if err := e.sched.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		j, err := e.st.Claim(ctx, time.Minute)
		if err != nil || j == nil {
			t.Fatalf("claim: %v %v", j, err)
		}
		order = append(order, j.ID)
	}
	want := []string{high.ID, mid.ID, low.ID}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("claim order %v, want %v", order, want)
		}
	}
}

func TestRetryThenDeadLetter(t *testing.T) {
	e := setup(t, 8)
	ctx := context.Background()
	j := e.submit(t, `{"type":"flaky","payload":{"fail_rate":1},"max_attempts":3}`)
	for attempt := 1; attempt <= 3; attempt++ {
		_ = e.sched.Tick(ctx) // promotes the due retry (zero backoff) and dispatches it
		_ = e.sched.Tick(ctx)
		if ran, err := e.wk.RunOnce(ctx); !ran || err != nil {
			t.Fatalf("attempt %d: ran=%v err=%v", attempt, ran, err)
		}
		got := e.get(t, j.ID)
		want := store.Retrying
		if attempt == 3 {
			want = store.Dead
		}
		if got.State != want || got.Attempts != attempt {
			t.Fatalf("attempt %d: state=%s attempts=%d, want %s", attempt, got.State, got.Attempts, want)
		}
	}
	if got := e.get(t, j.ID); got.LastError == "" {
		t.Fatal("dead job should keep last_error")
	}

	// A manual retry puts it back in the queue with a fresh attempt budget.
	res, _ := http.Post(e.srv.URL+"/v1/jobs/"+j.ID+"/retry", "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("retry status %d", res.StatusCode)
	}
	if got := e.get(t, j.ID); got.State != store.Queued || got.Attempts != 0 {
		t.Fatalf("after retry: %+v", got)
	}
}

func TestSuccessStoresResult(t *testing.T) {
	e := setup(t, 8)
	ctx := context.Background()
	j := e.submit(t, `{"type":"synthgen","payload":{"rows":1000,"seed":1}}`)
	_ = e.sched.Tick(ctx)
	if ran, _ := e.wk.RunOnce(ctx); !ran {
		t.Fatal("no job ran")
	}
	got := e.get(t, j.ID)
	if got.State != store.Succeeded || len(got.Result) == 0 {
		t.Fatalf("got %+v", got)
	}
	stats, _ := e.st.Stats(ctx)
	if stats[store.Succeeded] != 1 || stats[store.Queued] != 0 {
		t.Fatalf("stats %v", stats)
	}
}

func TestCanceledJobIsSkipped(t *testing.T) {
	e := setup(t, 8)
	ctx := context.Background()
	j := e.submit(t, `{"type":"echo","payload":{}}`)
	_ = e.sched.Tick(ctx) // now in the Redis ready list
	res, _ := http.Post(e.srv.URL+"/v1/jobs/"+j.ID+"/cancel", "", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("cancel status %d", res.StatusCode)
	}
	if ran, _ := e.wk.RunOnce(ctx); ran {
		t.Fatal("canceled job should not run")
	}
	res, _ = http.Post(e.srv.URL+"/v1/jobs/"+j.ID+"/cancel", "", nil)
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("second cancel status %d, want 409", res.StatusCode)
	}
}

func TestReaperRetriesAbandonedJob(t *testing.T) {
	e := setup(t, 8)
	ctx := context.Background()
	j := e.submit(t, `{"type":"echo","payload":{}}`)
	_ = e.sched.Tick(ctx)
	if _, err := e.st.Claim(ctx, -time.Second); err != nil { // claimed, then the "worker" dies
		t.Fatal(err)
	}
	_ = e.sched.Tick(ctx)
	if got := e.get(t, j.ID); got.State != store.Retrying {
		t.Fatalf("state %s, want retrying", got.State)
	}
}

func TestRebuildAfterLeaderChange(t *testing.T) {
	e := setup(t, 8)
	ctx := context.Background()
	j := e.submit(t, `{"type":"echo","payload":{}}`)
	if _, err := e.st.PopIncoming(ctx, 10); err != nil { // old leader took it, then crashed
		t.Fatal(err)
	}
	fresh := scheduler.New(e.st, "new-leader", scheduler.Config{ReadyTarget: 8, LeaseTTL: time.Second, Backoff: func(int) time.Duration { return 0 }})
	if err := fresh.Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	_ = fresh.Tick(ctx)
	got, _ := e.st.Claim(ctx, time.Minute)
	if got == nil || got.ID != j.ID {
		t.Fatalf("job lost after leader change: %v", got)
	}
}

func TestValidation(t *testing.T) {
	e := setup(t, 8)
	for _, body := range []string{
		`{"type":"nope"}`,
		`{"type":"echo","priority":10}`,
		`{"type":"synthgen","payload":{"rows":0}}`,
		`{"type":"echo","max_attempts":0}`,
		`not json`,
	} {
		res, _ := http.Post(e.srv.URL+"/v1/jobs", "application/json", bytes.NewBufferString(body))
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, res.StatusCode)
		}
	}
	res, _ := http.Get(e.srv.URL + "/v1/jobs/missing")
	if res.StatusCode != http.StatusNotFound {
		t.Errorf("missing job: status %d", res.StatusCode)
	}
}
