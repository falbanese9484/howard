# Howard — worker foundation for central agents

Howard is a small Go agent runtime being developed as the worker foundation for
centrally operated, governed shared agents. The proposed platform would add durable
agent identity, scoped context and tools, and requester-aware authorization around
execution. Existing harnesses remain useful; replacing personal OpenCode setups
is not Howard's goal.

## Current capabilities and status

**Implemented on main:** a local CLI/TUI and an in-process worker API. The enterprise
service and control plane described below are **proposed, not implemented**.

- CLI prompt runs and a Bubble Tea TUI with markdown output, model switching and
  slash commands (currently `/new`).
- Ollama and OpenRouter providers, configurable prompts, built-in file/shell tools
  for the legacy local CLI/TUI, and optional Brave Search/web fetching.
- A [run contract](notes/run-contract.md) and synchronous executor with ordered
  lifecycle events, separate output callbacks, per-run provider/conversation/tool
  state, and cancellation that waits for execution to stop before a terminal event.
- Immutable worker profiles selected by trusted embedding code and checked at
  dispatch: `ReadOnly` permits configured `ls`/`read_file`; `ReadOnlyWeb` also permits
  configured, enabled web tools. Both deny shell, file mutations and unknown tools.

### Proposed central-agent MVP

A durable central agent with essential utilities, with **all platform domain
records in PostgreSQL**: agent/config versions, scopes and context policy,
users/roles/permissions, interface identity bindings, tool/MCP registry and
assignments, sessions/runs/history, audit and artifact metadata.

The MVP includes an authorized management CRUD API and small admin UI for agent
configuration, tools/MCP integrations, context, roles and bindings. Registration
alone grants no execution authority. Each agent has an explicit scope, permissions
and context; each interface identifier must link to an authenticated company user
with a role. Effective access is **role permissions ∩ agent ceiling ∩
resource/context/audience policy**. User privileges can never widen an agent's
scope. The company identity/RBAC adapter remains unknown. Tools, skills and MCPs
are used only when needed, with authorization still checked at use and at
context, memory, artifact and output boundaries.

Workers/pods and their per-run workspaces are ephemeral; durable state lives
outside them. Local scratch is not automatically retained. Recovery must not
replay unknown side effects after a crash. Object storage for retained artifact
bytes and a secret manager for credentials are conditional implementation choices,
with references in PostgreSQL; neither EFS nor Redis is required.

Design sources (planning, not shipped capabilities):

- [Central platform vision and clarified MVP](https://github.com/falbanese9484/knowledgebase/blob/main/projects/central-agent-platform.md)
- [Draft PR #20: authenticated service boundary and management/authorization design](https://github.com/falbanese9484/howard/pull/20)
- [Draft PR #21: PostgreSQL domain, durable sessions and conservative recovery](https://github.com/falbanese9484/howard/pull/21)

The two design PRs are open, unmerged proposals as of 2026-10-25. They do not add a
running service, PostgreSQL persistence, a control-plane UI or a tool/MCP registry
to main.

## Requirements

- Go 1.24+
- For Ollama: local Ollama running at `localhost:11434` with a chosen model
- For OpenRouter: an API key stored with `howard auth openrouter` (legacy `apikey` config is also supported)

## Quickstart

```bash
mkdir -p ~/.config/howard
cp -n config_example.json ~/.config/howard/config.json
go build -o howard ./cmd/howard
./howard
```
Configuration is read from `~/.config/howard/config.json`; `config_example.json` in this repo is a template. Edit your copy to set the provider, model, and agent prompt.

The commands below assume `howard` is on your `PATH`; use `./howard` for the local build.

CLI commands:

- `howard` or `howard tui`: launch the interactive TUI
- `howard run "prompt"`: send one prompt and stream the assistant response to stdout
- `howard run -model llama3.2 "prompt"`: run one prompt with a temporary model override
- `howard run -root /path/to/project "prompt"`: run one prompt with a temporary workspace root override
- `howard models`: list models from the configured provider
- `howard auth openrouter`: store OpenRouter credentials and switch to OpenRouter
- `howard auth openrouter -key sk-or-...`: non-interactively store OpenRouter credentials
- `howard auth ollama`: switch to Ollama, which does not require credentials
- `howard auth brave`: store Brave Search credentials and enable web search
- `howard auth brave -key BSA...`: non-interactively store Brave Search credentials
- `howard help`: show command usage

During `howard run`, assistant text is written to stdout and tool/status messages are written to stderr.

Keybindings:

- `Enter` — send prompt
- `Ctrl+F` — open model selector
- `Esc` / `Ctrl+C` — close selector, or quit

Slash commands (type in the prompt and press `Enter`):

- `/new` — start a fresh session, clearing the chat, prompt history, and model conversation while keeping the selected model and settings. Available after the current response finishes.

Type `/` to show available commands above the prompt. Keep typing to filter the
list, use `↑` / `↓` to select a command, and press `Enter` to run it. `Tab` fills
in the selected command; `Esc` dismisses the menu.

Only an exact slash command runs directly. Leading or trailing whitespace,
newlines, additional text, and unknown slash commands are sent to the LLM as
ordinary prompt text. Selecting a command from the menu runs that command.

## Configuration

- `provider`: `ollama` or `openrouter`
- `model`: model name to use, such as an Ollama tag or OpenRouter model ID
- `workspace_root`: optional workspace root for file tools. Defaults to the current working directory; `~` is expanded. The `run -root` flag overrides this per invocation.
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

`howard auth brave` enables this config automatically. `fetch_url` checks the initial URL host against `web.domain_allowlist`; redirects are not reauthorized. This is not a strict egress/SSRF boundary.

Logs are written to `./logs/harness.log` by default; set `LOG_FILE_PATH` to change the directory prefix.

## Worker API and limits

`app.NewExecutor(cfg, log, provider.ReadOnly)` constructs the internal worker
adapter; `Execute(ctx, domain.RunRequest, executor.Observer)` runs one turn.
Configuration is snapshotted and provider/tool state is fresh per run. Active
RunIDs are unique within that executor only; `SessionID` correlates events and
does not restore conversation history. Invalid, already-canceled and duplicate
active requests are rejected before acceptance. Accepted runs emit `queued`,
then `running` and one terminal outcome; setup failure or pre-launch cancellation
can terminate directly from `queued`. Callbacks must return promptly.

The local CLI/TUI uses the separate legacy `app.NewProvider` policy, including
shell and editing tools. Bash runs with the user's full permissions. Workspace
path/symlink checks and independent worker objects are **in-process controls, not
OS sandboxing, tenant isolation or service authentication**. Concurrent filesystem
replacement is outside those checks. Cancellation does not undo completed effects.
Do not expose the legacy CLI/TUI factory as an untrusted multi-user service.
See the [run contract and worker profiles](notes/run-contract.md#restricted-worker-tools-8)
for enforcement, cancellation and verification limits.

## Development

```bash
go build ./...
go test ./internal/domain ./internal/executor ./internal/app ./internal/provider
```

Entry points live in `cmd/howard` (CLI/TUI) and `cmd/tui` (TUI); worker contracts
and execution are in `internal/domain`, `internal/executor` and `internal/app`.
Providers/tool dispatch live in `internal/provider`, UI in `internal/tui`, and
configuration plus built-in tool definitions in `pkg/config`.

Read before editing, keep changes small, preserve style and run relevant checks.
The [run contract](notes/run-contract.md) records known full-suite failures and
platform-specific validation limits; focused checks are not a full-suite pass.

## License

MIT
