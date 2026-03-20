// Package debounce provides a timer-based push debounce buffer.
// It collapses rapid pushes to the same branch into a single callback,
// firing only after a configurable quiet window elapses.
package debounce

import (
	"sync"
	"time"
)

// Key identifies a unique branch being tracked.
type Key struct {
	Owner  string
	Repo   string
	Branch string
}

// Callback is invoked when the debounce window expires for a key.
// It receives the key and the most recent data pushed for that key.
type Callback func(Key, any)

// entry holds the pending timer and latest data for a debounced key.
type entry struct {
	timer *time.Timer
	data  any
}

// Buffer tracks pending pushes per branch and fires a callback
// after the configured window of inactivity.
type Buffer struct {
	window   time.Duration
	callback Callback

	mu      sync.Mutex
	entries map[Key]*entry
	stopped bool
}

// NewBuffer creates a Buffer that waits for window duration of quiet
// before invoking callback for each key.
func NewBuffer(window time.Duration, callback Callback) *Buffer {
	return &Buffer{
		window:   window,
		callback: callback,
		entries:  make(map[Key]*entry),
	}
}

// Push records a push for the given key. If there is already a pending
// timer for this key, it is reset and the data is updated. The callback
// fires once the window elapses without another push for the same key.
func (b *Buffer) Push(key Key, data any) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.stopped {
		return
	}

	if e, ok := b.entries[key]; ok {
		if e.timer.Stop() {
			// Timer hadn't fired yet; safe to reuse.
			e.data = data
			e.timer.Reset(b.window)
			return
		}
		// Timer already fired; a stale fire() goroutine may be pending.
		// Fall through to create a new entry so the stale goroutine
		// sees a different *entry and exits harmlessly.
	}

	e := &entry{data: data}
	e.timer = time.AfterFunc(b.window, func() {
		b.fire(key, e)
	})
	b.entries[key] = e
}

// Cancel removes a pending debounce for the given key without firing.
func (b *Buffer) Cancel(key Key) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if e, ok := b.entries[key]; ok {
		e.timer.Stop()
		delete(b.entries, key)
	}
}

// Stop cancels all pending debounces. No further callbacks will fire.
func (b *Buffer) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.stopped = true
	for _, e := range b.entries {
		e.timer.Stop()
	}
	b.entries = nil
}

// fire is called by the timer goroutine when a debounce window expires.
// The expected parameter lets stale goroutines (from a replaced timer)
// detect that their entry has been superseded and exit without firing.
func (b *Buffer) fire(key Key, expected *entry) {
	b.mu.Lock()
	e, ok := b.entries[key]
	if !ok || e != expected {
		b.mu.Unlock()
		return
	}
	data := e.data
	delete(b.entries, key)
	b.mu.Unlock()

	b.callback(key, data)
}
