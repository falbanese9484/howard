package chat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

type fakeProvider struct{}

func (fakeProvider) Chat(c *types.BusConnector) {
	time.Sleep(5 * time.Millisecond)
	c.ResponseChan <- &types.ChatResponse{
		Thinking: "reasoning",
		Kind:     types.ResponseKindAssistant,
	}
	c.DoneChannel <- true
}

func (fakeProvider) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{Prompt: prompt}
}

func (fakeProvider) RetrieveModels() ([]types.Model, error) {
	return nil, nil
}

func (fakeProvider) SetModel(model string) {}

func (fakeProvider) Reset() {}

func TestBusLogsTimeToFirstTokenForThinkingChunk(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "harness.log")
	log, err := logger.New(logger.Config{FilePath: logPath, FileOnly: true})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	bus := NewBus(log, fakeProvider{})
	out := make(chan *types.ChatResponse, 2)
	go bus.Start(out)

	go bus.Send(&types.ChatRequest{Model: "test-model"})

	var sawThinking, sawDone bool
	for i := 0; i < 2; i++ {
		select {
		case response := <-out:
			if response.Thinking == "reasoning" {
				sawThinking = true
			}
			if response.Done {
				sawDone = true
			}
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for response")
		}
	}
	if !sawThinking {
		t.Fatal("did not receive thinking response")
	}
	if !sawDone {
		t.Fatal("did not receive done response")
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	logs := string(data)
	if !strings.Contains(logs, "time to first token") {
		t.Fatalf("logs = %q, want time to first token entry", logs)
	}
	if !strings.Contains(logs, "test-model") {
		t.Fatalf("logs = %q, want model field", logs)
	}
}

func TestBusDoesNotLogToolStatusAsFirstToken(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "harness.log")
	log, err := logger.New(logger.Config{FilePath: logPath, FileOnly: true})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}

	bus := NewBus(log, toolThenAssistantProvider{})
	out := make(chan *types.ChatResponse, 3)
	go bus.Start(out)

	go bus.Send(&types.ChatRequest{Model: "test-model"})

	for i := 0; i < 3; i++ {
		select {
		case <-out:
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for response")
		}
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if strings.Count(string(data), "time to first token") != 1 {
		t.Fatalf("logs = %q, want one time to first token entry", string(data))
	}
}

type toolThenAssistantProvider struct{}

func (toolThenAssistantProvider) Chat(c *types.BusConnector) {
	c.ResponseChan <- &types.ChatResponse{
		Response: "tool",
		Kind:     types.ResponseKindToolStatus,
	}
	c.ResponseChan <- &types.ChatResponse{
		Response: "answer",
		Kind:     types.ResponseKindAssistant,
	}
	c.DoneChannel <- true
}

func (toolThenAssistantProvider) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{Prompt: prompt}
}

func (toolThenAssistantProvider) RetrieveModels() ([]types.Model, error) {
	return nil, nil
}

func (toolThenAssistantProvider) SetModel(model string) {}

func (toolThenAssistantProvider) Reset() {}

var (
	_ types.Provider = fakeProvider{}
	_ types.Provider = toolThenAssistantProvider{}
)
