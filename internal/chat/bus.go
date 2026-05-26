package chat

import (
	"context"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

type Bus struct {
	Done     chan bool
	Content  chan *types.ChatResponse
	Error    chan error
	provider types.Provider
	logger   *logger.Logger
}

func NewBus(log *logger.Logger, provider types.Provider) *Bus {
	return &Bus{
		Done:     make(chan bool),
		Content:  make(chan *types.ChatResponse),
		Error:    make(chan error),
		provider: provider,
		logger:   log,
	}
}

func (b *Bus) Start(out chan *types.ChatResponse) {
	for {
		select {
		case response := <-b.Content:
			b.logger.Info("model raw response", "response", response.Response, "thinking", response.Thinking)
			out <- response
		case err := <-b.Error:
			b.logger.Error("chat error", "error", err)
			out <- &types.ChatResponse{Done: true}
		case <-b.Done:
			b.logger.Debug("message complete")
			out <- &types.ChatResponse{Done: true}
		}
	}
}

func (b *Bus) Send(request *types.ChatRequest) {
	b.provider.Chat(&types.BusConnector{
		Ctx:          context.Background(),
		Request:      request,
		ResponseChan: b.Content,
		ErrorChan:    b.Error,
		DoneChannel:  b.Done,
	})
}
