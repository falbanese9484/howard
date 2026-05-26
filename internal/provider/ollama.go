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
)

const (
	ollamaChatURL = "http://localhost:11434/api/chat"
	ollamaTagsURL = "http://localhost:11434/api/tags"
)

type Ollama struct {
	url        string
	model      string
	messages   []types.Message
	tools      []types.Tool
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

func NewOllama(log *logger.Logger, model string, cache *types.ModelCache) *Ollama {
	tools, err := loadToolsConfig()
	if err != nil && log != nil {
		log.Warn("failed to load tools config", "error", err)
	}
	if len(tools) > 0 && log != nil {
		log.Info("loaded tools config", "tools", len(tools))
	}

	messages := []types.Message{}
	systemPrompt, err := loadSystemPrompt()
	if err != nil && log != nil {
		log.Warn("failed to load system prompt", "error", err)
	}
	if systemPrompt != "" {
		messages = append(messages, types.Message{Role: "system", Content: systemPrompt})
	}

	return &Ollama{
		url:        ollamaChatURL,
		model:      model,
		messages:   messages,
		tools:      tools,
		modelCache: cache,
		logger:     log,
	}
}

func (o *Ollama) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{
		Model:    o.model,
		Messages: []types.Message{{Role: "user", Content: prompt}},
		Tools:    o.tools,
		Stream:   true,
	}
}

func (o *Ollama) Chat(conn *types.BusConnector) {
	newMessages := conn.Request.Messages
	if len(newMessages) == 0 && conn.Request.Prompt != "" {
		newMessages = []types.Message{{Role: "user", Content: conn.Request.Prompt}}
	}

	messages := append([]types.Message{}, o.messages...)
	messages = append(messages, newMessages...)

	tools := conn.Request.Tools
	if len(tools) == 0 {
		tools = o.tools
	}

	const maxToolRounds = 100
	for round := 0; round < maxToolRounds; round++ {
		assistantMsg, err := o.sendChat(conn, messages, tools)
		if err != nil {
			conn.ErrorChan <- err
			return
		}

		messages = append(messages, assistantMsg)
		if len(assistantMsg.ToolCalls) == 0 {
			o.messages = messages
			conn.DoneChannel <- true
			return
		}

		for _, call := range assistantMsg.ToolCalls {
			conn.ResponseChan <- &types.ChatResponse{Response: fmt.Sprintf("\n[executing tool: %s]\n", call.Function.Name)}
			result, err := executeToolCall(call)
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

	conn.ErrorChan <- fmt.Errorf("maximum tool call rounds exceeded")
}

func (o *Ollama) sendChat(conn *types.BusConnector, messages []types.Message, tools []types.Tool) (types.Message, error) {
	reqBody := ollamaChatRequest{
		Model:    o.model,
		Messages: messages,
		Tools:    ollamaTools(tools),
		Stream:   true,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return types.Message{}, err
	}

	req, err := http.NewRequestWithContext(conn.Ctx, "POST", o.url, bytes.NewReader(data))
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
		return types.Message{}, fmt.Errorf("Ollama returned status %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var assistant strings.Builder
	var thinking strings.Builder
	var toolCalls []types.ToolCall

	for scanner.Scan() {
		var chunk ollamaChatResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}

		assistant.WriteString(chunk.Message.Content)
		thinking.WriteString(chunk.Message.Thinking)
		if len(chunk.Message.ToolCalls) > 0 {
			toolCalls = append(toolCalls, chunk.Message.ToolCalls...)
		}
		if chunk.Message.Content != "" || chunk.Message.Thinking != "" || len(chunk.Message.ToolCalls) > 0 {
			conn.ResponseChan <- &types.ChatResponse{
				Response:  chunk.Message.Content,
				Thinking:  chunk.Message.Thinking,
				ToolCalls: chunk.Message.ToolCalls,
			}
		}
		if chunk.Done {
			return types.Message{Role: "assistant", Content: assistant.String(), Thinking: thinking.String(), ToolCalls: toolCalls}, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return types.Message{}, err
	}
	return types.Message{Role: "assistant", Content: assistant.String(), Thinking: thinking.String(), ToolCalls: toolCalls}, nil
}

func ollamaTools(tools []types.Tool) []ollamaTool {
	if len(tools) == 0 {
		return nil
	}

	converted := make([]ollamaTool, 0, len(tools))
	for _, tool := range tools {
		properties := make(map[string]ollamaToolProperty, len(tool.Parameters))
		required := make([]string, 0, len(tool.Parameters))
		for _, param := range tool.Parameters {
			properties[param.Param] = ollamaToolProperty{
				Type:        "string",
				Description: param.Description,
			}
			required = append(required, param.Param)
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
	o.model = model
}
