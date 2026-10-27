package events

import "sync"

// Bus is an in-process publish/subscribe channel for one event type. Delivery
// is synchronous, in the publisher's goroutine, so subscribers must not block.
type Bus[T any] struct {
	mu     sync.RWMutex
	nextID int
	subs   map[int]func(T)
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

// Publish delivers event to every subscriber. A nil Bus drops it, so services
// can publish whether or not anyone wired a bus.
func (b *Bus[T]) Publish(event T) {
	if b == nil {
		return
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, fn := range b.subs {
		fn(event)
	}
}
