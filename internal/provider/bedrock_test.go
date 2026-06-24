package provider

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/falbanese9484/agent-harness/internal/types"
	"github.com/falbanese9484/agent-harness/pkg/config"
)

// encodeBedrockFrame builds one AWS event stream
// (application/vnd.amazon.eventstream) message with string-valued headers and a
// JSON payload, computing real prelude/message CRCs so the frame is a faithful
// stand-in for what Bedrock emits.
func encodeBedrockFrame(headers map[string]string, payload []byte) []byte {
	var hb bytes.Buffer
	for name, value := range headers {
		hb.WriteByte(byte(len(name)))
		hb.WriteString(name)
		hb.WriteByte(7) // string value type
		var l [2]byte
		binary.BigEndian.PutUint16(l[:], uint16(len(value)))
		hb.Write(l[:])
		hb.WriteString(value)
	}
	headerBytes := hb.Bytes()

	totalLen := 12 + len(headerBytes) + len(payload) + 4
	var buf bytes.Buffer
	var prelude [12]byte
	binary.BigEndian.PutUint32(prelude[0:4], uint32(totalLen))
	binary.BigEndian.PutUint32(prelude[4:8], uint32(len(headerBytes)))
	binary.BigEndian.PutUint32(prelude[8:12], crc32.ChecksumIEEE(prelude[0:8]))
	buf.Write(prelude[:])
	buf.Write(headerBytes)
	buf.Write(payload)
	var msgCRC [4]byte
	binary.BigEndian.PutUint32(msgCRC[:], crc32.ChecksumIEEE(buf.Bytes()))
	buf.Write(msgCRC[:])
	return buf.Bytes()
}

func bedrockEvent(eventType, payload string) []byte {
	return encodeBedrockFrame(map[string]string{
		":message-type": "event",
		":event-type":   eventType,
		":content-type": "application/json",
	}, []byte(payload))
}

func writeBedrockEvents(w http.ResponseWriter, frames ...[]byte) {
	w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
	for _, f := range frames {
		_, _ = w.Write(f)
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func TestBedrockChatSendsToolResultAndStreamsFinalAnswer(t *testing.T) {
	var requests []bedrockConverseRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req bedrockConverseRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requests = append(requests, req)

		switch len(requests) {
		case 1:
			writeBedrockEvents(w,
				bedrockEvent("messageStart", `{"role":"assistant"}`),
				bedrockEvent("contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"Let me check. "}}`),
				bedrockEvent("contentBlockStart", `{"contentBlockIndex":1,"start":{"toolUse":{"toolUseId":"tool-1","name":"missing_tool"}}}`),
				bedrockEvent("contentBlockDelta", `{"contentBlockIndex":1,"delta":{"toolUse":{"input":"{\"path\":"}}}`),
				bedrockEvent("contentBlockDelta", `{"contentBlockIndex":1,"delta":{"toolUse":{"input":"\"x\"}"}}}`),
				bedrockEvent("contentBlockStop", `{"contentBlockIndex":1}`),
				bedrockEvent("messageStop", `{"stopReason":"tool_use"}`),
			)
		case 2:
			writeBedrockEvents(w,
				bedrockEvent("messageStart", `{"role":"assistant"}`),
				bedrockEvent("contentBlockDelta", `{"contentBlockIndex":0,"delta":{"text":"Final answer after the tool."}}`),
				bedrockEvent("messageStop", `{"stopReason":"end_turn"}`),
			)
		default:
			t.Fatalf("unexpected request %d", len(requests))
		}
	}))
	defer server.Close()

	b := &Bedrock{
		region:          "us-east-1",
		apiKey:          "test-key",
		model:           "test-model",
		maxTokens:       100,
		messages:        []bedrockMessage{},
		initialMessages: []bedrockMessage{},
		modelCache:      types.NewModelCache(3600),
		httpClient:      server.Client(),
		runtimeEndpoint: server.URL,
	}

	responses := make(chan *types.ChatResponse, 100)
	errs := make(chan error, 1)
	done := make(chan bool, 1)

	b.Chat(&types.BusConnector{
		Ctx: context.Background(),
		Request: &types.ChatRequest{
			Messages: []types.Message{{Role: "user", Content: "use a tool"}},
			Stream:   true,
		},
		ResponseChan: responses,
		ErrorChan:    errs,
		DoneChannel:  done,
	})

	select {
	case err := <-errs:
		t.Fatalf("Chat() error = %v", err)
	default:
	}
	select {
	case <-done:
	default:
		t.Fatal("Chat() did not signal done")
	}

	if len(requests) != 2 {
		t.Fatalf("requests = %d, want 2", len(requests))
	}

	second := requests[1].Messages
	if len(second) != 3 {
		t.Fatalf("second request messages = %d, want 3 (user, assistant tool use, tool result)", len(second))
	}
	if second[1].Role != "assistant" {
		t.Fatalf("second message role = %q, want assistant", second[1].Role)
	}
	var toolUse *bedrockToolUse
	for _, block := range second[1].Content {
		if block.ToolUse != nil {
			toolUse = block.ToolUse
		}
	}
	if toolUse == nil {
		t.Fatalf("assistant message missing tool use block: %#v", second[1].Content)
	}
	if toolUse.ToolUseID != "tool-1" || toolUse.Name != "missing_tool" {
		t.Fatalf("tool use = %#v, want id tool-1 name missing_tool", toolUse)
	}
	if got, ok := toolUse.Input["path"]; !ok || got != "x" {
		t.Fatalf("tool use input = %#v, want path=x (accumulated across deltas)", toolUse.Input)
	}
	if second[2].Role != "user" || len(second[2].Content) == 0 || second[2].Content[0].ToolResult == nil {
		t.Fatalf("third message = %#v, want user message with tool result", second[2])
	}
	if second[2].Content[0].ToolResult.ToolUseID != "tool-1" {
		t.Fatalf("tool result id = %q, want tool-1", second[2].Content[0].ToolResult.ToolUseID)
	}

	var final string
	for len(responses) > 0 {
		response := <-responses
		if response.Kind != types.ResponseKindAssistant {
			continue
		}
		final += response.Response
	}
	if final != "Let me check. Final answer after the tool." {
		t.Fatalf("streamed response = %q, want combined assistant text", final)
	}
}

func TestBedrockChatReturnsStreamException(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.amazon.eventstream")
		frame := encodeBedrockFrame(map[string]string{
			":message-type":   "exception",
			":exception-type": "throttlingException",
			":content-type":   "application/json",
		}, []byte(`{"message":"slow down"}`))
		_, _ = w.Write(frame)
	}))
	defer server.Close()

	b := &Bedrock{
		region:          "us-east-1",
		apiKey:          "test-key",
		model:           "test-model",
		maxTokens:       100,
		messages:        []bedrockMessage{},
		initialMessages: []bedrockMessage{},
		modelCache:      types.NewModelCache(3600),
		httpClient:      server.Client(),
		runtimeEndpoint: server.URL,
	}

	responses := make(chan *types.ChatResponse, 10)
	errs := make(chan error, 1)
	done := make(chan bool, 1)

	b.Chat(&types.BusConnector{
		Ctx:          context.Background(),
		Request:      &types.ChatRequest{Prompt: "hi"},
		ResponseChan: responses,
		ErrorChan:    errs,
		DoneChannel:  done,
	})

	select {
	case err := <-errs:
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("throttlingException")) {
			t.Fatalf("error = %v, want throttlingException", err)
		}
		if !bytes.Contains([]byte(err.Error()), []byte("slow down")) {
			t.Fatalf("error = %v, want exception message", err)
		}
	default:
		t.Fatal("Chat() did not report an error")
	}
}

func TestReadBedrockEventRoundTrip(t *testing.T) {
	// Include a non-string header (an integer) to confirm the parser skips it
	// while still capturing string headers and the payload.
	var hb bytes.Buffer
	writeStringHeader(&hb, ":event-type", "contentBlockDelta")
	writeIntHeader(&hb, ":some-int", 42)
	writeStringHeader(&hb, ":content-type", "application/json")
	headerBytes := hb.Bytes()

	payload := []byte(`{"contentBlockIndex":0,"delta":{"text":"hello"}}`)
	totalLen := 12 + len(headerBytes) + len(payload) + 4
	var buf bytes.Buffer
	var prelude [12]byte
	binary.BigEndian.PutUint32(prelude[0:4], uint32(totalLen))
	binary.BigEndian.PutUint32(prelude[4:8], uint32(len(headerBytes)))
	binary.BigEndian.PutUint32(prelude[8:12], crc32.ChecksumIEEE(prelude[0:8]))
	buf.Write(prelude[:])
	buf.Write(headerBytes)
	buf.Write(payload)
	var msgCRC [4]byte
	binary.BigEndian.PutUint32(msgCRC[:], crc32.ChecksumIEEE(buf.Bytes()))
	buf.Write(msgCRC[:])

	event, err := readBedrockEvent(&buf)
	if err != nil {
		t.Fatalf("readBedrockEvent() error = %v", err)
	}
	if event.headers[":event-type"] != "contentBlockDelta" {
		t.Fatalf(":event-type = %q, want contentBlockDelta", event.headers[":event-type"])
	}
	if event.headers[":content-type"] != "application/json" {
		t.Fatalf(":content-type = %q, want application/json", event.headers[":content-type"])
	}
	if string(event.payload) != string(payload) {
		t.Fatalf("payload = %q, want %q", event.payload, payload)
	}
}

func writeStringHeader(buf *bytes.Buffer, name, value string) {
	buf.WriteByte(byte(len(name)))
	buf.WriteString(name)
	buf.WriteByte(7)
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(value)))
	buf.Write(l[:])
	buf.WriteString(value)
}

func writeIntHeader(buf *bytes.Buffer, name string, value int32) {
	buf.WriteByte(byte(len(name)))
	buf.WriteString(name)
	buf.WriteByte(4) // integer value type
	var v [4]byte
	binary.BigEndian.PutUint32(v[:], uint32(value))
	buf.Write(v[:])
}

func TestBedrockToolConfigFromTools(t *testing.T) {
	tools := []config.ToolConfig{{
		Name:        "read_file",
		Description: "read a file",
		Parameters: []config.ParameterConfig{
			{ParameterName: "path", Description: "path to read"},
		},
	}}

	tc := bedrockToolConfigFromTools(tools)
	if tc == nil || len(tc.Tools) != 1 {
		t.Fatalf("tool config = %#v, want one tool", tc)
	}
	spec := tc.Tools[0].ToolSpec
	if spec.Name != "read_file" || spec.Description != "read a file" {
		t.Fatalf("tool spec = %#v, want read_file", spec)
	}
	if spec.InputSchema.JSON.Type != "object" {
		t.Fatalf("schema type = %q, want object", spec.InputSchema.JSON.Type)
	}
	prop, ok := spec.InputSchema.JSON.Properties["path"]
	if !ok || prop.Type != "string" {
		t.Fatalf("schema properties = %#v, want path:string", spec.InputSchema.JSON.Properties)
	}
	if len(spec.InputSchema.JSON.Required) != 1 || spec.InputSchema.JSON.Required[0] != "path" {
		t.Fatalf("required = %#v, want [path]", spec.InputSchema.JSON.Required)
	}

	if bedrockToolConfigFromTools(nil) != nil {
		t.Fatal("expected nil tool config for empty tools")
	}
}

func TestNewBedrockResolvesKeyAndRegion(t *testing.T) {
	cache := types.NewModelCache(3600)

	// Missing key and env unset -> error.
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	if _, err := NewBedrock(nil, &config.AgentConfig{Provider: "bedrock"}, cache); err == nil {
		t.Fatal("expected error when no API key is available")
	}

	// Key from env, region default.
	t.Setenv("AWS_BEARER_TOKEN_BEDROCK", "env-token")
	b, err := NewBedrock(nil, &config.AgentConfig{Provider: "bedrock", Model: "m"}, cache)
	if err != nil {
		t.Fatalf("NewBedrock() error = %v", err)
	}
	if b.apiKey != "env-token" {
		t.Fatalf("apiKey = %q, want env-token", b.apiKey)
	}
	if b.region != bedrockDefaultRegion {
		t.Fatalf("region = %q, want default %q", b.region, bedrockDefaultRegion)
	}
	if b.maxTokens != bedrockDefaultMaxTokens {
		t.Fatalf("maxTokens = %d, want default %d", b.maxTokens, bedrockDefaultMaxTokens)
	}

	// Config values take precedence over env/defaults.
	t.Setenv("AWS_REGION", "us-west-2")
	b, err = NewBedrock(nil, &config.AgentConfig{
		Provider: "bedrock",
		Model:    "m",
		ApiKey:   "cfg-token",
		Bedrock:  &config.BedrockConfig{Region: "eu-central-1", MaxTokens: 2048},
	}, cache)
	if err != nil {
		t.Fatalf("NewBedrock() error = %v", err)
	}
	if b.apiKey != "cfg-token" {
		t.Fatalf("apiKey = %q, want cfg-token", b.apiKey)
	}
	if b.region != "eu-central-1" {
		t.Fatalf("region = %q, want eu-central-1 from config", b.region)
	}
	if b.maxTokens != 2048 {
		t.Fatalf("maxTokens = %d, want 2048 from config", b.maxTokens)
	}
}
