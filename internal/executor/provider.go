package executor

import (
	"context"

	"github.com/falbanese9484/agent-harness/internal/types"
)

// Provider is the existing streaming provider boundary, narrowed to one turn.
type Provider interface {
	GenerateRequest(string) *types.ChatRequest
	Chat(*types.BusConnector)
}

type providerTurn struct {
	provider Provider
}

// NewProviderTurn adapts a freshly constructed provider. The caller transfers
// exclusive conversation ownership; it must not use the provider concurrently.
func NewProviderTurn(provider Provider) Turn {
	return &providerTurn{provider: provider}
}

func (t *providerTurn) Run(ctx context.Context, prompt string, output func(types.ChatResponse)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if t.provider == nil {
		return ErrProvider
	}
	request := t.provider.GenerateRequest(prompt)
	if request == nil {
		return ErrProvider
	}
	content := make(chan *types.ChatResponse)
	failures := make(chan error, 1)
	done := make(chan bool, 1)
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		t.provider.Chat(&types.BusConnector{
			Ctx: ctx, Request: request, ResponseChan: content,
			ErrorChan: failures, DoneChannel: done,
		})
	}()

	var completed, failed bool
	deliver := func(response *types.ChatResponse) {
		if response == nil || ctx.Err() != nil {
			return
		}
		if response.Kind == types.ResponseKindError {
			failed = true // Never forward raw provider errors as output.
			return
		}
		if output != nil {
			output(*response)
		}
	}
	for {
		select {
		case response := <-content:
			deliver(response)
		case <-failures:
			failed = true
		case success := <-done:
			completed = completed || success
		case <-returned:
			// Unbuffered content has already been delivered. Chat is now stopped,
			// so only buffered completion/error signals may remain.
			failed = failed || len(failures) > 0
			for len(done) > 0 {
				completed = <-done || completed
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if failed || !completed {
				return ErrProvider
			}
			return nil
		}
	}
}
