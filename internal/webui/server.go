// Package webui serves a local, single-user web chat frontend for the agent.
// It reuses the same provider/bus pipeline as the CLI and TUI: a single
// long-lived provider holds conversation state, and turns are serialized so the
// provider's history is never mutated concurrently.
package webui

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/falbanese9484/agent-harness/internal/chat"
	"github.com/falbanese9484/agent-harness/internal/logger"
	"github.com/falbanese9484/agent-harness/internal/types"
)

// Server hosts the web UI and bridges HTTP requests to the agent bus.
type Server struct {
	log       *logger.Logger
	provider  types.Provider
	bus       *chat.Bus
	responses chan *types.ChatResponse
	static    fs.FS

	// chatMu serializes chat turns and resets. The provider holds a single
	// mutable conversation history, so only one turn may run at a time.
	chatMu sync.Mutex

	// stateMu guards lightweight metadata: the active turn's cancel func and
	// the current model name.
	stateMu   sync.Mutex
	cancel    context.CancelFunc
	modelName string
}

// New constructs a Server around an existing provider. It starts the bus pump
// once for the server's lifetime so conversation history persists across turns.
func New(log *logger.Logger, provider types.Provider, modelName string) *Server {
	bus := chat.NewBus(log, provider)
	responses := make(chan *types.ChatResponse, 100)
	go bus.Start(responses)

	return &Server{
		log:       log,
		provider:  provider,
		bus:       bus,
		responses: responses,
		static:    staticFS(),
		modelName: modelName,
	}
}

// Handler returns the HTTP routes for the web UI.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.handleIndex)
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(s.static))))
	mux.HandleFunc("POST /api/chat", s.handleChat)
	mux.HandleFunc("POST /api/cancel", s.handleCancel)
	mux.HandleFunc("POST /api/reset", s.handleReset)
	mux.HandleFunc("GET /api/models", s.handleModels)
	mux.HandleFunc("POST /api/model", s.handleSetModel)
	mux.HandleFunc("GET /api/health", s.handleHealth)
	return mux
}

// Serve binds a TCP listener on addr and serves the UI. addr must be a loopback
// address: the agent can run shell commands and edit files with the user's full
// permissions, so the server must never be exposed to the network. When open is
// true the default browser is launched at the served URL.
func (s *Server) Serve(addr string, open bool, out io.Writer) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	url := "http://" + ln.Addr().String()
	fmt.Fprintf(out, "howard web listening on %s\n", url)
	fmt.Fprintln(out, "Press Ctrl+C to stop.")

	if open {
		go func() {
			// Give http.Serve a moment to start accepting before launching.
			time.Sleep(300 * time.Millisecond)
			if err := openBrowser(url); err != nil {
				s.log.Error("failed to open browser", "error", err)
			}
		}()
	}

	return http.Serve(ln, s.Handler())
}

// setCancel records the cancel func for the active turn.
func (s *Server) setCancel(cancel context.CancelFunc) {
	s.stateMu.Lock()
	s.cancel = cancel
	s.stateMu.Unlock()
}

// clearCancel drops the cancel func once a turn completes.
func (s *Server) clearCancel() {
	s.stateMu.Lock()
	s.cancel = nil
	s.stateMu.Unlock()
}

// invokeCancel aborts the active turn, if any. It deliberately avoids chatMu so
// it can interrupt an in-flight, mutex-holding chat handler without deadlock.
func (s *Server) invokeCancel() bool {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	if s.cancel != nil {
		s.cancel()
		return true
	}
	return false
}

func (s *Server) currentModel() string {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	return s.modelName
}

func (s *Server) setModelName(name string) {
	s.stateMu.Lock()
	s.modelName = name
	s.stateMu.Unlock()
}

// openBrowser launches the OS default browser at url.
func openBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	default:
		name, args = "xdg-open", []string{url}
	}
	return exec.Command(name, args...).Start()
}
