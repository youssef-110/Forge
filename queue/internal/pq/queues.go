package pq

import "time"

// Item is a job reference held by the scheduler. The job itself lives in Redis.
type Item struct {
	ID       string
	Priority int       // 0..9, higher runs first
	Seq      int64     // creation time in ms; FIFO tiebreak within a priority
	RunAt    time.Time // only used by the delay queue
}

// NewReady orders by priority (high first), then age (old first), then ID for a total order.
func NewReady() *Heap[Item] {
	return New(func(a, b Item) bool {
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if a.Seq != b.Seq {
			return a.Seq < b.Seq
		}
		return a.ID < b.ID
	})
}

// NewDelay orders by the time a retry becomes due, earliest first.
func NewDelay() *Heap[Item] {
	return New(func(a, b Item) bool {
		if !a.RunAt.Equal(b.RunAt) {
			return a.RunAt.Before(b.RunAt)
		}
		return a.ID < b.ID
	})
}
