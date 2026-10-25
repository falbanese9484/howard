package chat

import (
	"errors"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

func TestContentWaitsForBusBeforeProviderCanComplete(t *testing.T) {
	bus := NewBus(nil, nil)
	// Before Start receives content, the provider must not be able to finish
	// this send and enqueue a terminal on the independent Done/Error channels.
	// This checks the ordering boundary without relying on select randomness.
	select {
	case bus.Content <- &types.ChatResponse{Response: "old response"}:
		t.Fatal("content accepted without a receiver; completion can overtake it")
	default:
	}
}

func TestBusForwardsContentBeforeTerminal(t *testing.T) {
	for _, failure := range []bool{false, true} {
		name := "done"
		if failure {
			name = "error"
		}
		t.Run(name, func(t *testing.T) {
			log, err := logger.New(logger.Config{FileOnly: true})
			if err != nil {
				t.Fatal(err)
			}
			bus := NewBus(log, nil)
			out := make(chan *types.ChatResponse)
			go bus.Start(out)
			chunks := []*types.ChatResponse{
				{Thinking: "reasoning", Kind: types.ResponseKindAssistant},
				{Response: "tool status", Kind: types.ResponseKindToolStatus},
				{Response: "answer", Kind: types.ResponseKindAssistant},
			}
			for i, chunk := range chunks {
				// Each send returns with Start blocked forwarding this chunk to
				// out. Queue the terminal while the last chunk is still in flight.
				select {
				case bus.Content <- chunk:
				case <-time.After(time.Second):
					t.Fatal("bus did not receive content")
				}
				if i == len(chunks)-1 {
					if failure {
						bus.Error <- errors.New("stream failed")
					} else {
						bus.Done <- true
					}
				}
				if got := receiveResponse(t, out); got != chunk {
					t.Fatalf("got %#v before content %#v", got, chunk)
				}
			}
			terminal := receiveResponse(t, out)
			if !terminal.Done || (terminal.Kind == types.ResponseKindError) != failure {
				t.Fatalf("terminal = %#v", terminal)
			}
			if failure && terminal.Response != "stream failed" {
				t.Fatalf("error = %q", terminal.Response)
			}
		})
	}
}

func receiveResponse(t *testing.T, out <-chan *types.ChatResponse) *types.ChatResponse {
	t.Helper()
	select {
	case response := <-out:
		return response
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for bus output")
		return nil
	}
}
