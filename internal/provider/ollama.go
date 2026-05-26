package provider

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

const (
	ollamaGenerateURL = "http://localhost:11434/api/generate"
	ollamaTagsURL     = "http://localhost:11434/api/tags"
)

type Ollama struct {
	url        string
	model      string
	context    []int
	modelCache *types.ModelCache
	logger     *logger.Logger
}

func NewOllama(log *logger.Logger, model string, cache *types.ModelCache) *Ollama {
	return &Ollama{
		url:        ollamaGenerateURL,
		model:      model,
		context:    []int{},
		modelCache: cache,
		logger:     log,
	}
}

func (o *Ollama) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{
		Model:  o.model,
		Prompt: prompt,
		Stream: true,
	}
}

func (o *Ollama) Chat(conn *types.BusConnector) {
	if len(o.context) > 0 {
		conn.Request.Context = o.context
	}

	data, err := json.Marshal(conn.Request)
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

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		conn.ErrorChan <- err
		return
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	if err := scanner.Err(); err != nil {
		conn.ErrorChan <- err
		return
	}
	for scanner.Scan() {
		var chunk types.ChatResponse
		if err := json.Unmarshal(scanner.Bytes(), &chunk); err != nil {
			continue
		}
		if chunk.Response != "" {
			conn.ResponseChan <- &chunk
		}
		if chunk.Done {
			o.context = chunk.Context
			conn.DoneChannel <- true
			return
		}
	}
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
