package chat

import (
	"log/slog"
	"runtime/debug"
)

// safeGo runs f in its own goroutine with a recover: one bad turn (nil
// deref, index-out-of-range, map-without-lock, ...) anywhere in the long
// run()/workflow call chain must not take the whole office process down
// with it, the way internal/burn and internal/channels already guard their
// own background goroutines.
func safeGo(label string, f func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("chat: goroutine panic", "where", label, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		f()
	}()
}
