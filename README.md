# agent-harness

A focused coding agent harness built in Go. Runs a terminal UI (TUI) agent that inspects projects, explains tradeoffs, and makes precise, style-preserving code edits via configurable LLM providers.

## Features
- Terminal UI built with Bubble Tea + Lip Gloss, markdown rendering via Glamour
- Tool-using agent (ls, read_file, edit_file, write_file, mkdir), sandboxed to the project root
- Pluggable providers: Ollama (local) and OpenRouter
- Runtime model switching (Ctrl+F opens a model selector)
- System prompt and tool definitions driven by a JSON config
- Small, targeted edits by design: edit_file requires an exact, unique text match

## Project Layout
```
cmd/harness/     # entrypoint
internal/
  chat/          # message bus between provider and TUI
  provider/      # ollama, openrouter, tool execution
  tui/           # bubbletea model, components, styles
  types/         # shared types
  logger/        # logging
pkg/config/      # agent config loading + system prompt builder
config.json      # example config (copy to ~/.config/howard/config.json)
```

## Requirements
- Go 1.24+
- For Ollama: local Ollama running at `localhost:11434` with a chosen model
- For OpenRouter: an API key set via `apikey` in the config file

## Usage
```bash
mkdir -p ~/.config/howard
cp config.json ~/.config/howard/config.json
go run ./cmd/harness
```
Configuration is read from `~/.config/howard/config.json`; the `config.json` in this repo is a template. Edit your copy to switch providers or tweak the agent identity/prompt/tools.

Keybindings:
- `Enter` — send prompt
- `Ctrl+F` — open model selector
- `Esc` / `Ctrl+C` — close selector, or quit

## Configuration
- `provider`: `ollama` or `openrouter`
- `model`: model name to use (e.g. an Ollama tag or OpenRouter model ID)
- `apikey`: API key (required for OpenRouter)
- `system_prompt`: agent identity and operating principles split into titled sections
- `tools`: definitions for available tools (ls, read_file, edit_file, write_file, mkdir)

Logs are written to `./logs/harness.log` by default; override the directory with the `LOG_FILE_PATH` env var.

## Development
```bash
go mod tidy
go build ./...
```
Follow the agent principles when contributing: read before editing, make small exact replacements, preserve style, run formatting/tests afterward.

## License
MIT
