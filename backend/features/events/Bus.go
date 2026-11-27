package events

import "sync"

// Bus is an in-process publish/subscribe channel for one event type. Delivery
// is synchronous, in the publisher's goroutine, so subscribers must not block.
// A linked bus (Over, durable.Link; forge 21f-2) also carries its events to
// other processes.
type Bus[T any] struct {
	mu     sync.RWMutex
	nextID int
	subs   map[int]func(T)
	// out, when linked, sends a published event on to other processes and
	// says whether it is also delivered here.
	out func(T) (here bool)
}

func NewBus[T any]() *Bus[T] {
	return &Bus[T]{subs: map[int]func(T){}}
}

// Subscribe calls fn for every event published until unsubscribe is called.
func (b *Bus[T]) Subscribe(fn func(T)) (unsubscribe func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := b.nextID
	b.nextID++
	b.subs[id] = fn
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		delete(b.subs, id)
	}
}

// Publish delivers event to every subscriber, and through the bus's link
// to other processes. A nil Bus drops it, so services can publish whether
// or not anyone wired a bus.
func (b *Bus[T]) Publish(event T) {
	if b == nil {
		return
	}
	b.mu.RLock()
	out := b.out
	b.mu.RUnlock()
	if out != nil && !out(event) {
		return
	}
	b.Deliver(event)
}

// Deliver gives event to this process's subscribers alone: what a link
// calls for an event another process published.
func (b *Bus[T]) Deliver(event T) {
	if b == nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, fn := range b.subs {
		fn(event)
	}
}

// Link sends every event published from now on to out as well, which
// says whether the event is also delivered here; unlink ends it. A bus
// has one link at a time.
func (b *Bus[T]) Link(out func(T) (here bool)) (unlink func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.out = out
	return func() {
		b.mu.Lock()
		defer b.mu.Unlock()
		b.out = nil
	}
}
