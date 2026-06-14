package types

import (
	"context"

	"github.com/falbanese9484/agent-harness/pkg/config"
)

type ResponseKind string

const (
	ResponseKindAssistant  ResponseKind = "assistant"
	ResponseKindToolStatus ResponseKind = "tool_status"
	ResponseKindError      ResponseKind = "error"
)

type ChatRequest struct {
	Model    string              `json:"model"`
	Prompt   string              `json:"prompt,omitempty"`
	Messages []Message           `json:"messages,omitempty"`
	Tools    []config.ToolConfig `json:"tools,omitempty"`
	Stream   bool                `json:"stream"`
	Context  []int               `json:"context,omitempty"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	Thinking   string     `json:"thinking,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type ToolCall struct {
	ID       string           `json:"id,omitempty"`
	Type     string           `json:"type,omitempty"`
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

type ChatResponse struct {
	Response  string       `json:"response"`
	Context   []int        `json:"context,omitempty"`
	Done      bool         `json:"done"`
	Thinking  string       `json:"thinking"`
	ToolCalls []ToolCall   `json:"tool_calls,omitempty"`
	Kind      ResponseKind `json:"kind,omitempty"`
}

type BusConnector struct {
	Ctx          context.Context
	Request      *ChatRequest
	ResponseChan chan *ChatResponse
	ErrorChan    chan error
	DoneChannel  chan bool
}
