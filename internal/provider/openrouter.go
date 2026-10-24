package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
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

// openRouterStreamClient bounds connection setup and time-to-first-byte, but
// deliberately has no overall Timeout: a total timeout would cut off long
// streaming responses mid-stream. Cancellation is handled via the request
// context.
var openRouterStreamClient = &http.Client{
	Transport: &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 60 * time.Second,
	},
}

type OpenRouter struct {
	conversationState
	execution  *ExecutionState
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
	execution *ExecutionState,
) (*OpenRouter, error) {
	if cfg.ApiKey == "" {
		return nil, errors.New("OpenRouter API key required")
	}

	tools := execution.filterTools(cfg.Tools)
	if len(tools) > 0 && log != nil {
		log.Info("loaded tools config", "tools", len(tools))
	}

	messages := []openRouterMessage{}
	systemPrompt := config.BuildSystemPrompt(cfg.SystemPrompt, tools)
	if systemPrompt != "" {
		messages = append(messages, openRouterMessage{Role: "system", Content: systemPrompt})
	}

	return &OpenRouter{
		execution:  execution,
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
	o.modelMu.RLock()
	defer o.modelMu.RUnlock()
	return &types.ChatRequest{
		Model:    o.model,
		Messages: []types.Message{{Role: "user", Content: prompt}},
		Tools:    cloneTools(o.tools),
		Stream:   true,
	}
}

func (o *OpenRouter) Chat(conn *types.BusConnector) {
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
	// TODO(review): this agentic loop is ~90% identical to Ollama.Chat; extract a shared
	// driver taking a sendChat-like func. See notes/review-checklist.md.
	newMessages := openRouterMessages(conn.Request.Messages)
	if len(newMessages) == 0 && conn.Request.Prompt != "" {
		newMessages = []openRouterMessage{{Role: "user", Content: conn.Request.Prompt}}
	}

	messages := append([]openRouterMessage{}, o.messages...)
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
		assistantMsg, calls, err := o.sendChat(conn, model, messages, tools)
		if err != nil {
			sendEvent(conn.Ctx, conn.ErrorChan, err)
			return
		}

		if conn.Ctx.Err() != nil {
			return
		}
		messages = append(messages, assistantMsg)
		if len(calls) == 0 {
			// TODO(review): conversation history grows unbounded and re-sends full tool
			// output every turn; add a context budget / truncation. See notes/review-checklist.md.
			o.messages = messages
			sendEvent(conn.Ctx, conn.DoneChannel, true)
			return
		}

		for _, call := range calls {
			if !sendEvent(conn.Ctx, conn.ResponseChan, &types.ChatResponse{Response: fmt.Sprintf("\n[executing tool: %s - %s]\n",
				call.Function.Name, call.Function.Arguments), Kind: types.ResponseKindToolStatus}) {
				return
			}
			result, err := o.execution.executeToolCall(conn.Ctx, call)
			if conn.Ctx.Err() != nil {
				return
			}
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

	sendEvent(conn.Ctx, conn.ErrorChan, fmt.Errorf("maximum tool call rounds exceeded"))
}

func (o *OpenRouter) sendChat(conn *types.BusConnector,
	model string,
	messages []openRouterMessage,
	tools []config.ToolConfig,
) (openRouterMessage, []types.ToolCall, error) {
	reqBody := openRouterRequest{
		Model:    model,
		Messages: messages,
		Tools:    ollamaTools(tools),
		Stream:   true,
	}

	data, err := json.Marshal(&reqBody)
	if err != nil {
		return openRouterMessage{}, nil, err
	}

	req, err := http.NewRequestWithContext(conn.Ctx, "POST",
		o.url, bytes.NewReader(data))
	if err != nil {
		return openRouterMessage{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	resp, err := openRouterStreamClient.Do(req)
	if err != nil {
		return openRouterMessage{}, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return openRouterMessage{}, nil, fmt.Errorf("OpenRouter returned status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	// TODO(review): 1 MB scanner cap silently drops lines larger than the buffer;
	// a large reasoning/tool-call delta can exceed it. See notes/review-checklist.md.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var assistant strings.Builder
	var thinking strings.Builder
	builders := map[int]*openRouterToolCallBuilder{}

	for scanner.Scan() {
		if conn.Ctx.Err() != nil {
			return openRouterMessage{}, nil, conn.Ctx.Err()
		}
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
			return openRouterMessage{
				Role: "assistant", Content: assistant.String(),
				ToolCalls: toolCalls,
			}, calls, nil
		}

		var chunk openRouterStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// TODO(review): aborting the whole turn on any unparseable data: payload diverges
			// from Ollama (which skips). Pick one policy across providers. See notes/review-checklist.md.
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
			if !sendEvent(conn.Ctx, conn.ResponseChan, &types.ChatResponse{
				Response: delta.Content,
				Thinking: thinkingText, Done: false,
				Kind:    types.ResponseKindAssistant,
				Context: conn.Request.Context,
			}) {
				return openRouterMessage{}, nil, conn.Ctx.Err()
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return openRouterMessage{}, nil, err
	}

	toolCalls, calls, err := buildOpenRouterToolCalls(builders)
	if err != nil {
		return openRouterMessage{}, nil, err
	}
	return openRouterMessage{
		Role: "assistant", Content: assistant.String(),
		ToolCalls: toolCalls,
	}, calls, nil
}

type openRouterToolCallBuilder struct {
	id        string
	callType  string
	name      string
	arguments strings.Builder
}

func buildOpenRouterToolCalls(
	builders map[int]*openRouterToolCallBuilder,
) ([]openRouterToolCall, []types.ToolCall, error) {
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
	o.modelMu.Lock()
	defer o.modelMu.Unlock()
	o.model = model
}
