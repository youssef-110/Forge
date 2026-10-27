package pq

import (
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

func TestHeapSortsRandomInput(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for trial := 0; trial < 50; trial++ {
		h := New(func(a, b int) bool { return a < b })
		var want []int
		for i := 0; i < r.IntN(500); i++ {
			v := r.IntN(100) // many duplicates on purpose
			h.Push(v)
			want = append(want, v)
		}
		slices.Sort(want)
		for i, w := range want {
			got, ok := h.Pop()
			if !ok || got != w {
				t.Fatalf("trial %d pop %d: got %d,%v want %d", trial, i, got, ok, w)
			}
		}
		if _, ok := h.Pop(); ok {
			t.Fatal("heap should be empty")
		}
	}
}

func TestReadyPriorityThenFIFO(t *testing.T) {
	h := NewReady()
	h.Push(Item{ID: "low-old", Priority: 1, Seq: 1})
	h.Push(Item{ID: "high-new", Priority: 9, Seq: 5})
	h.Push(Item{ID: "high-old", Priority: 9, Seq: 2})
	h.Push(Item{ID: "mid", Priority: 5, Seq: 0})
	var got []string
	for h.Len() > 0 {
		it, _ := h.Pop()
		got = append(got, it.ID)
	}
	want := []string{"high-old", "high-new", "mid", "low-old"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestDelayEarliestFirst(t *testing.T) {
	now := time.Now()
	h := NewDelay()
	h.Push(Item{ID: "c", RunAt: now.Add(3 * time.Second)})
	h.Push(Item{ID: "a", RunAt: now.Add(1 * time.Second)})
	h.Push(Item{ID: "b", RunAt: now.Add(2 * time.Second)})
	for _, want := range []string{"a", "b", "c"} {
		if it, _ := h.Pop(); it.ID != want {
			t.Fatalf("got %s want %s", it.ID, want)
		}
	}
}

func BenchmarkReadyPushPop(b *testing.B) {
	h := NewReady()
	r := rand.New(rand.NewPCG(3, 4))
	for i := 0; i < b.N; i++ {
		h.Push(Item{Priority: r.IntN(10), Seq: int64(i)})
		if h.Len() > 10000 {
			h.Pop()
		}
	}
}
