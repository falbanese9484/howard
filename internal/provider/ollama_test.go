package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/types"
)

func TestOllamaChatSendsFinalMessageAfterToolCall(t *testing.T) {
	var requests []ollamaChatRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, req)

		w.Header().Set("Content-Type", "application/x-ndjson")
		switch len(requests) {
		case 1:
			fmt.Fprintln(w, `{"message":{"role":"assistant","thinking":"I need a tool.","tool_calls":[{"function":{"name":"missing_tool","arguments":{"input":"value"}}}]},"done":false}`)
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":""},"done":true}`)
		case 2:
			fmt.Fprintln(w, `{"message":{"role":"assistant","content":"Final answer after the tool."},"done":true}`)
		default:
			t.Fatalf("unexpected request %d", len(requests))
		}
	}))
	defer server.Close()

	o := &Ollama{url: server.URL, model: "test-model", execution: setupToolTestProject(t)}
	responses := make(chan *types.ChatResponse, 10)
	errors := make(chan error, 1)
	done := make(chan bool, 1)

	o.Chat(&types.BusConnector{
		Ctx: context.Background(),
		Request: &types.ChatRequest{
			Messages: []types.Message{{Role: "user", Content: "use a tool"}},
			Stream:   true,
		},
		ResponseChan: responses,
		ErrorChan:    errors,
		DoneChannel:  done,
	})

	select {
	case err := <-errors:
		t.Fatalf("Chat() error = %v", err)
	default:
	}

	select {
	case <-done:
	default:
		t.Fatal("Chat() did not signal done")
	}
	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}

	secondMessages := requests[1].Messages
	if len(secondMessages) != 3 {
		t.Fatalf("second request messages = %#v, want user, assistant tool call, tool result", secondMessages)
	}
	if secondMessages[1].Thinking != "" {
		t.Fatalf("assistant thinking was replayed in history: %q", secondMessages[1].Thinking)
	}
	if len(secondMessages[1].ToolCalls) != 1 {
		t.Fatalf("assistant tool calls = %#v, want one", secondMessages[1].ToolCalls)
	}
	if secondMessages[2].Role != "tool" || secondMessages[2].ToolName != "missing_tool" {
		t.Fatalf("tool result message = %#v, want role tool with tool_name", secondMessages[2])
	}

	var final string
	for len(responses) > 0 {
		response := <-responses
		if response.Kind != types.ResponseKindAssistant {
			continue
		}
		final += response.Response
	}
	if final != "Final answer after the tool." {
		t.Fatalf("streamed response = %q, want final answer", final)
	}
}
