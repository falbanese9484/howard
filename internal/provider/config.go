package provider

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/falbanese9484/agent-harness/internal/types"
)

type agentConfig struct {
	Provider     string             `json:"provider"`
	SystemPrompt systemPromptConfig `json:"system_prompt"`
	Tools        []types.Tool       `json:"tools"`
}

type systemPromptConfig struct {
	Name     string                `json:"name"`
	Sections []systemPromptSection `json:"sections"`
}

type systemPromptSection struct {
	Title   string   `json:"title"`
	Content []string `json:"content"`
}

func loadToolsConfig() ([]types.Tool, error) {
	cfg, err := loadAgentConfig()
	if err != nil {
		return nil, err
	}
	return cfg.Tools, nil
}

func loadSystemPrompt() (string, error) {
	cfg, err := loadAgentConfig()
	if err != nil {
		return "", err
	}
	return buildSystemPrompt(cfg.SystemPrompt), nil
}

func loadAgentConfig() (agentConfig, error) {
	path, err := findProjectConfig("config.json")
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return agentConfig{}, nil
		}
		return agentConfig{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return agentConfig{}, err
	}

	var cfg agentConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return agentConfig{}, err
	}
	return cfg, nil
}

func buildSystemPrompt(cfg systemPromptConfig) string {
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

func findProjectConfig(name string) (string, error) {
	root, err := findProjectRoot()
	if err != nil {
		return "", err
	}

	candidate := filepath.Join(root, name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return "", os.ErrNotExist
}

func findProjectRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.EvalSymlinks(dir)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
