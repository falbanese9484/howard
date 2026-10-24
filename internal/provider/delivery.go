package provider

import "context"

// sendEvent never strands a provider when a consumer stops reading and cancels
// its context. The preflight check avoids selecting a ready send for an already
// canceled turn. An in-flight send may race cancellation; the executor suppresses
// canceled output and waits for Chat to return before publishing the terminal.
func sendEvent[T any](ctx context.Context, ch chan<- T, event T) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	case ch <- event:
		return true
	}
}
