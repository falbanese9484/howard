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
	cfg.Tools = mergeTools(builtinTools, cfg.Tools)
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
