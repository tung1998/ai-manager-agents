package chat

import (
	"sync"
	"testing"
	"time"
)

// TestSafeGoRecoversPanic: a panic inside a background goroutine started with
// safeGo must not crash the test process — without the fix (go f() with no
// recover), this panic would terminate the whole binary.
func TestSafeGoRecoversPanic(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	safeGo("test", func() {
		defer wg.Done()
		panic("boom")
	})
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("goroutine never finished")
	}
}
