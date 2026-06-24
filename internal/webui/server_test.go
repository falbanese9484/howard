package webui

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

// mockProvider is a controllable types.Provider for handler tests.
type mockProvider struct {
	mu         sync.Mutex
	model      string
	resetCalls int
	chatFunc   func(c *types.BusConnector)
}

func (m *mockProvider) Chat(c *types.BusConnector) {
	if m.chatFunc != nil {
		m.chatFunc(c)
		return
	}
	c.ResponseChan <- &types.ChatResponse{Response: "hello ", Kind: types.ResponseKindAssistant}
	c.ResponseChan <- &types.ChatResponse{Thinking: "pondering", Kind: types.ResponseKindAssistant}
	c.ResponseChan <- &types.ChatResponse{Response: "world", Kind: types.ResponseKindAssistant}
	c.DoneChannel <- true
}

func (m *mockProvider) GenerateRequest(prompt string) *types.ChatRequest {
	return &types.ChatRequest{Prompt: prompt}
}

func (m *mockProvider) RetrieveModels() ([]types.Model, error) {
	return []types.Model{{Name: "model-a"}, {Name: "model-b"}}, nil
}

func (m *mockProvider) SetModel(model string) {
	m.mu.Lock()
	m.model = model
	m.mu.Unlock()
}

func (m *mockProvider) Reset() {
	m.mu.Lock()
	m.resetCalls++
	m.mu.Unlock()
}

func (m *mockProvider) currentModel() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.model
}

func (m *mockProvider) resets() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.resetCalls
}

func newTestServer(t *testing.T, mock *mockProvider) (*httptest.Server, *Server) {
	t.Helper()
	log, err := logger.New(logger.Config{
		FilePath: filepath.Join(t.TempDir(), "harness.log"),
		FileOnly: true,
	})
	if err != nil {
		t.Fatalf("logger.New() error = %v", err)
	}
	srv := New(log, mock, "model-x")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv
}

func TestIndexServesShell(t *testing.T) {
	ts, _ := newTestServer(t, &mockProvider{model: "model-x"})

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET / error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "howard") {
		t.Fatalf("index body missing app shell: %q", string(body))
	}
}

func TestModelsEndpoint(t *testing.T) {
	ts, _ := newTestServer(t, &mockProvider{model: "model-x"})

	resp, err := http.Get(ts.URL + "/api/models")
	if err != nil {
		t.Fatalf("GET /api/models error = %v", err)
	}
	defer resp.Body.Close()

	var got struct {
		Models  []string `json:"models"`
		Current string   `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if got.Current != "model-x" {
		t.Fatalf("current = %q, want model-x", got.Current)
	}
	if len(got.Models) != 2 || got.Models[0] != "model-a" || got.Models[1] != "model-b" {
		t.Fatalf("models = %v, want [model-a model-b]", got.Models)
	}
}

func TestChatStreamsSSE(t *testing.T) {
	ts, _ := newTestServer(t, &mockProvider{model: "model-x"})

	resp, err := http.Post(ts.URL+"/api/chat", "application/json", strings.NewReader(`{"prompt":"hi"}`))
	if err != nil {
		t.Fatalf("POST /api/chat error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	s := string(body)
	for _, want := range []string{`"response":"hello "`, `"response":"world"`, `"thinking":"pondering"`, `"done":true`} {
		if !strings.Contains(s, want) {
			t.Fatalf("stream %q missing %q", s, want)
		}
	}
	// Every event must use SSE framing.
	if !strings.HasPrefix(s, "data: ") {
		t.Fatalf("stream not SSE-framed: %q", s)
	}
}

func TestChatRequiresJSONContentType(t *testing.T) {
	ts, _ := newTestServer(t, &mockProvider{model: "model-x"})

	resp, err := http.Post(ts.URL+"/api/chat", "text/plain", strings.NewReader(`{"prompt":"hi"}`))
	if err != nil {
		t.Fatalf("POST /api/chat error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415", resp.StatusCode)
	}
}

func TestChatEmptyPrompt(t *testing.T) {
	ts, _ := newTestServer(t, &mockProvider{model: "model-x"})

	resp, err := http.Post(ts.URL+"/api/chat", "application/json", strings.NewReader(`{"prompt":"   "}`))
	if err != nil {
		t.Fatalf("POST /api/chat error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestSetModel(t *testing.T) {
	mock := &mockProvider{model: "model-x"}
	ts, srv := newTestServer(t, mock)

	resp, err := http.Post(ts.URL+"/api/model", "application/json", strings.NewReader(`{"model":"model-b"}`))
	if err != nil {
		t.Fatalf("POST /api/model error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if mock.currentModel() != "model-b" {
		t.Fatalf("provider model = %q, want model-b", mock.currentModel())
	}
	if srv.currentModel() != "model-b" {
		t.Fatalf("server model = %q, want model-b", srv.currentModel())
	}
}

func TestReset(t *testing.T) {
	mock := &mockProvider{model: "model-x"}
	ts, _ := newTestServer(t, mock)

	resp, err := http.Post(ts.URL+"/api/reset", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/reset error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if mock.resets() != 1 {
		t.Fatalf("reset calls = %d, want 1", mock.resets())
	}
}

func TestCancelIdle(t *testing.T) {
	ts, _ := newTestServer(t, &mockProvider{model: "model-x"})

	resp, err := http.Post(ts.URL+"/api/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/cancel error = %v", err)
	}
	defer resp.Body.Close()

	var got struct {
		Cancelled bool `json:"cancelled"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode error = %v", err)
	}
	if got.Cancelled {
		t.Fatalf("cancelled = true, want false when idle")
	}
}

func TestCancelInterruptsActiveTurn(t *testing.T) {
	started := make(chan struct{})
	mock := &mockProvider{
		model: "model-x",
		chatFunc: func(c *types.BusConnector) {
			close(started)
			<-c.Ctx.Done() // block until cancelled
			c.ErrorChan <- c.Ctx.Err()
		},
	}
	ts, _ := newTestServer(t, mock)

	done := make(chan string, 1)
	go func() {
		resp, err := http.Post(ts.URL+"/api/chat", "application/json", strings.NewReader(`{"prompt":"hi"}`))
		if err != nil {
			done <- "post error: " + err.Error()
			return
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		done <- string(body)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("chat turn never started")
	}

	resp, err := http.Post(ts.URL+"/api/cancel", "application/json", nil)
	if err != nil {
		t.Fatalf("POST /api/cancel error = %v", err)
	}
	resp.Body.Close()

	select {
	case body := <-done:
		if !strings.Contains(body, `"done":true`) {
			t.Fatalf("cancelled stream missing terminal done: %q", body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel did not interrupt the active turn")
	}
}
