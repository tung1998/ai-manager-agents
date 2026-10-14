package monitor

import (
	"log/slog"
	"runtime/debug"
)

// safeGo runs f in its own goroutine with a recover: a panic in one
// monitor's AI analysis or periodic check must not take the whole office
// process down with it.
func safeGo(label string, f func()) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Error("monitor: goroutine panic", "where", label, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		f()
	}()
}
