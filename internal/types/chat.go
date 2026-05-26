package types

import "context"

type ChatRequest struct {
	Model   string `json:"model"`
	Prompt  string `json:"prompt"`
	Stream  bool   `json:"stream"`
	Context []int  `json:"context,omitempty"`
}

type ChatResponse struct {
	Response string `json:"response"`
	Context  []int  `json:"context,omitempty"`
	Done     bool   `json:"done"`
	Thinking string `json:"thinking"`
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
