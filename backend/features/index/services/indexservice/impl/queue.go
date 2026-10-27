package impl

import (
	"context"
	"sync"
	"time"
)

// queue holds the files waiting to be indexed. touch debounces a file that is
// being edited; push queues it at once. Each file is pending at most once.
type queue struct {
	delay time.Duration

	mu      sync.Mutex
	timers  map[string]*time.Timer
	pending map[string]bool // file -> force
	order   []string
	wake    chan struct{}
}

func newQueue(delay time.Duration) *queue {
	return &queue{delay: delay, timers: map[string]*time.Timer{}, pending: map[string]bool{}, wake: make(chan struct{}, 1)}
}

// touch queues file once it has been left alone for the queue's delay.
func (q *queue) touch(file string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if t, ok := q.timers[file]; ok {
		t.Reset(q.delay)
		return
	}
	q.timers[file] = time.AfterFunc(q.delay, func() {
		q.mu.Lock()
		delete(q.timers, file)
		q.mu.Unlock()
		q.push(file, false)
	})
}

func (q *queue) push(file string, force bool) {
	q.mu.Lock()
	if f, ok := q.pending[file]; ok {
		q.pending[file] = f || force
	} else {
		q.pending[file] = force
		q.order = append(q.order, file)
	}
	q.mu.Unlock()
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *queue) pop() (file string, force, ok bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.order) == 0 {
		return "", false, false
	}
	file = q.order[0]
	q.order = q.order[1:]
	force = q.pending[file]
	delete(q.pending, file)
	return file, force, true
}

// run calls fn for each queued file, one at a time, until ctx ends.
func (q *queue) run(ctx context.Context, fn func(file string, force bool)) {
	for {
		for {
			file, force, ok := q.pop()
			if !ok {
				break
			}
			fn(file, force)
		}
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		}
	}
}
