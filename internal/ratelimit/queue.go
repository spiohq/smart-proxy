package ratelimit

import (
	"container/heap"
	"context"
	"errors"
	"sync"
	"time"
)

type PriorityLevel int

const (
	PriorityHigh   PriorityLevel = 0
	PriorityNormal PriorityLevel = 1
	PriorityLow    PriorityLevel = 2
)

var (
	ErrQueueFull    = errors.New("queue full")
	ErrQueueStopped = errors.New("queue stopped")
)

type queuedRequest struct {
	priority   PriorityLevel
	enqueuedAt time.Time
	ready      chan struct{}
	index      int
}

type requestHeap []*queuedRequest

func (h requestHeap) Len() int { return len(h) }
func (h requestHeap) Less(i, j int) bool {
	if h[i].priority != h[j].priority {
		return h[i].priority < h[j].priority
	}
	return h[i].enqueuedAt.Before(h[j].enqueuedAt)
}
func (h requestHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
	h[i].index = i
	h[j].index = j
}
func (h *requestHeap) Push(x any) {
	item := x.(*queuedRequest)
	item.index = len(*h)
	*h = append(*h, item)
}
func (h *requestHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*h = old[:n-1]
	return item
}

type RequestQueue struct {
	mu       sync.Mutex
	heap     requestHeap
	maxDepth int
	bucket   *TokenBucket
	done     chan struct{}
	notify   chan struct{}
}

func NewRequestQueue(bucket *TokenBucket, maxDepth int) *RequestQueue {
	q := &RequestQueue{
		heap:     make(requestHeap, 0),
		maxDepth: maxDepth,
		bucket:   bucket,
		done:     make(chan struct{}),
		notify:   make(chan struct{}, 1),
	}
	heap.Init(&q.heap)
	go q.dispatch()
	return q
}

func (q *RequestQueue) Enqueue(ctx context.Context, priority PriorityLevel) error {
	q.mu.Lock()
	if q.heap.Len() >= q.maxDepth {
		q.mu.Unlock()
		return ErrQueueFull
	}

	req := &queuedRequest{
		priority:   priority,
		enqueuedAt: time.Now(),
		ready:      make(chan struct{}),
	}
	heap.Push(&q.heap, req)
	q.mu.Unlock()

	select {
	case q.notify <- struct{}{}:
	default:
	}

	select {
	case <-req.ready:
		return nil
	case <-ctx.Done():
		q.remove(req)
		return ctx.Err()
	case <-q.done:
		return ErrQueueStopped
	}
}

func (q *RequestQueue) remove(req *queuedRequest) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if req.index >= 0 && req.index < q.heap.Len() && q.heap[req.index] == req {
		heap.Remove(&q.heap, req.index)
	}
}

func (q *RequestQueue) dispatch() {
	for {
		q.mu.Lock()
		if q.heap.Len() == 0 {
			q.mu.Unlock()
			select {
			case <-q.notify:
				continue
			case <-q.done:
				return
			}
		}

		allowed, wait := q.bucket.TryConsume()
		if allowed {
			req := heap.Pop(&q.heap).(*queuedRequest)
			q.mu.Unlock()
			close(req.ready)
			continue
		}
		q.mu.Unlock()

		const maxWait = 100 * time.Millisecond
		if wait > maxWait {
			wait = maxWait
		}
		timer := time.NewTimer(wait)
		select {
		case <-timer.C:
		case <-q.notify:
			timer.Stop()
		case <-q.done:
			timer.Stop()
			return
		}
	}
}

func (q *RequestQueue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.heap.Len()
}

func (q *RequestQueue) Stop() {
	select {
	case <-q.done:
	default:
		close(q.done)
	}
}
