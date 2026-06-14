package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildSystemPromptIncludesHowardIdentity(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{
		Name: "Howard",
		Sections: []SystemPromptSection{
			{
				Title: "Communication style",
				Content: []string{
					"Be warm, casual, dry, direct, and useful.",
				},
			},
		},
	}, nil)

	want := []string{
		"You are Howard, a local coding agent who feels like a sharp coworker the user would actually want to have a beer with.",
		"## Communication style",
		"- Be warm, casual, dry, direct, and useful.",
	}

	for _, text := range want {
		if !strings.Contains(prompt, text) {
			t.Fatalf("BuildSystemPrompt() missing %q in:\n%s", text, prompt)
		}
	}
}

func TestBuildSystemPromptEmptyConfig(t *testing.T) {
	if got := BuildSystemPrompt(SystemPromptConfig{}, nil); got != "" {
		t.Fatalf("BuildSystemPrompt(empty) = %q, want empty", got)
	}
}

func TestBuildSystemPromptIncludesTools(t *testing.T) {
	prompt := BuildSystemPrompt(SystemPromptConfig{Name: "Howard"}, []ToolConfig{
		{
			Name:        "read_file",
			Description: "Read the full contents of a text file in the project.",
			Parameters: []ParameterConfig{
				{
					ParameterName: "path",
					Description:   "File path to read, relative to the project root.",
				},
			},
		},
	})

	want := []string{
		"## Available tools",
		"- read_file: Read the full contents of a text file in the project.",
		"  - path: File path to read, relative to the project root.",
	}

	for _, text := range want {
		if !strings.Contains(prompt, text) {
			t.Fatalf("BuildSystemPrompt() missing %q in:\n%s", text, prompt)
		}
	}
}

func TestMergeToolsReplacesBaseAndAppendsExtensions(t *testing.T) {
	tools := mergeTools(
		[]ToolConfig{
			{Name: "read_file", Description: "base read"},
			{Name: "bash", Description: "base bash"},
		},
		[]ToolConfig{
			{Name: "read_file", Description: "custom read"},
			{Name: "search", Description: "custom search"},
		},
	)

	if len(tools) != 3 {
		t.Fatalf("mergeTools() returned %d tools, want 3", len(tools))
	}

	want := []ToolConfig{
		{Name: "read_file", Description: "custom read"},
		{Name: "bash", Description: "base bash"},
		{Name: "search", Description: "custom search"},
	}
	for i := range want {
		if tools[i].Name != want[i].Name || tools[i].Description != want[i].Description {
			t.Fatalf("mergeTools()[%d] = %#v, want %#v", i, tools[i], want[i])
		}
	}
}

func TestSaveAndLoadUserAgentConfigOmitsAPIKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := &AgentConfig{
		Provider: "openrouter",
		Model:    "openai/gpt-4o-mini",
		ApiKey:   "test-key",
		SystemPrompt: SystemPromptConfig{
			Name: "Howard",
		},
	}
	if err := SaveAgentConfig(cfg); err != nil {
		t.Fatalf("SaveAgentConfig() error = %v", err)
	}

	path := filepath.Join(home, ".config", "howard", "config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config at %s: %v", path, err)
	}

	loaded, err := LoadUserAgentConfig()
	if err != nil {
		t.Fatalf("LoadUserAgentConfig() error = %v", err)
	}
	if loaded.Provider != cfg.Provider || loaded.Model != cfg.Model || loaded.ApiKey != "" {
		t.Fatalf("LoadUserAgentConfig() = %#v, want provider/model from %#v and empty apikey", loaded, cfg)
	}
}

func TestSaveAndLoadAuthConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := SetProviderAuth("openrouter", "test-key"); err != nil {
		t.Fatalf("SetProviderAuth() error = %v", err)
	}

	path := filepath.Join(home, ".config", "howard", "auth.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected auth config at %s: %v", path, err)
	}

	auth, err := LoadAuthConfig()
	if err != nil {
		t.Fatalf("LoadAuthConfig() error = %v", err)
	}
	if auth.Credentials["openrouter"].ApiKey != "test-key" {
		t.Fatalf("openrouter auth key = %q, want test-key", auth.Credentials["openrouter"].ApiKey)
	}
}
