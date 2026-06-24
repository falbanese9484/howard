package provider

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

const (
	bedrockDefaultRegion    = "us-east-1"
	bedrockDefaultMaxTokens = 4096
	// bedrockMaxEventMessage bounds a single event stream frame to guard against
	// a corrupt length prefix triggering a huge allocation.
	bedrockMaxEventMessage = 16 * 1024 * 1024
)

// bedrockStreamClient mirrors openRouterStreamClient: it bounds connection
// setup and time-to-first-byte but deliberately has no overall Timeout, since a
// total timeout would cut off long streaming responses. Cancellation flows
// through the request context.
var bedrockStreamClient = &http.Client{
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

// Bedrock talks to the Amazon Bedrock runtime via the public Converse API using
// a long-term Bedrock API key (a bearer token). It implements types.Provider by
// mirroring the OpenRouter provider: it keeps a native message history, runs an
// agentic tool loop in Chat, and streams incremental responses onto the bus.
//
// The Converse API takes the system prompt as a top-level field rather than a
// message, so it is stored separately and replayed on every request.
type Bedrock struct {
	region    string
	apiKey    string
	model     string
	maxTokens int

	system          string
	messages        []bedrockMessage
	initialMessages []bedrockMessage
	tools           []config.ToolConfig

	modelCache *types.ModelCache
	logger     *logger.Logger
	httpClient *http.Client

	// runtimeEndpoint and controlEndpoint override the computed regional
	// endpoints. They are only set in tests; in normal use they are empty and
	// the regional hostnames are derived from region.
	runtimeEndpoint string
	controlEndpoint string
}

type bedrockMessage struct {
	Role    string                `json:"role"`
	Content []bedrockContentBlock `json:"content"`
}

// bedrockContentBlock is a Converse content block. Exactly one field is set per
// block; the pointer + omitempty combination keeps the unused members out of
// the JSON the API would otherwise reject.
type bedrockContentBlock struct {
	Text       *string            `json:"text,omitempty"`
	ToolUse    *bedrockToolUse    `json:"toolUse,omitempty"`
	ToolResult *bedrockToolResult `json:"toolResult,omitempty"`
}

type bedrockToolUse struct {
	ToolUseID string         `json:"toolUseId"`
	Name      string         `json:"name"`
	Input     map[string]any `json:"input"`
}

type bedrockToolResult struct {
	ToolUseID string                     `json:"toolUseId"`
	Content   []bedrockToolResultContent `json:"content"`
	Status    string                     `json:"status,omitempty"`
}

type bedrockToolResultContent struct {
	Text string `json:"text,omitempty"`
}

type bedrockConverseRequest struct {
	Messages        []bedrockMessage        `json:"messages"`
	System          []bedrockSystemBlock    `json:"system,omitempty"`
	ToolConfig      *bedrockToolConfig      `json:"toolConfig,omitempty"`
	InferenceConfig *bedrockInferenceConfig `json:"inferenceConfig,omitempty"`
}

type bedrockSystemBlock struct {
	Text string `json:"text"`
}

type bedrockInferenceConfig struct {
	MaxTokens int `json:"maxTokens,omitempty"`
}

type bedrockToolConfig struct {
	Tools []bedrockTool `json:"tools"`
}

type bedrockTool struct {
	ToolSpec bedrockToolSpec `json:"toolSpec"`
}

type bedrockToolSpec struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema bedrockToolInputSchema `json:"inputSchema"`
}

type bedrockToolInputSchema struct {
	JSON bedrockJSONSchema `json:"json"`
}

type bedrockJSONSchema struct {
	Type       string                           `json:"type"`
	Properties map[string]bedrockSchemaProperty `json:"properties"`
	Required   []string                         `json:"required,omitempty"`
}

type bedrockSchemaProperty struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
}

func NewBedrock(
	log *logger.Logger,
	cfg *config.AgentConfig,
	cache *types.ModelCache,
) (*Bedrock, error) {
	apiKey := strings.TrimSpace(cfg.ApiKey)
	if apiKey == "" {
		apiKey = strings.TrimSpace(os.Getenv("AWS_BEARER_TOKEN_BEDROCK"))
	}
	if apiKey == "" {
		return nil, errors.New("Bedrock API key required: run 'howard auth bedrock' or set AWS_BEARER_TOKEN_BEDROCK")
	}

	region := ""
	maxTokens := 0
	if cfg.Bedrock != nil {
		region = strings.TrimSpace(cfg.Bedrock.Region)
		maxTokens = cfg.Bedrock.MaxTokens
	}
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_REGION"))
	}
	if region == "" {
		region = strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION"))
	}
	if region == "" {
		region = bedrockDefaultRegion
	}
	if maxTokens <= 0 {
		maxTokens = bedrockDefaultMaxTokens
	}

	tools := cfg.Tools
	if len(tools) > 0 && log != nil {
		log.Info("loaded tools config", "tools", len(tools))
	}

	return &Bedrock{
		region:          region,
		apiKey:          apiKey,
		model:           cfg.Model,
		maxTokens:       maxTokens,
		system:          config.BuildSystemPrompt(cfg.SystemPrompt, tools),
		messages:        []bedrockMessage{},
		initialMessages: []bedrockMessage{},
		tools:           tools,
		modelCache:      cache,
		logger:          log,
		httpClient:      bedrockStreamClient,
	}, nil
}

func (b *Bedrock) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{
		Model:    b.model,
		Messages: []types.Message{{Role: "user", Content: prompt}},
		Tools:    b.tools,
		Stream:   true,
	}
}

func (b *Bedrock) Chat(conn *types.BusConnector) {
	newMessages := bedrockMessagesFromTypes(conn.Request.Messages)
	if len(newMessages) == 0 && conn.Request.Prompt != "" {
		newMessages = []bedrockMessage{{
			Role:    "user",
			Content: []bedrockContentBlock{{Text: strPtr(conn.Request.Prompt)}},
		}}
	}

	messages := append([]bedrockMessage{}, b.messages...)
	messages = append(messages, newMessages...)

	tools := conn.Request.Tools
	if len(tools) == 0 {
		tools = b.tools
	}

	const maxToolRounds = 100
	for range maxToolRounds {
		assistantMsg, calls, err := b.sendChat(conn, messages, tools)
		if err != nil {
			conn.ErrorChan <- err
			return
		}

		messages = append(messages, assistantMsg)
		if len(calls) == 0 {
			b.messages = messages
			conn.DoneChannel <- true
			return
		}

		resultBlocks := make([]bedrockContentBlock, 0, len(calls))
		for _, call := range calls {
			conn.ResponseChan <- &types.ChatResponse{
				Response: fmt.Sprintf("\n[executing tool: %s - %v]\n",
					call.Function.Name, call.Function.Arguments),
				Kind: types.ResponseKindToolStatus,
			}
			result, err := executeToolCall(call)
			status := "success"
			if err != nil {
				result = "tool error: " + err.Error()
				status = "error"
			}
			if strings.TrimSpace(result) == "" {
				// Bedrock rejects an empty toolResult text block.
				result = "(no output)"
			}
			resultBlocks = append(resultBlocks, bedrockContentBlock{
				ToolResult: &bedrockToolResult{
					ToolUseID: call.ID,
					Content:   []bedrockToolResultContent{{Text: result}},
					Status:    status,
				},
			})
		}
		messages = append(messages, bedrockMessage{Role: "user", Content: resultBlocks})
	}

	conn.ErrorChan <- fmt.Errorf("maximum tool call rounds exceeded")
}

func (b *Bedrock) sendChat(
	conn *types.BusConnector,
	messages []bedrockMessage,
	tools []config.ToolConfig,
) (bedrockMessage, []types.ToolCall, error) {
	reqBody := bedrockConverseRequest{
		Messages:        messages,
		ToolConfig:      bedrockToolConfigFromTools(tools),
		InferenceConfig: &bedrockInferenceConfig{MaxTokens: b.maxTokens},
	}
	if b.system != "" {
		reqBody.System = []bedrockSystemBlock{{Text: b.system}}
	}

	data, err := json.Marshal(&reqBody)
	if err != nil {
		return bedrockMessage{}, nil, err
	}

	endpoint := b.runtimeBaseURL() + "/model/" + url.PathEscape(b.model) + "/converse-stream"
	req, err := http.NewRequestWithContext(conn.Ctx, "POST", endpoint, bytes.NewReader(data))
	if err != nil {
		return bedrockMessage{}, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/vnd.amazon.eventstream")
	req.Header.Set("Authorization", "Bearer "+b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		return bedrockMessage{}, nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return bedrockMessage{}, nil, fmt.Errorf("Bedrock returned status %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var assistant strings.Builder
	builders := map[int]*bedrockToolUseBuilder{}

	for {
		event, err := readBedrockEvent(resp.Body)
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return bedrockMessage{}, nil, err
		}

		if event.headers[":message-type"] == "exception" || event.headers[":exception-type"] != "" {
			name := event.headers[":exception-type"]
			if name == "" {
				name = "exception"
			}
			return bedrockMessage{}, nil, fmt.Errorf("Bedrock stream %s: %s",
				name, bedrockPayloadMessage(event.payload))
		}

		switch event.headers[":event-type"] {
		case "contentBlockStart":
			var p bedrockBlockStartEvent
			if err := json.Unmarshal(event.payload, &p); err != nil {
				return bedrockMessage{}, nil, err
			}
			if p.Start.ToolUse != nil {
				builders[p.ContentBlockIndex] = &bedrockToolUseBuilder{
					id:   p.Start.ToolUse.ToolUseID,
					name: p.Start.ToolUse.Name,
				}
			}
		case "contentBlockDelta":
			var p bedrockBlockDeltaEvent
			if err := json.Unmarshal(event.payload, &p); err != nil {
				return bedrockMessage{}, nil, err
			}
			if p.Delta.ToolUse != nil {
				bld := builders[p.ContentBlockIndex]
				if bld == nil {
					bld = &bedrockToolUseBuilder{}
					builders[p.ContentBlockIndex] = bld
				}
				bld.input.WriteString(p.Delta.ToolUse.Input)
			}
			thinking := ""
			if p.Delta.ReasoningContent != nil {
				thinking = p.Delta.ReasoningContent.Text
			}
			if p.Delta.Text != "" {
				assistant.WriteString(p.Delta.Text)
			}
			if p.Delta.Text != "" || thinking != "" {
				conn.ResponseChan <- &types.ChatResponse{
					Response: p.Delta.Text,
					Thinking: thinking,
					Done:     false,
					Kind:     types.ResponseKindAssistant,
				}
			}
		case "messageStop", "messageStart", "contentBlockStop", "metadata":
			// No action: tool calls are detected from accumulated tool-use
			// blocks rather than the stop reason, and metadata is not surfaced.
		}
	}

	toolBlocks, calls, err := buildBedrockToolCalls(builders)
	if err != nil {
		return bedrockMessage{}, nil, err
	}

	content := make([]bedrockContentBlock, 0, len(toolBlocks)+1)
	if text := assistant.String(); text != "" {
		content = append(content, bedrockContentBlock{Text: strPtr(text)})
	}
	content = append(content, toolBlocks...)

	return bedrockMessage{Role: "assistant", Content: content}, calls, nil
}

// bedrockBlockStartEvent is the payload of a contentBlockStart event.
type bedrockBlockStartEvent struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
	Start             struct {
		ToolUse *struct {
			ToolUseID string `json:"toolUseId"`
			Name      string `json:"name"`
		} `json:"toolUse"`
	} `json:"start"`
}

// bedrockBlockDeltaEvent is the payload of a contentBlockDelta event.
type bedrockBlockDeltaEvent struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
	Delta             struct {
		Text    string `json:"text"`
		ToolUse *struct {
			Input string `json:"input"`
		} `json:"toolUse"`
		ReasoningContent *struct {
			Text string `json:"text"`
		} `json:"reasoningContent"`
	} `json:"delta"`
}

type bedrockToolUseBuilder struct {
	id    string
	name  string
	input strings.Builder
}

func buildBedrockToolCalls(
	builders map[int]*bedrockToolUseBuilder,
) ([]bedrockContentBlock, []types.ToolCall, error) {
	if len(builders) == 0 {
		return nil, nil, nil
	}

	indexes := make([]int, 0, len(builders))
	for index := range builders {
		indexes = append(indexes, index)
	}
	sort.Ints(indexes)

	blocks := make([]bedrockContentBlock, 0, len(indexes))
	calls := make([]types.ToolCall, 0, len(indexes))
	for _, index := range indexes {
		bld := builders[index]
		raw := bld.input.String()
		var args map[string]any
		if strings.TrimSpace(raw) == "" {
			args = map[string]any{}
		} else if err := json.Unmarshal([]byte(raw), &args); err != nil {
			return nil, nil, fmt.Errorf("decode tool input for %s: %w", bld.name, err)
		}

		blocks = append(blocks, bedrockContentBlock{
			ToolUse: &bedrockToolUse{
				ToolUseID: bld.id,
				Name:      bld.name,
				Input:     args,
			},
		})
		calls = append(calls, types.ToolCall{
			ID:   bld.id,
			Type: "function",
			Function: types.ToolCallFunction{
				Name:      bld.name,
				Arguments: args,
			},
		})
	}
	return blocks, calls, nil
}

func bedrockMessagesFromTypes(messages []types.Message) []bedrockMessage {
	converted := make([]bedrockMessage, 0, len(messages))
	for _, msg := range messages {
		if msg.Content == "" {
			continue
		}
		converted = append(converted, bedrockMessage{
			Role:    bedrockRole(msg.Role),
			Content: []bedrockContentBlock{{Text: strPtr(msg.Content)}},
		})
	}
	return converted
}

// bedrockRole maps a generic role to one Bedrock accepts. Converse only allows
// "user" and "assistant"; anything else (e.g. a stray system/tool role) is
// treated as user input.
func bedrockRole(role string) string {
	if role == "assistant" {
		return "assistant"
	}
	return "user"
}

func bedrockToolConfigFromTools(tools []config.ToolConfig) *bedrockToolConfig {
	if len(tools) == 0 {
		return nil
	}

	converted := make([]bedrockTool, 0, len(tools))
	for _, tool := range tools {
		properties := make(map[string]bedrockSchemaProperty, len(tool.Parameters))
		required := make([]string, 0, len(tool.Parameters))
		for _, param := range tool.Parameters {
			properties[param.ParameterName] = bedrockSchemaProperty{
				Type:        "string",
				Description: param.Description,
			}
			required = append(required, param.ParameterName)
		}
		converted = append(converted, bedrockTool{
			ToolSpec: bedrockToolSpec{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: bedrockToolInputSchema{
					JSON: bedrockJSONSchema{
						Type:       "object",
						Properties: properties,
						Required:   required,
					},
				},
			},
		})
	}
	return &bedrockToolConfig{Tools: converted}
}

func (b *Bedrock) RetrieveModels() ([]types.Model, error) {
	if !b.modelCache.IsStale() {
		return b.modelCache.Load(), nil
	}

	endpoint := b.controlBaseURL() + "/foundation-models"
	req, err := http.NewRequest(http.MethodGet, endpoint, nil)
	if err != nil {
		return b.fallbackModels(), nil
	}
	req.Header.Set("Authorization", "Bearer "+b.apiKey)

	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.logModelError(err)
		return b.fallbackModels(), nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		b.logModelError(fmt.Errorf("status %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
		return b.fallbackModels(), nil
	}

	var result struct {
		ModelSummaries []struct {
			ModelID string `json:"modelId"`
		} `json:"modelSummaries"`
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return b.fallbackModels(), nil
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return b.fallbackModels(), nil
	}

	models := make([]types.Model, 0, len(result.ModelSummaries))
	for _, m := range result.ModelSummaries {
		if m.ModelID == "" {
			continue
		}
		models = append(models, types.Model{Name: m.ModelID})
	}
	if len(models) == 0 {
		return b.fallbackModels(), nil
	}

	b.modelCache.Store(models)
	return models, nil
}

// fallbackModels returns the configured model so the model selector is never
// empty when ListFoundationModels is unavailable (for example, when the API key
// lacks bedrock:ListFoundationModels permission).
func (b *Bedrock) fallbackModels() []types.Model {
	if strings.TrimSpace(b.model) == "" {
		return nil
	}
	return []types.Model{{Name: b.model}}
}

func (b *Bedrock) logModelError(err error) {
	if b.logger != nil {
		b.logger.Info("bedrock list models failed, using fallback", "error", err)
	}
}

func (b *Bedrock) SetModel(model string) {
	b.model = model
}

// Reset discards the conversation history. The system prompt is held separately
// and replayed on every request, so the initial history is empty.
func (b *Bedrock) Reset() {
	b.messages = append([]bedrockMessage{}, b.initialMessages...)
}

func (b *Bedrock) runtimeBaseURL() string {
	if b.runtimeEndpoint != "" {
		return b.runtimeEndpoint
	}
	return fmt.Sprintf("https://bedrock-runtime.%s.amazonaws.com", b.region)
}

func (b *Bedrock) controlBaseURL() string {
	if b.controlEndpoint != "" {
		return b.controlEndpoint
	}
	return fmt.Sprintf("https://bedrock.%s.amazonaws.com", b.region)
}

// bedrockEventMessage is one decoded AWS event stream
// (application/vnd.amazon.eventstream) frame.
type bedrockEventMessage struct {
	headers map[string]string
	payload []byte
}

// readBedrockEvent reads and decodes a single event stream frame. The wire
// format per message is:
//
//	[total length: 4][headers length: 4][prelude CRC: 4]
//	[headers: headers length][payload: rest][message CRC: 4]
//
// All integers are big-endian. CRCs are not validated. io.EOF is returned at a
// clean message boundary when the stream ends.
func readBedrockEvent(r io.Reader) (*bedrockEventMessage, error) {
	var prelude [12]byte
	if _, err := io.ReadFull(r, prelude[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, io.EOF
		}
		return nil, err
	}

	totalLen := binary.BigEndian.Uint32(prelude[0:4])
	headersLen := binary.BigEndian.Uint32(prelude[4:8])
	if totalLen < 16 || totalLen > bedrockMaxEventMessage {
		return nil, fmt.Errorf("invalid event stream message length %d", totalLen)
	}
	if headersLen > totalLen-16 {
		return nil, fmt.Errorf("invalid event stream headers length %d", headersLen)
	}

	// Remaining bytes after the 12-byte prelude: headers + payload + 4-byte CRC.
	remaining := make([]byte, totalLen-12)
	if _, err := io.ReadFull(r, remaining); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	headerBytes := remaining[:headersLen]
	payload := remaining[headersLen : len(remaining)-4]

	headers, err := parseBedrockHeaders(headerBytes)
	if err != nil {
		return nil, err
	}
	return &bedrockEventMessage{headers: headers, payload: payload}, nil
}

// parseBedrockHeaders decodes the event stream header block. Only string-valued
// headers (the ones Bedrock uses for routing: :event-type, :message-type,
// :exception-type, :content-type) are retained; other value types are skipped
// over so the parser stays aligned.
func parseBedrockHeaders(b []byte) (map[string]string, error) {
	headers := map[string]string{}
	i := 0
	for i < len(b) {
		nameLen := int(b[i])
		i++
		if i+nameLen > len(b) {
			return nil, errors.New("truncated event stream header name")
		}
		name := string(b[i : i+nameLen])
		i += nameLen
		if i >= len(b) {
			return nil, errors.New("truncated event stream header type")
		}
		valueType := b[i]
		i++

		switch valueType {
		case 0, 1: // bool true / false: no value bytes
		case 2: // byte
			i++
		case 3: // short
			i += 2
		case 4: // integer
			i += 4
		case 5, 8: // long, timestamp
			i += 8
		case 6, 7: // byte array, string: 2-byte length prefix
			if i+2 > len(b) {
				return nil, errors.New("truncated event stream header value length")
			}
			valLen := int(binary.BigEndian.Uint16(b[i:]))
			i += 2
			if i+valLen > len(b) {
				return nil, errors.New("truncated event stream header value")
			}
			if valueType == 7 {
				headers[name] = string(b[i : i+valLen])
			}
			i += valLen
		case 9: // uuid
			i += 16
		default:
			return nil, fmt.Errorf("unknown event stream header value type %d", valueType)
		}
	}
	return headers, nil
}

// bedrockPayloadMessage extracts a human-readable message from an exception
// event payload, falling back to the raw payload.
func bedrockPayloadMessage(payload []byte) string {
	var p struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(payload, &p); err == nil && p.Message != "" {
		return p.Message
	}
	return strings.TrimSpace(string(payload))
}

func strPtr(s string) *string {
	return &s
}
