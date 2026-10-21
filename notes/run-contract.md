# First worker slice: run lifecycle contract

`internal/domain/run.go` defines an additive, internal contract. It does **not**
change the current CLI, TUI, message bus, providers, or tool execution. Nothing
emits these events yet. This is not an authenticated or isolated worker service.

- A session groups turns using the existing `domain.Session.ID`. A run is one
  turn with a unique caller-assigned `RunID` and an explicit `SessionID`.
- `RunRequest` captures the prompt, provider/model, and absolute workspace root.
  Validation checks shape only: it does not check session existence, ID uniqueness,
  supported models, filesystem existence, symlinks, permissions, or containment.
  Callers must resolve the workspace before execution; a path is not a sandbox.
- Lifecycle events start at `queued` with sequence 1. They proceed to `running`
  and then `succeeded`, `failed`, or `canceled`. Setup failure and cancellation
  may terminate a queued run directly. Terminal states cannot transition again.
- `ValidateAfter` checks identity, contiguous sequence, nondecreasing timestamps,
  and transitions. Consumers must validate the complete stream in order and
  retain the last accepted event. It is not a durable store or replay mechanism.
- A failed event contains an error description. A canceled event means execution
  has stopped, not just that a stop was requested. Existing cancellation behavior
  does not yet satisfy this contract. No automatic retry or side-effect replay is
  implied. Error strings should be sanitized by a future producer before export.

Provider token/tool streaming remains in `types.ChatResponse` unchanged. Result
payloads, artifact export, an executor interface, isolation, cancellation wiring,
tool policy, persistence, API/auth, Bedrock, and Jarvis integration are deferred.
No code from the separate Development PR #3 is included.

Validation: `go test ./internal/domain`, `go test -race ./internal/domain`,
`go vet ./...`, `go test ./...`, and `go build ./...` (Go 1.24+).
Rollback is removal/revert of these new files; there are no runtime callers or
data migrations.
