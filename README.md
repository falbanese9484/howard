# howard - a minimal local AI agent

## Mission
There are a lot of coding agents and harnesses out there, and this is very much another one. The goal is not to have every possible bell and whistle; it is to stay small, understandable, and useful, with an emphasis on getting the most out of lightweight open source models.

Howard is a focused coding agent harness built in Go. It runs a terminal UI (TUI) agent that inspects projects, explains tradeoffs, and makes precise, style-preserving code edits via configurable LLM providers.

## Features
- Terminal UI built with Bubble Tea + Lip Gloss, markdown rendering via Glamour
- Tool-using agent (ls, read_file, edit_file, write_file, mkdir, bash); file tools are sandboxed to the workspace root (the current directory by default, overridable via `workspace_root` config or `run -root`), while bash runs with your full user permissions
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
Configuration is read from `~/.config/howard/config.json`; `config_example.json` in this repo is a template. Edit your copy to set the provider, model, and agent prompt.

CLI commands:
- `howard` or `howard tui`: launch the interactive TUI
- `howard run "prompt"`: send one prompt and stream the assistant response to stdout
- `howard run -model llama3.2 "prompt"`: run one prompt with a temporary model override
- `howard run -root /path/to/project "prompt"`: run one prompt with a temporary workspace root override
- `howard models`: list models from the configured provider
- `howard auth openrouter`: securely store OpenRouter credentials and switch to OpenRouter
- `howard auth openrouter -key sk-or-...`: non-interactively store OpenRouter credentials
- `howard auth ollama`: switch to Ollama, which does not require credentials
- `howard auth brave`: securely store Brave Search credentials and enable web search
- `howard auth brave -key BSA...`: non-interactively store Brave Search credentials
- `howard help`: show command usage

During `howard run`, assistant text is written to stdout and tool/status messages are written to stderr.

Keybindings:
- `Enter` — send prompt
- `Ctrl+F` — open model selector
- `Esc` / `Ctrl+C` — close selector, or quit

## Configuration
- `provider`: `ollama` or `openrouter`
- `model`: model name to use, such as an Ollama tag or OpenRouter model ID
- `workspace_root`: optional sandbox root for file tools. Defaults to the current working directory; `~` is expanded. The `run -root` flag overrides this per invocation.
- `system_prompt`: agent identity and operating principles split into titled sections
- `tools`: optional additional tool definitions. Built-in tools are embedded from `pkg/config/tools.json`; entries in user config with the same name override the built-in definition, and new names are appended.

Credentials are stored separately in `~/.config/howard/auth.json` by `howard auth`. Runtime config still comes from `~/.config/howard/config.json`.

## Web Search
Howard can expose `web_search` and `fetch_url` when web tooling is enabled in config. Brave Search is the supported search backend.

Store your Brave Search API key:
```bash
howard auth brave
```

Or set it in the environment:
```bash
export BRAVE_SEARCH_API_KEY="..."
```

Enable web tools in `~/.config/howard/config.json`:
```json
"web": {
  "enabled": true,
  "search_backend": "brave"
}
```

`howard auth brave` enables this config automatically. `fetch_url` remains restricted by `web.domain_allowlist` and will fail closed for domains that are not allowed.

Logs are written to `./logs/harness.log` by default; set `LOG_FILE_PATH` to change the directory prefix.

## Development
```bash
go mod tidy
go build ./...
```
Follow the agent principles when contributing: read before editing, make small exact replacements, preserve style, run formatting/tests afterward.

### Worker state ownership
`app.NewProvider` constructs a separate execution state and conversation for each
provider. File tools and bash use that execution's resolved workspace; web tools
use its own service and copied web settings. Tool definitions are copied at
construction and when generating requests. Workspace selection precedence remains
`run -root`, then `workspace_root`, then the working directory at construction.

A provider owns one in-memory conversation. Overlapping `Chat` calls on that
provider are rejected with `provider.ErrConversationBusy`; independent workers
must use independently constructed providers. Model changes are synchronized and
take effect on the next turn, without changing an active tool round-trip. Callers
must not mutate requests while a turn is using them.

This is in-process state isolation. Bash still has the user's full permissions,
and path/symlink checks do not protect against concurrent filesystem replacement.
Separate worker objects do not isolate processes or two workers deliberately
configured to use the same files. Lifecycle execution, cancellation, restricted
tool policy, and durable sessions are subsequent slices.

### In-process lifecycle executor

`app.NewExecutor(cfg, log)` exposes a synchronous internal
`Execute(ctx, domain.RunRequest, executor.Observer)` entry point. It constructs
fresh provider/tool state per run and emits ordered lifecycle events separately
from assistant/tool output. Active RunIDs are unique within that executor;
`SessionID` correlates events but does not restore conversation history.

Accepted turns emit `queued`, `running`, and one `succeeded`/`failed` outcome;
setup failure may fail directly from `queued`. Invalid, already-canceled, and
duplicate-active requests are rejected before acceptance. Exported failures use
fixed sanitized messages. The adapter waits for provider return and pending
output before finalizing. Callbacks must return promptly.

This entry point does not replace the CLI/TUI or guarantee in-flight cancellation;
it never emits `canceled`. See [the run contract](notes/run-contract.md) for
ownership, context, failure and rollback semantics.

## License
MIT
