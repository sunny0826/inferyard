package remote

import (
	"sync"
	"time"
)

type eventBus struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]chan HubEvent
	closed      bool
}

func (b *eventBus) subscribe() (<-chan HubEvent, func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		ch := make(chan HubEvent)
		close(ch)
		return ch, func() {}
	}
	b.nextID++
	id := b.nextID
	ch := make(chan HubEvent, 64)
	if b.subscribers == nil {
		b.subscribers = make(map[uint64]chan HubEvent)
	}
	b.subscribers[id] = ch
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if existing, ok := b.subscribers[id]; ok {
				delete(b.subscribers, id)
				close(existing)
			}
		})
	}
}

func (b *eventBus) emit(event HubEvent) {
	event.Timestamp = time.Now()
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, subscriber := range b.subscribers {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func (b *eventBus) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for id, subscriber := range b.subscribers {
		delete(b.subscribers, id)
		close(subscriber)
	}
}
