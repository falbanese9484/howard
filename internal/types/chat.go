package types

import "context"

type ChatRequest struct {
	Model    string    `json:"model"`
	Prompt   string    `json:"prompt,omitempty"`
	Messages []Message `json:"messages,omitempty"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
	Context  []int     `json:"context,omitempty"`
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
	Response  string     `json:"response"`
	Context   []int      `json:"context,omitempty"`
	Done      bool       `json:"done"`
	Thinking  string     `json:"thinking"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

type BusConnector struct {
	Ctx          context.Context
	Request      *ChatRequest
	ResponseChan chan *ChatResponse
	ErrorChan    chan error
	DoneChannel  chan bool
}

type ToolParam struct {
	Param       string `json:"param"`
	Description string `json:"description"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Parameters  []ToolParam `json:"parameters"`
}
