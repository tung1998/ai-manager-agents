package auth

import (
	"sync"
	"time"
)

// throttle locks a key after too many failures inside a sliding window.
// In-memory is enough for a single office process (ADR-004: one process).
type throttle struct {
	mu       sync.Mutex
	max      int
	window   time.Duration
	failures map[string][]time.Time
}

func newThrottle(max int, window time.Duration) *throttle {
	return &throttle{max: max, window: window, failures: map[string][]time.Time{}}
}

func (t *throttle) prune(key string, now time.Time) []time.Time {
	kept := t.failures[key][:0]
	for _, at := range t.failures[key] {
		if now.Sub(at) < t.window {
			kept = append(kept, at)
		}
	}
	if len(kept) == 0 {
		delete(t.failures, key)
		return nil
	}
	t.failures[key] = kept
	return kept
}

func (t *throttle) locked(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.prune(key, now)) >= t.max
}

func (t *throttle) fail(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failures[key] = append(t.prune(key, now), now)
}

func (t *throttle) reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, key)
}
