package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

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
	path, err := findUserConfig(filepath.Join(".config", "howard", "config.json"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AgentConfig{}, nil
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
	return &cfg, nil
}

func BuildSystemPrompt(cfg SystemPromptConfig) string {
	if cfg.Name == "" && len(cfg.Sections) == 0 {
		return ""
	}

	prompt := "You are " + cfg.Name + ", a coding agent."
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
	return prompt
}
