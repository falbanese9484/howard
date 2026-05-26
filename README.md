# agent-harness

A focused coding agent harness built in Go. Runs a terminal UI (TUI) agent that inspects projects, explains tradeoffs, and makes precise, style-preserving code edits via configurable LLM providers.

## Features
- Terminal UI built with Bubble Tea + Lip Gloss
- Tool-using agent (ls, read_file, edit_file, write_file, mkdir)
- Pluggable providers: Ollama (local) and OpenRouter
- System prompt and tool definitions driven by `config.json`
- Small, targeted edits by design; auto-runs formatting/tests after edits when available

## Project Layout
```
cmd/harness/     # entrypoint
internal/
  chat/          # message bus + executor
  provider/      # ollama, openrouter, tools, config
  tui/           # bubbletea model, components, styles
  types/         # shared types
  logger/        # logging
config.json      # provider + system prompt + tools
```

## Requirements
- Go 1.24+
- For Ollama: local Ollama running with a chosen model
- For OpenRouter: `OPENROUTER_API_KEY` in env or `.env`

## Usage
```bash
go run ./cmd/harness
```
Edit `config.json` to switch providers or tweak the agent identity/prompt/tools.

## Configuration
- `provider`: `ollama` or `openrouter`
- `system_prompt`: agent identity and operating principles split into titled sections
- `tools`: JSON schema for available tools (ls, read_file, edit_file, write_file, mkdir)

## Development
```bash
go mod tidy
go build ./...
```
Follow the agent principles when contributing: read before editing, make small exact replacements, preserve style, run formatting/tests afterward.

## License
MIT
