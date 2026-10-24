package provider

import (
	"errors"
	"fmt"

	"github.com/falbanese9484/agent-harness/pkg/config"
)

// ToolProfile is selected by trusted embedding code, never by model output or
// prompt text. Worker profiles are a closed set; an empty value grants nothing.
type ToolProfile string

const (
	ReadOnly ToolProfile = "read-only"
	// ReadOnlyWeb additionally permits configured web tools. Their existing web
	// enablement, domain authorization and response limits still apply.
	ReadOnlyWeb ToolProfile = "read-only-web"
	legacyCLI   ToolProfile = "legacy-cli"
)

var ErrToolDenied = errors.New("tool denied by execution policy")

func (p ToolProfile) validWorker() bool { return p == ReadOnly || p == ReadOnlyWeb }

func (p ToolProfile) permits(name string) bool {
	switch name {
	case "ls", "read_file":
		return p == ReadOnly || p == ReadOnlyWeb || p == legacyCLI
	case "web_search", "fetch_url":
		return p == ReadOnlyWeb || p == legacyCLI
	case "bash", "mkdir", "write_file", "edit_file":
		return p == legacyCLI
	default:
		return false
	}
}

// allowed is private and populated once from a copied configuration at worker
// construction. A known tool omitted from that configuration is also denied.
func (s *ExecutionState) allows(name string) bool {
	if s == nil || !s.profile.permits(name) {
		return false
	}
	if s.profile == legacyCLI {
		return true
	}
	return s.allowed[name]
}

func (s *ExecutionState) authorizeTool(name string) error {
	if !s.allows(name) {
		return fmt.Errorf("%w: %s", ErrToolDenied, name)
	}
	return nil
}

func (s *ExecutionState) filterTools(tools []config.ToolConfig) []config.ToolConfig {
	if s != nil && s.profile == legacyCLI {
		return cloneTools(tools)
	}
	// Worker definitions and dispatch names come from the same snapshot. Even
	// callers of the lower-level provider constructors cannot desynchronize them
	// by passing a different configuration after constructing the state.
	if s == nil {
		return nil
	}
	return cloneTools(s.tools)
}
