package provider

import (
	"encoding/json"
	"errors"
	"sync"

	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

// ErrConversationBusy rejects overlapping turns rather than sharing mutable
// history. A provider owns one conversation; independent workers need providers
// constructed separately. Callers must not mutate requests while Chat uses them.
var ErrConversationBusy = errors.New("conversation already has an active turn")

type conversationState struct {
	turnMu  sync.Mutex
	modelMu sync.RWMutex
}

func cloneTools(tools []config.ToolConfig) []config.ToolConfig {
	cloned := append([]config.ToolConfig(nil), tools...)
	for i := range cloned {
		cloned[i].Parameters = append([]config.ParameterConfig(nil), tools[i].Parameters...)
	}
	return cloned
}

// Clone the JSON-shaped request data before retaining it in Ollama history, so
// later edits to a completed request cannot change the conversation's tool args.
func cloneMessages(messages []types.Message) ([]types.Message, error) {
	data, err := json.Marshal(messages)
	if err != nil {
		return nil, err
	}
	var cloned []types.Message
	err = json.Unmarshal(data, &cloned)
	return cloned, err
}
