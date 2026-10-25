// Package chat is the intermediate channel in which the backend providers connect
// with the different application entrypoints
package chat

import (
	"context"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

// this is my favorite file in the codebase.

type Bus struct {
	Done     chan bool
	Content  chan *types.ChatResponse
	Error    chan error
	provider types.Provider
	logger   *logger.Logger
}

func NewBus(log *logger.Logger, provider types.Provider) *Bus {
	// Content must be unbuffered: a provider sends all content before its terminal
	// signal, and Start forwards each received chunk before selecting again. This
	// prevents Done or Error from overtaking content and leaving it for a later turn.
	// TODO(review): Content blocks the provider goroutine forever if the
	// consumer stops draining. See notes/review-checklist.md.
	return &Bus{
		Done:     make(chan bool, 1),
		Content:  make(chan *types.ChatResponse),
		Error:    make(chan error, 1),
		provider: provider,
		logger:   log,
	}
}

func (b *Bus) Start(out chan *types.ChatResponse) {
	for {
		select {
		case response := <-b.Content:
			out <- response
		case err := <-b.Error:
			b.logger.Error("chat error", "error", err)
			out <- &types.ChatResponse{
				Response: err.Error(),
				Done:     true,
				Kind:     types.ResponseKindError,
			}
		case <-b.Done:
			b.logger.Debug("message complete")
			out <- &types.ChatResponse{Done: true}
		}
	}
}

func (b *Bus) Send(request *types.ChatRequest) {
	b.provider.Chat(&types.BusConnector{
		// TODO(review): Ctx is always context.Background() and never cancelled, so in-flight
		// requests cannot be aborted. Wire real cancellation (Esc/Ctrl+C, new prompt). See notes/review-checklist.md.
		Ctx:          context.Background(),
		Request:      request,
		ResponseChan: b.Content,
		ErrorChan:    b.Error,
		DoneChannel:  b.Done,
	})
}
