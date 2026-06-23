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
	// TODO(review): Content (cap 16) blocks the provider goroutine forever if the
	// consumer stops draining. See notes/review-checklist.md.
	return &Bus{
		Done:     make(chan bool, 1),
		Content:  make(chan *types.ChatResponse, 16),
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
			// A turn sends all Content before signaling Error/Done on its own
			// channel, so any buffered Content is already queued. Flush it first
			// so the terminal event never races ahead of trailing tokens.
			b.drainContent(out)
			b.logger.Error("chat error", "error", err)
			out <- &types.ChatResponse{
				Response: err.Error(),
				Done:     true,
				Kind:     types.ResponseKindError,
			}
		case <-b.Done:
			b.drainContent(out)
			b.logger.Debug("message complete")
			out <- &types.ChatResponse{Done: true}
		}
	}
}

// drainContent flushes any buffered Content responses to out without blocking.
func (b *Bus) drainContent(out chan *types.ChatResponse) {
	for {
		select {
		case response := <-b.Content:
			out <- response
		default:
			return
		}
	}
}

func (b *Bus) Send(request *types.ChatRequest) {
	b.SendWithContext(context.Background(), request)
}

// SendWithContext drives the provider with a caller-supplied context so the
// turn can be cancelled (e.g. a web "stop" button or client disconnect).
// Cancelling ctx aborts the provider's in-flight HTTP request, which surfaces
// as an error on the bus.
func (b *Bus) SendWithContext(ctx context.Context, request *types.ChatRequest) {
	b.provider.Chat(&types.BusConnector{
		Ctx:          ctx,
		Request:      request,
		ResponseChan: b.Content,
		ErrorChan:    b.Error,
		DoneChannel:  b.Done,
	})
}
