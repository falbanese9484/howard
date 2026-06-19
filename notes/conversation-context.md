# Conversation Context

This file captures the visible conversation context from the current session. Private system/developer instructions are intentionally not included.

## Project

The project is a Go application named **Howard**, a minimal local AI agent harness.

Module:

```text
github.com/falbanese9484/agent-harness
```

Main README description:

> Howard is a focused coding agent harness built in Go. It runs a terminal UI (TUI) agent that inspects projects, explains tradeoffs, and makes precise, style-preserving code edits via configurable LLM providers.

## Conversation Transcript

### User

> Please review this project im working on and provide feedback

### Assistant

I reviewed the project. Short version: this is a strong foundation — clean Go structure, real CLI/TUI, provider support, tool sandboxing, auth separation, and tests. But before I’d trust it as a daily coding agent, I’d fix a few correctness/safety issues.

#### Biggest issues to fix

##### 1. Tests are currently failing

`go test ./...` fails in `internal/chat`.

The tests expect “time to first token” logging, but `bus.go` only logs `"message complete"`. I’d fix this first because a project with failing tests loses credibility quickly.

##### 2. Chat bus can deadlock

`chat.Bus.Content` is buffered at 16. Provider goroutines write directly to it. If the TUI stops draining — user cancels, quits, sends a new prompt — the provider can block forever.

Also, `Send` uses `context.Background()`, so in-flight requests cannot be cancelled.

I’d add per-request contexts and cancellation support.

##### 3. Provider state is mutated without synchronization

Both `Ollama` and `OpenRouter` mutate `o.messages` from `Chat`, which runs in a goroutine.

That’s a data race waiting to happen.

At minimum, add a mutex around provider message history. Better: avoid mutating provider state directly from the agentic loop.

##### 4. Conversation history grows unbounded

This is probably the biggest practical issue for lightweight local models.

Every user prompt, assistant response, and full tool output is appended forever. `read_file` and `bash` output can be huge, which means local models will burn context fast.

I’d add context budgeting before adding more features:

- keep only last N messages;
- cap tool output size;
- summarize or drop old tool output;
- preserve user messages more carefully than tool output.

##### 5. Tool parameter schema is too weak

Right now all tool params are treated as:

```go
type string
required: true
```

That means optional params like:

- `fetch_url.format`
- `web_search.max_results`

are advertised as required.

I’d extend `ParameterConfig` with `type` and `required`.

##### 6. Bash tool needs timeout hardening

The README correctly warns that `bash` runs with full user permissions. But on timeout, `exec.CommandContext` may leave child/grandchild processes running.

I’d use process groups or `cmd.Cancel` to kill the whole tree.

##### 7. Project root assumes Go projects

`findProjectRoot()` searches upward for `go.mod`. That means file tools are rootless in non-Go projects.

I’d add:

- `HOWARD_ROOT` env var;
- maybe `--root` CLI flag;
- fallback to `os.Getwd()` if no `go.mod` exists.

##### 8. SSE/NDJSON line buffering can fail

This was already noted in `notes/review-checklist.md`.

`bufio.Scanner` has a 1 MB cap. Large reasoning/tool-call chunks can exceed it. I’d switch to `bufio.Reader.ReadString('\n')` or explicitly detect `bufio.ErrTooLong` and return a clear error.

#### Good stuff

This project already has a lot right:

- clean package boundaries;
- useful CLI and TUI;
- Ollama and OpenRouter support;
- runtime model switching;
- auth separated from config;
- built-in tools merged with user config;
- web search/fetch sandboxed by domain allowlist;
- exact-match `edit_file`;
- tests across config, CLI, provider tools, and web.

The self-review notes are also useful. The project already knows where the rough edges are.

#### Suggested priority order

If I were tackling this next, I’d do:

1. **Make tests pass**
2. **Add request cancellation**
3. **Fix provider data races**
4. **Add context budgeting/history truncation**
5. **Strengthen tool schemas**
6. **Make project root configurable**
7. **Add OpenRouter tests**
8. **Harden `bash` timeout cleanup**

#### Overall take

This is a solid minimal agent harness. The architecture is understandable and the safety model is directionally right. The main risks are concurrency, cancellation, and context explosion — exactly the kinds of things that feel fine in demos but bite you during real use.

Fix those, and this becomes much more trustworthy as a daily-driver local coding agent.

### User

> Take everything in the context window of this conversation and write a markdown file with its contents.

## Project Feedback Summary

### Architecture

The project has a clean Go structure:

```text
cmd/
internal/
  app/
  chat/
  cli/
  provider/
  tui/
  types/
  web/
pkg/config/
```

The main runtime pieces are:

- CLI entrypoint in `cmd/howard`
- TUI entrypoint in `cmd/tui`
- Provider implementations in `internal/provider`
- Shared types in `internal/types`
- Config loading and tool schema in `pkg/config`
- Web search/fetch tooling in `internal/web`
- Chat bus in `internal/chat`

### Existing Features

- Terminal UI built with Bubble Tea and Lip Gloss
- Markdown rendering through Glamour
- Tool-using agent with:
  - `ls`
  - `read_file`
  - `edit_file`
  - `write_file`
  - `mkdir`
  - `bash`
  - `web_search`
  - `fetch_url`
- File tools are sandboxed to the project root
- Bash runs with full user permissions
- Ollama provider support
- OpenRouter provider support
- Runtime model switching through the TUI
- System prompt generated from JSON config
- Built-in tools embedded from `pkg/config/tools.json`
- User-provided tools can override or extend built-in tools
- Auth credentials stored separately from runtime config
- Brave Search support for web search
- Domain allowlist for `fetch_url`

### Main Risks

1. **Failing tests**
   - `internal/chat` tests currently fail because expected “time to first token” logs are not present.

2. **Deadlock risk in chat bus**
   - `Content` channel has a fixed buffer of 16.
   - Provider goroutines can block forever if consumers stop draining.

3. **No request cancellation**
   - `context.Background()` is used for provider calls.
   - Requests cannot be cancelled from TUI or CLI.

4. **Unsynchronized provider state**
   - `Ollama.messages` and `OpenRouter.messages` are mutated from goroutines.
   - This can cause data races.

5. **Unbounded conversation history**
   - All messages and tool outputs are retained.
   - Large tool outputs will quickly exhaust context windows.

6. **Weak tool schema**
   - Tool parameters do not model type or requiredness.
   - Optional parameters are incorrectly advertised as required.

7. **Bash timeout cleanup**
   - `CommandContext` may leave descendant processes running after timeout.

8. **Project root discovery**
   - `findProjectRoot()` depends on finding a `go.mod`.
   - Non-Go projects are not handled well.

9. **Streaming line handling**
   - `bufio.Scanner` cap can drop or fail on large streamed lines.

## Suggested Next Work

Priority order:

1. Fix failing chat tests.
2. Add request cancellation.
3. Add synchronization around provider message state.
4. Add context budgeting or history truncation.
5. Extend tool parameter schema with type and required fields.
6. Make project root configurable.
7. Add OpenRouter tests.
8. Harden bash timeout cleanup.
