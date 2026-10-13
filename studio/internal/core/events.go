package core

import (
	"encoding/json"
	"sync"
	"time"
)

const ReplayCapacity = 2048
const HeartbeatInterval = 15 * time.Second
const ProgressInterval = 500 * time.Millisecond

type Event struct {
	ID   uint64
	Type string
	Data json.RawMessage
}

// Subscribers receive wakeups, then read the shared ring. Slow consumers get a
// reset instead of silently losing individual events.
type EventBus struct {
	mu   sync.Mutex
	next uint64
	ring []Event
	wake map[chan struct{}]struct{}
}

func NewEventBus() *EventBus {
	return &EventBus{next: uint64(time.Now().UnixNano()), wake: map[chan struct{}]struct{}{}}
}
func (b *EventBus) Publish(kind string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	b.ring = append(b.ring, Event{b.next, kind, data})
	if len(b.ring) > ReplayCapacity {
		copy(b.ring, b.ring[len(b.ring)-ReplayCapacity:])
		b.ring = b.ring[:ReplayCapacity]
	}
	for ch := range b.wake {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return nil
}
func (b *EventBus) Subscribe() (<-chan struct{}, func(), uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	ch := make(chan struct{}, 1)
	b.wake[ch] = struct{}{}
	return ch, func() { b.mu.Lock(); delete(b.wake, ch); b.mu.Unlock() }, b.next
}
func (b *EventBus) Since(id uint64) ([]Event, bool, uint64) {
	return b.since(id, true)
}
func (b *EventBus) After(id uint64) ([]Event, bool, uint64) { return b.since(id, false) }
func (b *EventBus) since(id uint64, strict bool) ([]Event, bool, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if id > b.next || (len(b.ring) > 0 && (id < b.ring[0].ID && (strict || id+1 < b.ring[0].ID))) || (len(b.ring) == 0 && id != b.next) {
		return nil, true, b.next
	}
	out := []Event{}
	for _, e := range b.ring {
		if e.ID > id {
			out = append(out, e)
		}
	}
	return out, false, b.next
}
