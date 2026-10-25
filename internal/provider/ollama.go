package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

const (
	ollamaChatURL = "http://localhost:11434/api/chat"
	ollamaTagsURL = "http://localhost:11434/api/tags"
)

type Ollama struct {
	conversationState
	execution  *ExecutionState
	url        string
	model      string
	messages   []types.Message
	tools      []config.ToolConfig
	modelCache *types.ModelCache
	logger     *logger.Logger
}

type ollamaChatRequest struct {
	Model    string          `json:"model"`
	Messages []types.Message `json:"messages"`
	Tools    []ollamaTool    `json:"tools,omitempty"`
	Stream   bool            `json:"stream"`
}

type ollamaChatResponse struct {
	Message types.Message `json:"message"`
	Done    bool          `json:"done"`
}

type ollamaTool struct {
	Type     string             `json:"type"`
	Function ollamaToolFunction `json:"function"`
}

type ollamaToolFunction struct {
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Parameters  ollamaToolParameters `json:"parameters"`
}

type ollamaToolParameters struct {
	Type       string                        `json:"type"`
	Properties map[string]ollamaToolProperty `json:"properties"`
	Required   []string                      `json:"required,omitempty"`
}

type ollamaToolProperty struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

func NewOllama(log *logger.Logger,
	cfg *config.AgentConfig,
	cache *types.ModelCache,
	execution *ExecutionState,
) *Ollama {
	tools := execution.filterTools(cfg.Tools)
	if len(tools) > 0 && log != nil {
		log.Info("loaded tools config", "tools", len(tools))
	}

	messages := []types.Message{}
	systemPrompt := config.BuildSystemPrompt(cfg.SystemPrompt, tools)
	if systemPrompt != "" {
		messages = append(messages, types.Message{Role: "system", Content: systemPrompt})
	}

	return &Ollama{
		execution:  execution,
		url:        ollamaChatURL,
		model:      cfg.Model,
		messages:   messages,
		tools:      tools,
		modelCache: cache,
		logger:     log,
	}
}

func (o *Ollama) ResetConversation() error {
	if !o.turnMu.TryLock() {
		return ErrConversationBusy
	}
	defer o.turnMu.Unlock()

	var messages []types.Message
	if len(o.messages) > 0 && o.messages[0].Role == "system" {
		messages = []types.Message{o.messages[0]}
	}
	o.messages = messages
	return nil
}

func (o *Ollama) GenerateRequest(prompt string) *types.ChatRequest {
	o.modelMu.RLock()
	defer o.modelMu.RUnlock()
	return &types.ChatRequest{
		Model:    o.model,
		Messages: []types.Message{{Role: "user", Content: prompt}},
		Tools:    cloneTools(o.tools),
		Stream:   true,
	}
}

func (o *Ollama) Chat(conn *types.BusConnector) {
	if !o.turnMu.TryLock() {
		sendEvent(conn.Ctx, conn.ErrorChan, ErrConversationBusy)
		return
	}
	defer o.turnMu.Unlock()
	if conn.Ctx.Err() != nil {
		return
	}
	o.modelMu.RLock()
	model := o.model
	o.modelMu.RUnlock()
	newMessages, err := cloneMessages(conn.Request.Messages)
	if err != nil {
		sendEvent(conn.Ctx, conn.ErrorChan, err)
		return
	}
	if len(newMessages) == 0 && conn.Request.Prompt != "" {
		newMessages = []types.Message{{
			Role:    "user",
			Content: conn.Request.Prompt,
		}}
	}

	messages := append([]types.Message{}, o.messages...)
	messages = append(messages, newMessages...)

	tools := conn.Request.Tools
	if len(tools) == 0 || o.execution == nil || o.execution.worker {
		tools = o.tools
	}

	const maxToolRounds = 100
	for range maxToolRounds {
		if conn.Ctx.Err() != nil {
			return
		}
		assistantMsg, err := o.sendChat(conn, model, messages, tools)
		if err != nil {
			sendEvent(conn.Ctx, conn.ErrorChan, err)
			return
		}

		if conn.Ctx.Err() != nil {
			return
		}
		messages = append(messages, assistantMsg)
		if len(assistantMsg.ToolCalls) == 0 {
			// TODO(review): conversation history grows unbounded and re-sends full tool
			// output every turn; add a context budget / truncation. See notes/review-checklist.md.
			o.messages = messages
			sendEvent(conn.Ctx, conn.DoneChannel, true)
			return
		}

		for _, call := range assistantMsg.ToolCalls {
			if !sendEvent(conn.Ctx, conn.ResponseChan, &types.ChatResponse{
				Response: fmt.Sprintf("\n[executing tool: %s -- %s]\n",
					call.Function.Name,
					call.Function.Arguments),
				Kind:    types.ResponseKindToolStatus,
				Context: conn.Request.Context,
			}) {
				return
			}
			result, err := o.execution.executeToolCall(conn.Ctx, call)
			if conn.Ctx.Err() != nil {
				return
			}
			if err != nil {
				result = "tool error: " + err.Error()
			}
			messages = append(messages, types.Message{
				Role:     "tool",
				Content:  result,
				ToolName: call.Function.Name,
			})
		}
	}

	sendEvent(conn.Ctx, conn.ErrorChan, fmt.Errorf("maximum tool call rounds exceeded"))
}

func (o *Ollama) sendChat(
	conn *types.BusConnector,
	model string,
	messages []types.Message,
	tools []config.ToolConfig,
) (types.Message, error) {
	reqBody := ollamaChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    ollamaTools(tools),
		Stream:   true,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return types.Message{}, err
	}

	req, err := http.NewRequestWithContext(conn.Ctx, "POST",
		o.url, bytes.NewReader(data))
	if err != nil {
		return types.Message{}, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return types.Message{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return types.Message{}, fmt.Errorf("Ollama returned status %d",
			resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	// TODO(review): 1 MB scanner cap silently drops lines larger than the buffer
	// (surfaces as bufio.ErrTooLong). Use bufio.Reader.ReadString('\n') or raise/announce the cap.
	// See notes/review-checklist.md.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var assistant strings.Builder
	var toolCalls []types.ToolCall

	for scanner.Scan() {
		if conn.Ctx.Err() != nil {
			return types.Message{}, conn.Ctx.Err()
		}
		var chunk ollamaChatResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			// TODO(review): silently skipping malformed chunks loses data; OpenRouter aborts
			// instead. Pick one policy across providers. See notes/review-checklist.md.
			continue
		}

		assistant.WriteString(chunk.Message.Content)
		if len(chunk.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		}
		if chunk.Message.Content != "" || chunk.Message.Thinking != "" {
			if !sendEvent(conn.Ctx, conn.ResponseChan, &types.ChatResponse{
				Response: chunk.Message.Content,
				Thinking: chunk.Message.Thinking,
				Done:     false,
				Kind:     types.ResponseKindAssistant,
				Context:  conn.Request.Context,
			}) {
				return types.Message{}, conn.Ctx.Err()
			}
		}
		if chunk.Done {
			return types.Message{
				Role:      "assistant",
				Content:   assistant.String(),
				ToolCalls: toolCalls,
			}, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return types.Message{}, err
	}
	return types.Message{
		Role:      "assistant",
		Content:   assistant.String(),
		ToolCalls: toolCalls,
	}, nil
}

func ollamaTools(tools []config.ToolConfig) []ollamaTool {
	// TODO(review): all params are hardcoded type "string" and marked required, so
	// optional params cannot be expressed. Add type/required to ToolParameter. See notes/review-checklist.md.
	if len(tools) == 0 {
		return nil
	}

	converted := make([]ollamaTool, 0, len(tools))
	for _, tool := range tools {
		properties := make(map[string]ollamaToolProperty, len(tool.Parameters))
		required := make([]string, 0, len(tool.Parameters))
		for _, param := range tool.Parameters {
			properties[param.ParameterName] = ollamaToolProperty{
				Type:        "string",
				Description: param.Description,
			}
			required = append(required, param.ParameterName)
		}

		converted = append(converted, ollamaTool{
			Type: "function",
			Function: ollamaToolFunction{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters: ollamaToolParameters{
					Type:       "object",
					Properties: properties,
					Required:   required,
				},
			},
		})
	}
	return converted
}

func (o *Ollama) RetrieveModels() ([]types.Model, error) {
	if !o.modelCache.IsStale() {
		return o.modelCache.Load(), nil
	}

	resp, err := http.Get(ollamaTagsURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	models := make([]types.Model, len(result.Models))
	for i, m := range result.Models {
		models[i] = types.Model{Name: m.Name}
	}

	o.modelCache.Store(models)
	return models, nil
}

func (o *Ollama) SetModel(model string) {
	o.modelMu.Lock()
	defer o.modelMu.Unlock()
	o.model = model
}
