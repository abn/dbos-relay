package safego

import (
	"log/slog"
	"runtime/debug"
)

// Go runs fn in a new goroutine with panic recovery. If fn panics, the panic
// is recovered, logged at Error level with stack trace, and process termination is prevented.
func Go(logger *slog.Logger, name string, fn func()) {
	if logger == nil {
		logger = slog.Default()
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic in "+name, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		fn()
	}()
}
