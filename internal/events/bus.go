package events

import "sync"

// Bus provides simple in-process pub/sub for observability.
type Bus struct {
    mu   sync.RWMutex
    subs []chan any
}

func NewBus() *Bus { return &Bus{} }

func (b *Bus) Subscribe() <-chan any {
    ch := make(chan any, 16)
    b.mu.Lock()
    defer b.mu.Unlock()
    b.subs = append(b.subs, ch)
    return ch
}

func (b *Bus) Publish(ev any) {
    b.mu.RLock()
    defer b.mu.RUnlock()
    for _, ch := range b.subs {
        select {
        case ch <- ev:
        default:
        }
    }
}
