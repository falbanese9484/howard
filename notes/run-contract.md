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
  has stopped, not just that a stop was requested. Existing cancellation behavior
  does not yet satisfy this contract. No automatic retry or side-effect replay is
  implied. The executor exports fixed, sanitized failure strings.

## In-process executor (#6)

Construct with `app.NewExecutor(cfg, log)`, then synchronously call
`Execute(ctx, request, executor.Observer{Lifecycle: ..., Output: ...})`.

- Configuration is snapshotted at construction. Each accepted run constructs a
  fresh provider, conversation, and isolated tool execution state through
  `app.NewProvider`. The request selects model and workspace; its provider must
  match the configured backend so credentials do not cross providers.
- `SessionID` is correlation metadata only. Reusing it does not restore history.
  Lifecycle and output callbacks belong to a single call; independent runs may
  invoke their callbacks concurrently.
- Invalid requests, already-canceled contexts, and duplicate active RunIDs are
  rejected without events or work. A nil context is an invalid request. Uniqueness
  is per executor instance and in memory; completed IDs may be reused. The ID is
  held through the terminal callback and released before `Execute` returns.
- Accepted runs emit `queued`, then `running`, then exactly one `succeeded` or
  `failed` event. Setup failure goes directly from `queued` to `failed`. Context
  cancellation observed after acceptance but before launch also fails from
  `queued`. Sequence starts at one and timestamps are nondecreasing.
- `Turn.Run` must return only when execution and output have stopped. Factories
  construct fresh state and must not launch turns. The provider adapter drains
  text/tool output and waits for `Chat` to return, even after a done/error signal.
  Missing completion is a failure. It does not finalize just because the caller
  context was canceled. Callbacks must return promptly and must not panic;
  implementations must not retain callbacks or emit after returning.
- Provider errors become `provider execution failed`; setup errors become
  `execution setup failed`. Raw errors are not wrapped into returned errors or
  lifecycle metadata. `ResponseKindError` chunks are suppressed and treated as
  failures. Assistant/tool output is still ordinary, potentially sensitive
  provider content, not a sanitized result/artifact format.
- Caller context reaches the provider HTTP path, but cancellation of tools,
  process trees and blocked output consumers is not reliable yet. No `canceled`
  event is emitted. Once launched, the actual turn result determines success or
  failure, even if cancellation was requested meanwhile. End-to-end cancellation
  is the separate #7 slice.

Provider token/tool streaming remains separate in `types.ChatResponse`. Result
payloads, artifact export, restricted tool policy, persistence, API/auth, Bedrock,
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
