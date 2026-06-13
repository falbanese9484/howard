package config

import (
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
