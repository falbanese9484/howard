package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

const (
	openRouterChatURL   = "https://openrouter.ai/api/v1/chat/completions"
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
)

type OpenRouter struct {
	url        string
	apiKey     string
	model      string
	context    []openRouterMessage
	modelCache *types.ModelCache
	logger     *logger.Logger
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterRequest struct {
	Model    string              `json:"model"`
	Messages []openRouterMessage `json:"messages"`
	Stream   bool                `json:"stream"`
}

type openRouterStreamResponse struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

type openRouterModelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func NewOpenRouter(log *logger.Logger, apiKey, model string, cache *types.ModelCache) (*OpenRouter, error) {
	if apiKey == "" {
		return nil, errors.New("OpenRouter API key required")
	}
	return &OpenRouter{
		url:        openRouterChatURL,
		apiKey:     apiKey,
		model:      model,
		context:    []openRouterMessage{},
		modelCache: cache,
		logger:     log,
	}, nil
}

func (o *OpenRouter) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{
		Model:  o.model,
		Prompt: prompt,
		Stream: true,
	}
}

func (o *OpenRouter) Chat(conn *types.BusConnector) {
	messages := append(o.context, openRouterMessage{Role: "user", Content: conn.Request.Prompt})

	reqBody := openRouterRequest{
		Model:    o.model,
		Messages: messages,
		Stream:   true,
	}

	data, err := json.Marshal(&reqBody)
	if err != nil {
		conn.ErrorChan <- err
		return
	}

	req, err := http.NewRequestWithContext(conn.Ctx, "POST", o.url, bytes.NewReader(data))
	if err != nil {
		conn.ErrorChan <- err
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.apiKey)

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		conn.ErrorChan <- err
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		conn.ErrorChan <- fmt.Errorf("OpenRouter returned status %d", resp.StatusCode)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	var assistant strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := line[6:]
		if data == "[DONE]" {
			o.context = append(o.context, messages...)
			o.context = append(o.context, openRouterMessage{Role: "assistant", Content: assistant.String()})
			conn.DoneChannel <- true
			return
		}

		var chunk openRouterStreamResponse
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			conn.ErrorChan <- err
			return
		}
		if len(chunk.Choices) == 0 {
			conn.DoneChannel <- true
			return
		}

		text := chunk.Choices[0].Delta.Content
		assistant.WriteString(text)
		conn.ResponseChan <- &types.ChatResponse{Response: text}
	}

	if err := scanner.Err(); err != nil {
		conn.ErrorChan <- err
	}
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
