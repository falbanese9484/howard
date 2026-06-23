package webui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/falbanese9484/agent-harness/internal/types"
)

type chatRequest struct {
	Prompt string `json:"prompt"`
}

type modelRequest struct {
	Model string `json:"model"`
}

// handleIndex serves the single-page app shell.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, err := s.static.Open("index.html")
	if err != nil {
		http.Error(w, "index not found", http.StatusInternalServerError)
		return
	}
	defer data.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.Copy(w, data); err != nil {
		s.log.Error("write index", "error", err)
	}
}

// handleChat runs one agent turn and streams ChatResponse events back as SSE.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}

	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	prompt := strings.TrimSpace(req.Prompt)
	if prompt == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Serialize turns: the provider's conversation history is a single mutable
	// slice and is not safe to mutate concurrently.
	s.chatMu.Lock()
	defer s.chatMu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Derive a cancellable context from the request so both the /api/cancel
	// endpoint and a client disconnect abort the provider's HTTP call.
	ctx, cancel := context.WithCancel(r.Context())
	s.setCancel(cancel)
	defer func() {
		cancel()
		s.clearCancel()
	}()

	chatReq := s.provider.GenerateRequest(prompt)
	go s.bus.SendWithContext(ctx, chatReq)

	// Drain responses until Done. A cancelled turn still produces a terminal
	// (error) Done via the provider, so the shared channel is left clean for the
	// next request.
	for resp := range s.responses {
		if resp == nil {
			continue
		}
		writeSSE(w, flusher, resp)
		if resp.Done {
			return
		}
	}
}

// handleCancel aborts the active turn, if any.
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	cancelled := s.invokeCancel()
	writeJSON(w, http.StatusOK, map[string]bool{"cancelled": cancelled})
}

// handleReset clears conversation history back to the system prompt. It waits
// for any in-flight turn to finish via chatMu.
func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	s.chatMu.Lock()
	s.provider.Reset()
	s.chatMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleModels lists available models and reports the current one.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.provider.RetrieveModels()
	if err != nil {
		s.log.Error("retrieve models", "error", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"models":  names,
		"current": s.currentModel(),
	})
}

// handleSetModel switches the active model.
func (s *Server) handleSetModel(w http.ResponseWriter, r *http.Request) {
	if !requireJSON(w, r) {
		return
	}
	var req modelRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Model)
	if name == "" {
		http.Error(w, "model is required", http.StatusBadRequest)
		return
	}
	s.provider.SetModel(name)
	s.setModelName(name)
	writeJSON(w, http.StatusOK, map[string]string{"model": name})
}

// handleHealth reports basic status for the header.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"model": s.currentModel()})
}

// writeSSE encodes a ChatResponse as a single SSE "data:" event. json.Marshal
// escapes newlines, so the payload is always a single line and SSE-safe.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, resp *types.ChatResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "data: %s\n\n", data)
	flusher.Flush()
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// requireJSON enforces an application/json content type on mutating endpoints.
// This is the baseline CSRF defense: a cross-origin JSON POST triggers a CORS
// preflight that this server never answers, and a no-preflight POST (text/plain
// etc.) is rejected here.
func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	if strings.TrimSpace(ct) != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	return true
}
