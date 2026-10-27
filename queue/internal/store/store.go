// Package store keeps jobs in Redis. Every state change goes through one Lua script,
// so a job is always in exactly one state index, even with many API and worker replicas.
package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

// Scripts touch keys not passed in KEYS, which is fine on a single Redis node.
// shortcut: single-node only, hash-tag the keys before moving to Redis Cluster.
const prefix = "forge:"

const (
	Queued    = "queued"
	Running   = "running"
	Retrying  = "retrying"
	Succeeded = "succeeded"
	Dead      = "dead"
	Canceled  = "canceled"
)

var States = []string{Queued, Running, Retrying, Succeeded, Dead, Canceled}

var (
	ErrNotFound = errors.New("job not found")
	ErrConflict = errors.New("job state does not allow this")
)

type Job struct {
	ID          string          `json:"id"`
	Type        string          `json:"type"`
	Payload     json.RawMessage `json:"payload"`
	Priority    int             `json:"priority"`
	State       string          `json:"state"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	LastError   string          `json:"last_error"`
	Result      json.RawMessage `json:"result"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	NextRunAt   *time.Time      `json:"next_run_at,omitempty"`
	TraceParent string          `json:"-"` // W3C trace context carried from the API to the worker
}

// NewID returns a 25-char ID that sorts by creation time.
func NewID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%013x%s", time.Now().UnixMilli(), hex.EncodeToString(b))
}

type Store struct{ rdb *redis.Client }

func New(rdb *redis.Client) *Store { return &Store{rdb: rdb} }

func jobKey(id string) string { return prefix + "job:" + id }

func ms(t time.Time) int64 { return t.UnixMilli() }

func (s *Store) Ping(ctx context.Context) error { return s.rdb.Ping(ctx).Err() }

func (s *Store) Create(ctx context.Context, j *Job) error {
	now := time.Now()
	j.ID, j.State, j.CreatedAt, j.UpdatedAt = NewID(), Queued, now.UTC(), now.UTC()
	if len(j.Payload) == 0 {
		j.Payload = json.RawMessage("{}")
	}
	score := float64(ms(now))
	_, err := s.rdb.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.HSet(ctx, jobKey(j.ID), map[string]any{
			"id": j.ID, "type": j.Type, "payload": string(j.Payload), "priority": j.Priority,
			"state": j.State, "attempts": 0, "max_attempts": j.MaxAttempts, "last_error": "",
			"result": "", "created_at": ms(now), "updated_at": ms(now), "next_run_at": "",
			"traceparent": j.TraceParent,
		})
		p.ZAdd(ctx, prefix+"jobs", redis.Z{Score: score, Member: j.ID})
		p.ZAdd(ctx, prefix+"state:"+Queued, redis.Z{Score: score, Member: j.ID})
		p.RPush(ctx, prefix+"incoming", j.ID)
		return nil
	})
	return err
}

func (s *Store) Get(ctx context.Context, id string) (*Job, error) {
	m, err := s.rdb.HGetAll(ctx, jobKey(id)).Result()
	if err != nil {
		return nil, err
	}
	if len(m) == 0 {
		return nil, ErrNotFound
	}
	return decode(m), nil
}

func decode(m map[string]string) *Job {
	atoi := func(k string) int { n, _ := strconv.Atoi(m[k]); return n }
	tm := func(k string) time.Time { n, _ := strconv.ParseInt(m[k], 10, 64); return time.UnixMilli(n).UTC() }
	j := &Job{
		ID: m["id"], Type: m["type"], Payload: json.RawMessage(m["payload"]), Priority: atoi("priority"),
		State: m["state"], Attempts: atoi("attempts"), MaxAttempts: atoi("max_attempts"),
		LastError: m["last_error"], CreatedAt: tm("created_at"), UpdatedAt: tm("updated_at"),
		TraceParent: m["traceparent"],
	}
	if m["result"] != "" {
		j.Result = json.RawMessage(m["result"])
	}
	if m["next_run_at"] != "" {
		t := tm("next_run_at")
		j.NextRunAt = &t
	}
	return j
}

// List returns jobs newest first, optionally only those in one state.
func (s *Store) List(ctx context.Context, state string, limit int) ([]*Job, error) {
	key := prefix + "jobs"
	if state != "" {
		key = prefix + "state:" + state
	}
	ids, err := s.rdb.ZRevRange(ctx, key, 0, int64(limit-1)).Result()
	if err != nil || len(ids) == 0 {
		return []*Job{}, err
	}
	cmds, err := s.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for _, id := range ids {
			p.HGetAll(ctx, jobKey(id))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	jobs := make([]*Job, 0, len(cmds))
	for _, c := range cmds {
		if m := c.(*redis.MapStringStringCmd).Val(); len(m) > 0 {
			jobs = append(jobs, decode(m))
		}
	}
	return jobs, nil
}

func (s *Store) Stats(ctx context.Context) (map[string]int64, error) {
	cmds, err := s.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		for _, st := range States {
			p.ZCard(ctx, prefix+"state:"+st)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(States))
	for i, st := range States {
		out[st] = cmds[i].(*redis.IntCmd).Val()
	}
	return out, nil
}

// transition moves a job between states atomically if its current state is in the
// allowed set. Returns 1 ok, 0 wrong state, -1 missing.
var transition = redis.NewScript(`
local cur = redis.call('HGET', KEYS[1], 'state')
if not cur then return -1 end
if not string.find(ARGV[2], ',' .. cur .. ',', 1, true) then return 0 end
local p, id = KEYS[2], ARGV[1]
local created = redis.call('HGET', KEYS[1], 'created_at')
redis.call('ZREM', p .. 'state:' .. cur, id)
redis.call('ZADD', p .. 'state:' .. ARGV[3], created, id)
redis.call('HSET', KEYS[1], 'state', ARGV[3], 'updated_at', ARGV[4])
for i = 6, #ARGV, 2 do redis.call('HSET', KEYS[1], ARGV[i], ARGV[i + 1]) end
if cur == 'running' then redis.call('ZREM', p .. 'running', id) end
if ARGV[5] == '1' then redis.call('RPUSH', p .. 'incoming', id) end
return 1
`)

// Transition changes state. requeue pushes the job back to the scheduler's inbox.
func (s *Store) Transition(ctx context.Context, id string, from []string, to string, requeue bool, fields map[string]any) error {
	push := "0"
	if requeue {
		push = "1"
	}
	args := []any{id, "," + strings.Join(from, ",") + ",", to, ms(time.Now()), push}
	for k, v := range fields {
		args = append(args, k, v)
	}
	n, err := transition.Run(ctx, s.rdb, []string{jobKey(id), prefix}, args...).Int()
	if err != nil {
		return err
	}
	switch n {
	case -1:
		return ErrNotFound
	case 0:
		return ErrConflict
	}
	return nil
}

func (s *Store) Complete(ctx context.Context, id string, result []byte) error {
	return s.Transition(ctx, id, []string{Running}, Succeeded, false, map[string]any{"result": string(result)})
}

// Fail records a failed attempt: retry later with backoff, or dead-letter once attempts run out.
func (s *Store) Fail(ctx context.Context, id, msg string, delay func(attempt int) time.Duration) (string, error) {
	j, err := s.Get(ctx, id)
	if err != nil {
		return "", err
	}
	if j.State != Running {
		return j.State, ErrConflict
	}
	if j.Attempts >= j.MaxAttempts {
		return Dead, s.Transition(ctx, id, []string{Running}, Dead, false, map[string]any{"last_error": msg})
	}
	at := time.Now().Add(delay(j.Attempts))
	return Retrying, s.Transition(ctx, id, []string{Running}, Retrying, true,
		map[string]any{"last_error": msg, "next_run_at": ms(at)})
}

func (s *Store) Cancel(ctx context.Context, id string) error {
	return s.Transition(ctx, id, []string{Queued, Retrying, Running}, Canceled, false, nil)
}

// Retry re-runs a dead or canceled job from scratch.
func (s *Store) Retry(ctx context.Context, id string) error {
	return s.Transition(ctx, id, []string{Dead, Canceled}, Queued, true,
		map[string]any{"attempts": 0, "next_run_at": ""})
}

// Promote moves a due retry back to queued. Called by the scheduler.
func (s *Store) Promote(ctx context.Context, id string) error {
	return s.Transition(ctx, id, []string{Retrying}, Queued, false, map[string]any{"next_run_at": ""})
}

// PopIncoming drains up to n IDs the scheduler has not seen yet.
func (s *Store) PopIncoming(ctx context.Context, n int) ([]string, error) {
	ids, err := s.rdb.LPopCount(ctx, prefix+"incoming", n).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	return ids, err
}

func (s *Store) ReadyLen(ctx context.Context) (int64, error) {
	return s.rdb.LLen(ctx, prefix+"ready").Result()
}

func (s *Store) ReadyIDs(ctx context.Context) ([]string, error) {
	return s.rdb.LRange(ctx, prefix+"ready", 0, -1).Result()
}

func (s *Store) PushReady(ctx context.Context, ids ...string) error {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return s.rdb.RPush(ctx, prefix+"ready", args...).Err()
}

func (s *Store) IDsInState(ctx context.Context, state string) ([]string, error) {
	return s.rdb.ZRange(ctx, prefix+"state:"+state, 0, -1).Result()
}

// claim pops ready IDs until it finds one still queued (canceled jobs are skipped
// lazily here instead of being removed from the heap), then marks it running with a
// visibility deadline. Returns nil when nothing is ready.
var claim = redis.NewScript(`
local p = ARGV[1]
while true do
  local id = redis.call('LPOP', p .. 'ready')
  if not id then return false end
  local k = p .. 'job:' .. id
  if redis.call('HGET', k, 'state') == 'queued' then
    local created = redis.call('HGET', k, 'created_at')
    redis.call('ZREM', p .. 'state:queued', id)
    redis.call('ZADD', p .. 'state:running', created, id)
    redis.call('HSET', k, 'state', 'running', 'updated_at', ARGV[2])
    redis.call('HINCRBY', k, 'attempts', 1)
    redis.call('ZADD', p .. 'running', ARGV[3], id)
    return id
  end
end
`)

func (s *Store) Claim(ctx context.Context, visibility time.Duration) (*Job, error) {
	now := time.Now()
	id, err := claim.Run(ctx, s.rdb, nil, prefix, ms(now), ms(now.Add(visibility))).Text()
	if errors.Is(err, redis.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.Get(ctx, id)
}

// Heartbeat extends a running job's deadline and reports its current state, so the
// worker can stop if the job was canceled.
func (s *Store) Heartbeat(ctx context.Context, id string, visibility time.Duration) (string, error) {
	var state *redis.StringCmd
	_, err := s.rdb.Pipelined(ctx, func(p redis.Pipeliner) error {
		p.ZAddXX(ctx, prefix+"running", redis.Z{Score: float64(ms(time.Now().Add(visibility))), Member: id})
		state = p.HGet(ctx, jobKey(id), "state")
		return nil
	})
	if err != nil {
		return "", err
	}
	return state.Val(), nil
}

// Expired returns running jobs whose worker stopped heartbeating.
func (s *Store) Expired(ctx context.Context, now time.Time) ([]string, error) {
	return s.rdb.ZRangeByScore(ctx, prefix+"running", &redis.ZRangeBy{Min: "-inf", Max: strconv.FormatInt(ms(now), 10)}).Result()
}

func (s *Store) DropRunning(ctx context.Context, id string) error {
	return s.rdb.ZRem(ctx, prefix+"running", id).Err()
}

var lease = redis.NewScript(`
local cur = redis.call('GET', KEYS[1])
if not cur then redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[2]) return 1 end
if cur == ARGV[1] then redis.call('PEXPIRE', KEYS[1], ARGV[2]) return 1 end
return 0
`)

// AcquireLeader takes or renews the scheduler lease. Only the holder runs the heaps.
func (s *Store) AcquireLeader(ctx context.Context, me string, ttl time.Duration) (bool, error) {
	n, err := lease.Run(ctx, s.rdb, []string{prefix + "leader"}, me, ttl.Milliseconds()).Int()
	return n == 1, err
}
