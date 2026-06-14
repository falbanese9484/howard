# howard - a minimal local AI agent

## Mission
There are a lot of coding agents and harnesses out there, and this is very much another one. The goal is not to have every possible bell and whistle; it is to stay small, understandable, and useful, with an emphasis on getting the most out of lightweight open source models.

Howard is a focused coding agent harness built in Go. It runs a terminal UI (TUI) agent that inspects projects, explains tradeoffs, and makes precise, style-preserving code edits via configurable LLM providers.

## Features
- Terminal UI built with Bubble Tea + Lip Gloss, markdown rendering via Glamour
- Tool-using agent (ls, read_file, edit_file, write_file, mkdir, bash); file tools are sandboxed to the project root, while bash runs with your full user permissions
- Pluggable providers: Ollama (local) and OpenRouter
- Runtime model switching (Ctrl+F opens a model selector)
- System prompt driven by JSON config, with built-in tools embedded from `pkg/config/tools.json`
- Small, targeted edits by design: edit_file requires an exact, unique text match

## Project Layout
```
cmd/tui/         # TUI entrypoint
internal/
  chat/          # message bus between provider and TUI
  provider/      # ollama, openrouter, tool execution
  tui/           # bubbletea model, components, styles
  types/         # shared types
  logger/        # logging
pkg/config/         # agent config loading + system prompt builder
config_example.json # example config (copy to ~/.config/howard/config.json)
```

## Requirements
- Go 1.24+
- For Ollama: local Ollama running at `localhost:11434` with a chosen model
- For OpenRouter: an API key set via `apikey` in the config file

## Usage
```bash
mkdir -p ~/.config/howard
cp config_example.json ~/.config/howard/config.json
go run ./cmd/howard
```
Configuration is read from `~/.config/howard/config.json`; `config_example.json` in this repo is a template. Edit your copy to set the provider, model, API key if needed, and agent prompt.

CLI commands:
- `howard` or `howard tui`: launch the interactive TUI
- `howard run "prompt"`: send one prompt and stream the assistant response to stdout
- `howard run -model llama3.2 "prompt"`: run one prompt with a temporary model override
- `howard models`: list models from the configured provider
- `howard help`: show command usage

During `howard run`, assistant text is written to stdout and tool/status messages are written to stderr.

Keybindings:
- `Enter` — send prompt
- `Ctrl+F` — open model selector
- `Esc` / `Ctrl+C` — close selector, or quit

## Configuration
- `provider`: `ollama` or `openrouter`
- `model`: model name to use, such as an Ollama tag or OpenRouter model ID
- `apikey`: API key (required for OpenRouter)
- `system_prompt`: agent identity and operating principles split into titled sections
- `tools`: optional additional tool definitions. Built-in tools are embedded from `pkg/config/tools.json`; entries in user config with the same name override the built-in definition, and new names are appended.

Logs are written to `./logs/harness.log` by default; set `LOG_FILE_PATH` to change the directory prefix.

## Development
```bash
go mod tidy
go build ./...
```
Follow the agent principles when contributing: read before editing, make small exact replacements, preserve style, run formatting/tests afterward.

## License
MIT
