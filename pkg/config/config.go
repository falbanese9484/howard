package config

import (
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

//go:embed tools.json
var builtinToolsJSON []byte

type AgentConfig struct {
	Provider     string             `json:"provider"`
	SystemPrompt SystemPromptConfig `json:"system_prompt"`
	Model        string             `json:"model"`
	ApiKey       string             `json:"apikey,omitempty"`
	Tools        []ToolConfig       `json:"tools"`
	Web          WebConfig          `json:"web"`
}

// WebConfig controls the web_search and fetch_url tools. When Enabled is
// false (the default) those tools refuse to run and the agent is told they
// are unavailable.
type WebConfig struct {
	Enabled             bool     `json:"enabled"`
	SearchBackend       string   `json:"search_backend,omitempty"`
	DomainAllowlist     []string `json:"domain_allowlist,omitempty"`
	MaxFetchBytes       int64    `json:"max_fetch_bytes,omitempty"`
	FetchTimeoutSeconds int      `json:"fetch_timeout_seconds,omitempty"`
	ApiKey              string   `json:"-"`
}

type AuthConfig struct {
	Credentials map[string]ProviderAuth `json:"credentials"`
}

type ProviderAuth struct {
	ApiKey string `json:"apikey,omitempty"`
}

type SystemPromptConfig struct {
	Name     string                `json:"name"`
	Sections []SystemPromptSection `json:"sections"`
}

type SystemPromptSection struct {
	Title   string   `json:"title"`
	Content []string `json:"content"`
}

type ToolConfig struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Parameters  []ParameterConfig `json:"parameters"`
}

type ParameterConfig struct {
	ParameterName string `json:"param"`
	Description   string `json:"description"`
}

type toolsFileConfig struct {
	Tools []ToolConfig `json:"tools"`
}

func findUserConfig(name string) (string, error) {
	root, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	candidate := filepath.Join(root, name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return "", os.ErrNotExist
}

func UserConfigPath() (string, error) {
	root, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".config", "howard", "config.json"), nil
}

func UserAuthPath() (string, error) {
	root, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, ".config", "howard", "auth.json"), nil
}

func LoadUserAgentConfig() (*AgentConfig, error) {
	path, err := UserConfigPath()
	if err != nil {
		return &AgentConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AgentConfig{}, nil
		}
		return &AgentConfig{}, err
	}

	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &AgentConfig{}, err
	}
	return &cfg, nil
}

func SaveAgentConfig(cfg *AgentConfig) error {
	path, err := UserConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	configCopy := *cfg
	configCopy.ApiKey = ""

	data, err := json.MarshalIndent(&configCopy, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func LoadAuthConfig() (*AuthConfig, error) {
	path, err := UserAuthPath()
	if err != nil {
		return &AuthConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AuthConfig{Credentials: map[string]ProviderAuth{}}, nil
		}
		return &AuthConfig{}, err
	}

	var cfg AuthConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &AuthConfig{}, err
	}
	if cfg.Credentials == nil {
		cfg.Credentials = map[string]ProviderAuth{}
	}
	return &cfg, nil
}

func SaveAuthConfig(cfg *AuthConfig) error {
	path, err := UserAuthPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func SetProviderAuth(provider, apiKey string) error {
	cfg, err := LoadAuthConfig()
	if err != nil {
		return err
	}
	if cfg.Credentials == nil {
		cfg.Credentials = map[string]ProviderAuth{}
	}
	if apiKey == "" {
		delete(cfg.Credentials, provider)
	} else {
		cfg.Credentials[provider] = ProviderAuth{ApiKey: apiKey}
	}
	return SaveAuthConfig(cfg)
}

func LoadAgentConfig() (*AgentConfig, error) {
	builtinTools, err := loadBuiltinTools()
	if err != nil {
		return &AgentConfig{}, err
	}

	path, err := findUserConfig(filepath.Join(".config", "howard", "config.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AgentConfig{Tools: builtinTools}, nil
		}
		return &AgentConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return &AgentConfig{}, err
	}

	var cfg AgentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return &AgentConfig{}, err
	}
	auth, err := LoadAuthConfig()
	if err != nil {
		return &AgentConfig{}, err
	}
	if credentials, ok := auth.Credentials[cfg.Provider]; ok && credentials.ApiKey != "" {
		cfg.ApiKey = credentials.ApiKey
	}
	if cfg.Web.SearchBackend != "" {
		if credentials, ok := auth.Credentials[cfg.Web.SearchBackend]; ok && credentials.ApiKey != "" {
			cfg.Web.ApiKey = credentials.ApiKey
		}
	}
	cfg.Tools = mergeTools(builtinTools, cfg.Tools)
	if !cfg.Web.Enabled {
		cfg.Tools = filterWebTools(cfg.Tools)
	}
	return &cfg, nil
}

func loadBuiltinTools() ([]ToolConfig, error) {
	var cfg toolsFileConfig
	if err := json.Unmarshal(builtinToolsJSON, &cfg); err != nil {
		return nil, err
	}
	return cfg.Tools, nil
}

func mergeTools(base, extensions []ToolConfig) []ToolConfig {
	tools := append([]ToolConfig(nil), base...)
	indexes := make(map[string]int, len(tools))
	for i, tool := range tools {
		indexes[tool.Name] = i
	}

	for _, tool := range extensions {
		if index, ok := indexes[tool.Name]; ok {
			tools[index] = tool
			continue
		}
		indexes[tool.Name] = len(tools)
		tools = append(tools, tool)
	}
	return tools
}

// WebToolNames lists the tool names that depend on the web subsystem.
var WebToolNames = []string{"web_search", "fetch_url"}

func filterWebTools(tools []ToolConfig) []ToolConfig {
	skip := make(map[string]struct{}, len(WebToolNames))
	for _, name := range WebToolNames {
		skip[name] = struct{}{}
	}
	out := make([]ToolConfig, 0, len(tools))
	for _, tool := range tools {
		if _, drop := skip[tool.Name]; drop {
			continue
		}
		out = append(out, tool)
	}
	return out
}

func BuildSystemPrompt(cfg SystemPromptConfig, tools []ToolConfig) string {
	if cfg.Name == "" && len(cfg.Sections) == 0 && len(tools) == 0 {
		return ""
	}

	prompt := "You are " + cfg.Name + ", a local coding agent who feels like a sharp coworker the user would actually want to have a beer with."
	if cfg.Name == "" {
		prompt = "You are a coding agent."
	}

	for _, section := range cfg.Sections {
		if section.Title != "" {
			prompt += "\n\n## " + section.Title
		}
		for _, item := range section.Content {
			prompt += "\n- " + item
		}
	}

	if len(tools) > 0 {
		prompt += "\n\n## Available tools"
		for _, tool := range tools {
			prompt += "\n- " + tool.Name + ": " + tool.Description
			for _, param := range tool.Parameters {
				prompt += "\n  - " + param.ParameterName + ": " + param.Description
			}
		}
	}
	return prompt
}
