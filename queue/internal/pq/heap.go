// Package pq is a hand-written binary min-heap and the two queues the scheduler is built on.
// It deliberately avoids container/heap: the point of the package is the algorithm.
package pq

// Heap is a binary heap ordered by less. Push and Pop are O(log n), Peek is O(1).
type Heap[T any] struct {
	items []T
	less  func(a, b T) bool
}

func New[T any](less func(a, b T) bool) *Heap[T] {
	return &Heap[T]{less: less}
}

func (h *Heap[T]) Len() int { return len(h.items) }

func (h *Heap[T]) Push(x T) {
	h.items = append(h.items, x)
	h.up(len(h.items) - 1)
}

func (h *Heap[T]) Peek() (T, bool) {
	var zero T
	if len(h.items) == 0 {
		return zero, false
	}
	return h.items[0], true
}

func (h *Heap[T]) Pop() (T, bool) {
	var zero T
	n := len(h.items)
	if n == 0 {
		return zero, false
	}
	top := h.items[0]
	h.items[0] = h.items[n-1]
	h.items[n-1] = zero // drop the reference so the GC can reclaim it
	h.items = h.items[:n-1]
	if len(h.items) > 0 {
		h.down(0)
	}
	return top, true
}

func (h *Heap[T]) Clear() { h.items = h.items[:0] }

func (h *Heap[T]) up(i int) {
	for i > 0 {
		parent := (i - 1) / 2
		if !h.less(h.items[i], h.items[parent]) {
			return
		}
		h.items[i], h.items[parent] = h.items[parent], h.items[i]
		i = parent
	}
}

func (h *Heap[T]) down(i int) {
	n := len(h.items)
	for {
		smallest, l, r := i, 2*i+1, 2*i+2
		if l < n && h.less(h.items[l], h.items[smallest]) {
			smallest = l
		}
		if r < n && h.less(h.items[r], h.items[smallest]) {
			smallest = r
		}
		if smallest == i {
			return
		}
		h.items[i], h.items[smallest] = h.items[smallest], h.items[i]
		i = smallest
	}
}
