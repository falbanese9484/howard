package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

const (
	openRouterChatURL   = "https://openrouter.ai/api/v1/chat/completions"
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
)

type OpenRouter struct {
	url        string
	apiKey     string
	model      string
	messages   []openRouterMessage
	tools      []config.ToolConfig
	modelCache *types.ModelCache
	logger     *logger.Logger
}

type openRouterMessage struct {
	Role       string               `json:"role"`
	Content    string               `json:"content,omitempty"`
	ToolCalls  []openRouterToolCall `json:"tool_calls,omitempty"`
	ToolCallID string               `json:"tool_call_id,omitempty"`
	Name       string               `json:"name,omitempty"`
}

type openRouterToolCall struct {
	ID       string                 `json:"id,omitempty"`
	Type     string                 `json:"type,omitempty"`
	Function openRouterToolFunction `json:"function"`
}

type openRouterToolFunction struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type openRouterRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
	Tools    []ollamaTool        `json:"tools,omitempty"`
	Stream   bool                `json:"stream"`
}

type openRouterStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content   string                    `json:"content"`
			Reasoning string                    `json:"reasoning"`
			Thinking  string                    `json:"thinking"`
			ToolCalls []openRouterToolCallDelta `json:"tool_calls"`
		} `json:"delta"`
	} `json:"choices"`
}

type openRouterToolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id,omitempty"`
	Type     string `json:"type,omitempty"`
	Function struct {
		Name      string `json:"name,omitempty"`
		Arguments string `json:"arguments,omitempty"`
	} `json:"function"`
}

type openRouterModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func NewOpenRouter(
	log *logger.Logger,
	cfg *config.AgentConfig,
	cache *types.ModelCache,
) (*OpenRouter, error) {
	if cfg.ApiKey == "" {
		return nil, errors.New("OpenRouter API key required")
	}

	tools := cfg.Tools
	if len(tools) > 0 && log != nil {
		log.Info("loaded tools config", "tools", len(tools))
	}

	messages := []openRouterMessage{}
	systemPrompt := config.BuildSystemPrompt(cfg.SystemPrompt)
	if systemPrompt != "" {
		messages = append(messages, openRouterMessage{Role: "system", Content: systemPrompt})
	}

	return &OpenRouter{
		url:        openRouterChatURL,
		apiKey:     cfg.ApiKey,
		model:      cfg.Model,
		messages:   messages,
		tools:      tools,
		modelCache: cache,
		logger:     log,
	}, nil
}

func (o *OpenRouter) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{
		Model:    o.model,
		Messages: []types.Message{{Role: "user", Content: prompt}},
		Tools:    o.tools,
		Stream:   true,
	}
}

func (o *OpenRouter) Chat(conn *types.BusConnector) {
	newMessages := openRouterMessages(conn.Request.Messages)
	if len(newMessages) == 0 && conn.Request.Prompt != "" {
		newMessages = []openRouterMessage{{Role: "user", Content: conn.Request.Prompt}}
	}

	messages := append([]openRouterMessage{}, o.messages...)
	messages = append(messages, newMessages...)

	tools := conn.Request.Tools
	if len(tools) == 0 {
		tools = o.tools
	}

	const maxToolRounds = 100
	for range maxToolRounds {
		assistantMsg, calls, err := o.sendChat(conn, messages, tools)
		if err != nil {
			conn.ErrorChan <- err
			return
		}

		messages = append(messages, assistantMsg)
		if len(calls) == 0 {
			o.messages = messages
			conn.DoneChannel <- true
			return
		}

		for _, call := range calls {
			conn.ResponseChan <- &types.ChatResponse{Response: fmt.Sprintf("\n[executing tool: %s]\n", call.Function.Name), Kind: types.ResponseKindToolStatus}
			result, err := executeToolCall(call)
			if err != nil {
				result = "tool error: " + err.Error()
			}
			messages = append(messages, openRouterMessage{
				Role:       "tool",
				Content:    result,
				ToolCallID: call.ID,
				Name:       call.Function.Name,
			})
		}
	}

	conn.ErrorChan <- fmt.Errorf("maximum tool call rounds exceeded")
}

func (o *OpenRouter) sendChat(conn *types.BusConnector, messages []openRouterMessage, tools []config.ToolConfig) (openRouterMessage, []types.ToolCall, error) {
	reqBody := openRouterRequest{
		Model:    o.model,
		Messages: messages,
		Tools:    ollamaTools(tools),
		Stream:   true,
	}

	data, err := json.Marshal(&reqBody)
	if err != nil {
		return openRouterMessage{}, nil, err
	}

	req, err := http.NewRequestWithContext(conn.Ctx, "POST", o.url, bytes.NewReader(data))
	if err != nil {
		return openRouterMessage{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return openRouterMessage{}, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return openRouterMessage{}, nil, fmt.Errorf("OpenRouter returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var assistant strings.Builder
	var thinking strings.Builder
	builders := map[int]*openRouterToolCallBuilder{}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			toolCalls, calls, err := buildOpenRouterToolCalls(builders)
			if err != nil {
				return openRouterMessage{}, nil, err
			}
			return openRouterMessage{Role: "assistant", Content: assistant.String(), ToolCalls: toolCalls}, calls, nil
		}

		var chunk openRouterStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return openRouterMessage{}, nil, err
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta
		assistant.WriteString(delta.Content)
		thinkingText := delta.Thinking
		if thinkingText == "" {
			thinkingText = delta.Reasoning
		}
		thinking.WriteString(thinkingText)

		for _, tc := range delta.ToolCalls {
			b := builders[tc.Index]
			if b == nil {
				b = &openRouterToolCallBuilder{}
				builders[tc.Index] = b
			}
			if tc.ID != "" {
				b.id = tc.ID
			}
			if tc.Type != "" {
				b.callType = tc.Type
			}
			if tc.Function.Name != "" {
				b.name = tc.Function.Name
			}
			b.arguments.WriteString(tc.Function.Arguments)
		}

		if delta.Content != "" || thinkingText != "" {
			conn.ResponseChan <- &types.ChatResponse{Response: delta.Content, Thinking: thinkingText, Done: false, Kind: types.ResponseKindAssistant, Context: conn.Request.Context}
		}
	}

	if err := scanner.Err(); err != nil {
		return openRouterMessage{}, nil, err
	}

	toolCalls, calls, err := buildOpenRouterToolCalls(builders)
	if err != nil {
		return openRouterMessage{}, nil, err
	}
	return openRouterMessage{Role: "assistant", Content: assistant.String(), ToolCalls: toolCalls}, calls, nil
}

type openRouterToolCallBuilder struct {
	id        string
	callType  string
	name      string
	arguments strings.Builder
}

func buildOpenRouterToolCalls(builders map[int]*openRouterToolCallBuilder) ([]openRouterToolCall, []types.ToolCall, error) {
	if len(builders) == 0 {
		return nil, nil, nil
	}

	indexes := make([]int, 0, len(builders))
	for index := range builders {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	openRouterCalls := make([]openRouterToolCall, 0, len(indexes))
	calls := make([]types.ToolCall, 0, len(indexes))
	for _, index := range indexes {
		b := builders[index]
		callType := b.callType
		if callType == "" {
			callType = "function"
		}
		id := b.id
		if id == "" {
			id = "call_" + strconv.Itoa(index)
		}
		argsJSON := b.arguments.String()
		var args map[string]any
		if strings.TrimSpace(argsJSON) == "" {
			args = map[string]any{}
		} else if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return nil, nil, fmt.Errorf("decode tool arguments for %s: %w", b.name, err)
		}

		openRouterCalls = append(openRouterCalls, openRouterToolCall{
			ID:   id,
			Type: callType,
			Function: openRouterToolFunction{
				Name:      b.name,
				Arguments: argsJSON,
			},
		})
		calls = append(calls, types.ToolCall{
			ID:   id,
			Type: callType,
			Function: types.ToolCallFunction{
				Name:      b.name,
				Arguments: args,
			},
		})
	}
	return openRouterCalls, calls, nil
}

func openRouterMessages(messages []types.Message) []openRouterMessage {
	converted := make([]openRouterMessage, 0, len(messages))
	for _, msg := range messages {
		converted = append(converted, openRouterMessage{
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCalls:  openRouterToolCalls(msg.ToolCalls),
			ToolCallID: msg.ToolCallID,
			Name:       msg.ToolName,
		})
	}
	return converted
}

func openRouterToolCalls(calls []types.ToolCall) []openRouterToolCall {
	if len(calls) == 0 {
		return nil
	}
	converted := make([]openRouterToolCall, 0, len(calls))
	for _, call := range calls {
		callType := call.Type
		if callType == "" {
			callType = "function"
		}
		args, _ := json.Marshal(call.Function.Arguments)
		converted = append(converted, openRouterToolCall{
			ID:   call.ID,
			Type: callType,
			Function: openRouterToolFunction{
				Name:      call.Function.Name,
				Arguments: string(args),
			},
		})
	}
	return converted
}

func (o *OpenRouter) RetrieveModels() ([]types.Model, error) {
	if !o.modelCache.IsStale() {
		return o.modelCache.Load(), nil
	}

	resp, err := http.Get(openRouterModelsURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to retrieve models: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result openRouterModelsResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	models := make([]types.Model, len(result.Data))
	for i, m := range result.Data {
		models[i] = types.Model{Name: m.ID}
	}

	o.modelCache.Store(models)
	return models, nil
}

func (o *OpenRouter) SetModel(model string) {
	o.model = model
}
