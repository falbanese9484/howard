# Review Checklist

Running list of code-review findings to revisit. Grouped by priority. Each item
has a corresponding `//TODO:` comment at the referenced code location.

## High Priority — Correctness

- [ ] **SSE/NDJSON line buffer dropped on overflow** — `ollama.go:190`, `openrouter.go:231`.
  `bufio.Scanner` is capped at 1 MB. A single streamed line larger than that makes
  `Scan()` stop and surface `bufio.ErrTooLong` (Ollama) or silently truncate. Use
  `bufio.Reader.ReadString('\n')` or raise/announce the cap.

- [ ] **Unmarshal error handling diverges between providers** — `ollama.go:196`, `openrouter.go:254`.
  Ollama `continue`s past malformed chunks (silent data loss); OpenRouter aborts the
  whole turn on any non-`[DONE]` `data:` payload that fails to parse. Pick one policy.

- [ ] **`o.messages` mutated from a goroutine without synchronization** — `ollama.go:126`, `openrouter.go:170`.
  `bus.Send` runs in a goroutine (`model.go:266`, `cli.go:82`) and writes provider state.
  TUI guards with `isResponding` but there is no memory barrier or structural guarantee.
  Run `go test -race ./...`; add a mutex or serialize sends through the bus.

- [ ] **Conversation history grows unbounded, includes full tool output** — `ollama.go:124-126`, `openrouter.go:168-170`.
  Every user/assistant/tool message is appended permanently and re-sent each turn.
  Verbatim tool output (read_file, bash) blows small context windows — the core
  use case for lightweight local models. Add a context budget / truncation / summary.

## Medium Priority — Robustness

- [ ] **Bus channels can deadlock; no request cancellation** — `bus.go:24-26`, `bus.go:53`.
  `Content` buffered at 16; provider blocks forever if the TUI stops draining.
  `Ctx` is always `context.Background()` and never cancelled, so in-flight requests
  cannot be aborted. Wire real `context.Context` cancellation (Esc/Ctrl+C, new prompt).

- [ ] **All tool params typed `string` and marked `required`** — `ollama.go:241-246`.
  Optional params (e.g. `ls` path, `fetch_url` format) cannot be expressed and the
  model is told they are mandatory. Add `type` and `required` to `ToolParameter`.

- [ ] **`bash` timeout orphans child processes** — `tools.go:204-207`.
  `CommandContext` sends SIGKILL only to the direct child; spawned grandchildren leak.
  Use a process group / `cmd.Cancel` to kill the whole tree.

## Low Priority — Polish / Tech Debt

- [ ] **OpenRouter has no tests** — `openrouter.go`.
  Largest, most complex file; streaming tool-call delta assembly
  (`buildOpenRouterToolCalls`) is untested. Add table tests with captured SSE fixtures.

- [ ] **Duplicated `Chat` loops** — `ollama.go:99`, `openrouter.go:146`.
  ~90% identical agentic loops. Extract a shared driver taking a `sendChat`-like func.

- [ ] **Dead/WIP code on main** — `internal/database`, `internal/domain`, `sandbox/main.go`.
  No callers; pulls cgo sqlite into the binary. Gate or mark clearly.

- [x] **`findProjectRoot` keys on `os.Getwd()` + `go.mod`** — `tools.go:329`.
  Resolved: the sandbox root now defaults to `os.Getwd()` (works in any project, Go
  or not) and is overridable via the `run -root` flag and the `workspace_root` config
  field, wired through `app.NewProvider` → `provider.SetWorkspaceRoot`.

- [ ] **`wrapText` uses `len()` for width** — `model.go:312`.
  Byte length, not display width; multi-byte/wide chars wrap wrong. Use `lipgloss`/`x/ansi`.

- [ ] **Vestigial `Context []int` / `Stream` plumbing** — `types/chat.go:23`, `types/chat.go:48`.
  `Context` is Ollama `/api/generate` legacy, unused by `/api/chat`. Remove to reduce confusion.
