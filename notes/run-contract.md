# First worker slice: run lifecycle contract

`internal/domain/run.go` defines the internal contract. `internal/executor` now
emits these events for one in-process turn through a fakeable execution boundary.
The CLI, TUI and existing message bus retain their current entry points. This is
not an authenticated worker service.

- A session groups turns using the existing `domain.Session.ID`. A run is one
  turn with a unique caller-assigned `RunID` and an explicit `SessionID`.
- `RunRequest` captures the prompt, provider/model, and absolute workspace root.
  Validation checks shape only: it does not check session existence, ID uniqueness,
  supported models, filesystem existence, symlinks, permissions, or containment.
  Application construction resolves the workspace before the provider starts;
  a path is not a sandbox.
- Lifecycle events start at `queued` with sequence 1. They proceed to `running`
  and then `succeeded`, `failed`, or `canceled`. Setup failure and cancellation
  may terminate a queued run directly. Terminal states cannot transition again.
- `ValidateAfter` checks identity, contiguous sequence, nondecreasing timestamps,
  and transitions. Consumers must validate the complete stream in order and
  retain the last accepted event. It is not a durable store or replay mechanism.
- A failed event contains an error description. A canceled event means execution
  has stopped, not just that a stop was requested. The in-process executor/provider
  path implements this contract; legacy CLI/TUI cancellation wiring is separate.
  No automatic retry or side-effect replay is implied. The executor exports fixed,
  sanitized failure strings.

## In-process executor (#6)

Construct with `app.NewExecutor(cfg, log, provider.ReadOnly)`, then synchronously call
`Execute(ctx, request, executor.Observer{Lifecycle: ..., Output: ...})`.

- Configuration is snapshotted at construction. Each accepted run constructs a
  fresh provider, conversation, and isolated tool execution state through
  the private worker factory (not the legacy `app.NewProvider`). The request selects model and workspace; its provider must
  match the configured backend so credentials do not cross providers.
- `SessionID` is correlation metadata only. Reusing it does not restore history.
  Lifecycle and output callbacks belong to a single call; independent runs may
  invoke their callbacks concurrently.
- Invalid requests, already-canceled contexts, and duplicate active RunIDs are
  rejected without events or work. A nil context is an invalid request. Uniqueness
  is per executor instance and in memory; completed IDs may be reused. The ID is
  held through the terminal callback and released before `Execute` returns.
- Accepted runs emit `queued`, then `running`, then exactly one `succeeded`,
  `failed`, or `canceled` event. Setup failure goes directly from `queued` to
  `failed`; cancellation observed after acceptance but before launch goes to
  `canceled` without launching a turn. Sequence starts at one and timestamps are
  nondecreasing.
- `Turn.Run` must return only when execution and output have stopped. Factories
  construct fresh state and must not launch turns. The provider adapter drains
  text/tool output and waits for `Chat` to return, even after a done/error signal.
  Missing completion without cancellation is a failure. It does not finalize just
  because the caller context was canceled: `Chat` must return first. Callbacks must
  return promptly and must not panic; a callback forwarding output to a channel
  must select on the run context. Implementations must not retain callbacks or
  emit after returning. Arbitrarily blocking callbacks cannot be forcibly stopped.
- Provider errors become `provider execution failed`; setup errors become
  `execution setup failed`. Raw errors are not wrapped into returned errors or
  lifecycle metadata. `ResponseKindError` chunks are suppressed and treated as
  failures. Assistant/tool output is still ordinary, potentially sensitive
  provider content, not a sanitized result/artifact format.

## Restricted worker tools (#8)

The embedding caller selects `provider.ReadOnly` or `provider.ReadOnlyWeb` when
constructing `app.NewExecutor`. This is a Go API policy input, deliberately absent
from `RunRequest`, JSON prompts, and model tool arguments. Empty, unknown, and
`legacy-cli` worker profiles fail setup before any model call. The profile value
and configured tool names are snapshotted, so later caller configuration changes
cannot widen an existing executor's permissions.

- `ReadOnly`: only `ls` and `read_file`, and only when explicitly present in the
  configured tool list. An empty list permits no tools.
- `ReadOnlyWeb`: the same file tools plus explicitly configured `web_search` and
  `fetch_url`. Web must also be enabled; existing search backend, domain allowlist,
  fetch limits and timeouts still apply. This is explicit outbound-network
  authority, not a guarantee that external servers have no side effects. The
  existing fetch implementation checks the initial URL host; automatic redirects
  are not reauthorized. This profile is not a strict egress/SSRF boundary.
- No worker profile allows `bash`, `edit_file`, `write_file`, or `mkdir`.
  Unknown tool names are denied. A model-supplied shell call is rejected even if
  the caller's configuration accidentally includes its definition.
- Dispatch checks policy before arguments or tool side effects. Worker provider
  constructors filter the advertised definitions and system-prompt tool list.
  Per-turn `ChatRequest.Tools` cannot widen or replace worker definitions; the
  provider uses its fixed construction-time list in every model round.
- Local CLI/TUI uses `app.NewProvider` and an explicit internal legacy policy. Its
  full-user shell behavior is intentionally unchanged; it is **not** an endpoint
  for untrusted multi-user execution. Do not build a worker service by wrapping
  the legacy factory. Low-level `executor.New` accepts trusted custom code and
  does not make arbitrary custom turns safe.

File reads/listing use the resolved workspace and existing lexical/symlink
containment checks. These reject ordinary `..`, outside absolute paths, and
symlink escapes, but the check and open are not atomic. A concurrently modified
filesystem, hard links, special files, or mounts are not an OS isolation boundary.
Do not place secrets in a readable workspace or describe this policy as a shell
sandbox, tenant isolation, RBAC, or secure service authentication. Those require
separate controls. The Linux shell cleanup helper remains for legacy/internal
use; restricted workers cannot invoke it at all.

## End-to-end cancellation (#7)

- The executor is the single terminal owner. After the turn has joined all work,
  cancellation observed at its terminal decision wins over the turn's result and
  returns `context.Canceled` or `context.DeadlineExceeded`, with one `canceled`
  event and no error payload. Cancellation arriving after that decision cannot
  rewrite a completed success/failure. Repeated cancellation is harmless.
- The run context reaches both Ollama and OpenRouter HTTP streams, every provider
  channel send, tool dispatch, web authorization/search/fetch, and the bash
  timeout. A stopped channel consumer must cancel that context. Provider sends
  then unblock, response bodies close, and the adapter joins `Chat` before the
  executor emits the terminal. The adapter still drains legacy sends while
  suppressing output once cancellation is observed.
- Cancellation checks precede each model round and tool dispatch. Synchronous
  filesystem operations already in progress finish before the turn returns;
  completed writes are not rolled back. No later tool is dispatched after
  cancellation is observed. An in-flight send/operation may race the cancellation
  request, but there is no output or execution after the terminal outcome.
- The lower-level worker bash cleanup implementation is supported only on **Linux with readable `/proc` process/thread
  metadata and exclusive ownership of child waits**. The host must not ignore
  `SIGCHLD`, set `SA_NOCLDWAIT`, or externally reap the executor's children.
  The #8 worker profiles now reject bash on all platforms before this helper.
  The helper also rejects unsupported worker platforms before spawning. This does not disable ordinary model/file/web
  runs. Legacy `app.NewProvider` (CLI/TUI) keeps its existing bash compatibility on
  other platforms, without the worker's descendant-cleanup guarantee.
- Linux bash creates a dedicated process group and derives its existing 60-second
  timeout from the run context. Cancellation and natural shell exit converge on
  group cleanup, including same-group background children. The shell's PID remains
  reserved with `waitid(WNOWAIT)` until cleanup finishes, preventing accidental
  signaling of a recycled group. `SIGKILL` is followed by verification that the
  group has no live threads (including siblings of a zombie leader); `cmd.Wait`
  then reaps the owned shell and joins output copying. Descendant zombies are stopped, not executing; their OS/ancestor reaper
  owns their eventual collection. No process-global subreaper or signal handler is
  installed, and canceling one run does not signal another run's group.
- If cleanup verification becomes unavailable after launch, ownership is retained
  rather than emitting a false terminal. Uninterruptible kernel work can delay
  cancellation. Descendants deliberately escaping the group/session are outside
  this mechanism: bash still has the user's full permissions, not an OS sandbox.
  Durable cancellation and CLI/TUI cancel controls remain separate work.

Provider token/tool streaming remains separate in `types.ChatResponse`. Result
payloads, artifact export, persistence, API/auth, Bedrock,
and Jarvis integration remain deferred. Bash retains the user's full permissions.
No code from Development PR #3 is included.

Validation: `go test ./internal/domain`, `go test -race ./internal/domain`,
`go vet ./...`, `go test ./...`, and `go build ./...` (Go 1.24+).
Focused executor checks: `go test ./internal/executor ./internal/app ./internal/domain ./internal/provider`
and the same command with `-race`. Rollback of #6 removes the executor package,
`internal/app/executor*.go`, and its documentation updates; CLI/TUI behavior and
the original domain contract do not depend on it. There are no data migrations.

### Verification record (2026-10-23, Go 1.24.0 / darwin-arm64)

- Focused executor/app/domain/provider tests and their `-race` runs pass.
- `go vet ./...`, `go build ./...`, and `git diff --check` pass.
- `go test ./...` has the same failures before and after this slice:
  `internal/chat.TestBusLogsTimeToFirstTokenForThinkingChunk` and
  `internal/chat.TestBusDoesNotLogToolStatusAsFirstToken`, plus an
  `internal/database` test-link failure in the local clang/SDK combination
  (`MacOSX27.0.sdk`, unsupported `arm64e.x1` architecture in `.tbd` files).
  All other packages pass or have no tests. These baseline failures are not
  executor regressions; the full suite is not a pass.

### Cancellation verification record (2026-10-24, Go 1.27.3 / linux-amd64)

- Focused executor/app/domain/provider/web tests pass normally and under `-race`.
  Repeated cancellation/race cases include pre-start cancellation, blocked HTTP
  bodies and consumers, between-tool cancellation with retained writes, duplicate
  cancellation, completion races, independent runs, shell parent reaping and child
  stopping before the terminal, and zombie leaders with live sibling threads.
- `go vet ./...`, `go build ./...`, and `git diff --check` pass.
- `go test ./...` and `go test -race ./...` reproduce only the two existing
  `internal/chat` logging failures listed above, also reproduced on clean base
  `3565e43e547a9358aaa539bb850b748725b61b09` in this environment. All other packages
  pass or have no tests. The full suite is **not** a pass.
- The prior macOS database-linker failure does not reproduce on Linux; database
  tests pass here. Provider test binaries cross-compile for darwin-arm64 and
  windows-amd64; these binaries were not run, and no macOS runtime cleanup claim
  is made. Unsupported worker bash fails closed; legacy behavior stays separate.

### Tool-profile verification record (2026-10-24, Go 1.27.3 / linux-amd64)

- Focused provider/app/executor/domain/web tests pass under `-race -count=10`.
  Policy cases cover zero/unknown profiles, shell/mutation/unconfigured dispatch,
  caller mutation of tool names/parameters, injected per-turn tool definitions,
  model-originated forbidden calls for both providers, path/symlink escape,
  explicit web enablement and domain checks, and concurrent profile isolation.
- Full `go test -count=1 ./...` and `go test -race -count=1 ./...` reproduce only
  the two `internal/chat` logging failures above. Both also reproduce on clean
  main `5448848852f89746d1728d656391b220e7615006`; full suite is **not** a pass.
- `go vet ./...`, `go build ./...`, and `git diff --check` pass. Provider test
  binaries cross-compile for darwin-arm64 and windows-amd64; neither was run.
- No live provider calls, Kubernetes/container deployment tests, or new shell
  cleanup claims. The existing shell cancellation integration fixture now uses
  the explicit legacy provider because restricted worker profiles deny shell.
