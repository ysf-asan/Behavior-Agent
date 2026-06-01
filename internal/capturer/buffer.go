package capturer

import "sync"

type EventBuffer struct {
	mu      sync.Mutex
	events  []InputEvent
	maxSize int
}

func NewEventBuffer(maxSize int) *EventBuffer {
	return &EventBuffer{
		events:  make([]InputEvent, 0, maxSize),
		maxSize: maxSize,
	}
}

func (b *EventBuffer) Add(e InputEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.events) >= b.maxSize {
		b.events = b.events[1:]
	}
	b.events = append(b.events, e)
}

func (b *EventBuffer) Flush() []InputEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	result := b.events
	b.events = make([]InputEvent, 0, b.maxSize)
	return result
}

func (b *EventBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.events)
}
