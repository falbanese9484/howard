# ADR 9: trusted service boundary (proposed)

Design only, 2026-10-25. [Issue #9](https://github.com/falbanese9484/howard/issues/9).
Baseline: main `0c6e67be2251034f8a239c1e49285183c14c76e5`.
The central enterprise governance direction retains personal OpenCode and existing
CLI/TUI entry points. No listener, credentials, RBAC integration or runtime change.

## Evidence and vocabulary

[Current contract](../run-contract.md), `internal/domain/run.go`,
`internal/executor/executor.go`, `internal/app/executor.go` and
`internal/provider/tool_profile.go` implement in-process lifecycle/cancellation
and immutable profile ceilings. They do not authenticate requesters, persist
sessions, recheck enterprise policy, or isolate tenants at the OS boundary.
SessionID currently only correlates events. Closed, unmerged Development PR #3's loopback web UI
shares a conversation and lacks this ownership/admission boundary; exposing it
cannot implement this design. Never wrap the legacy provider as a service.

Shared vocabulary is owned here; [#10](https://github.com/falbanese9484/howard/issues/10)
owns durable acceptance/recovery. Both designs branch independently from main;
reviewing either does not require merging the other. Their later integration
requires agreement on this vocabulary, not two competing request contracts.

| Term | Trusted meaning |
| --- | --- |
| service principal | Authenticated calling workload, not a human |
| owner scope | Immutable `(tenant_id, requester_id, agent_id)` partition |
| requester binding | Backend-verified human linkage, expiry and revocation version |
| workspace_ref | Server-owned logical resource; resolved local path is ephemeral |
| tool_profile | Server-selected capability ceiling, initially `read-only` |
| resource/action | Canonical target and operation evaluated outside model reasoning |
| audience_ref | Verified private output destination for the same requester |
| RunID | Client idempotency key unique within owner scope, including completed runs |

## Decision: one internal caller first

Select one opaque bearer service credential over TLS for the first internal caller,
resolved by a server-side credential reference to one service principal. This is
minimal single-caller authentication, not enterprise SSO. No actual secret or
configuration is supplied here. No token in URL, prompt, body, history or audit.
A future implementation must use constant-time credential verification, reject
unrecognized/expired/revoked credentials, bound request sizes and rate-limit failed
auth; authenticated network access alone grants no tools/data. TLS termination,
secret storage/rotation and trusted proxy configuration require separate review.

For the first synthetic pilot, the authenticated service maps to one fixed synthetic
requester/agent/tenant, workspace and private audience. Caller body/header claims
of human identity are rejected; prompts are untrusted data. Multi-human traffic is
denied until a company-approved identity-link adapter can verify channel identity
and resolve a binding. A shared service credential must never authorize arbitrary
`requester_id` values supplied by the gateway. Unknown mappings fail closed.

The enterprise RBAC provider/API and owner are UNKNOWN; Okta is not selected.
Required future adapter contract: `VerifyBinding(service, channel_subject)` returns
an issuer-verified tenant/requester/binding ID, verified_at, expires_at and revocation
version; `Authorize(binding, agent, action, canonical_resource, audience)` returns
allow/deny plus decision/version/freshness bounds. Authenticity, service delegation
scope, subject correlation and revocation semantics require the company's contract.
No adapter implementation or fallback local role directory is proposed.

## Enforcement and information flow

At admission and again immediately before each tool dispatch, evaluate current
requester rights intersected with agent ceiling, workspace/resource constraints
and current context/audience policy. Resolve workspace/profile/model from an
allowlist owned by the service; never accept an absolute path or model-selected
profile. Canonicalize resource identifiers before authorization; reject unsupported
actions/resources. Current #8 checks remain a second ceiling, not enterprise RBAC.
Initial profile disables network, shell and mutations. Existing file containment is
not atomic or tenant-grade isolation; only controlled synthetic workspaces qualify
until an independently reviewed filesystem/OS boundary exists.

Partition history, model context, retrieval, caches, tool results and artifact
metadata by owner scope plus resource-policy version. No shared confidential agent
memory or cross-requester session. Recheck retained source permissions before
context selection, reads and output delivery; if unavailable/revoked, suppress
content and block execution. Filtering must remove dependent summaries/tool pairs,
not just original documents. Initial audience is private and bound to the same
requester; shared-channel/multi-participant delivery is denied until all readers
can be authorized. A shared Slack thread is not proof of a safe audience.

Identity verification expires at the backend binding TTL (company policy must set
the value); expiry/revocation requires re-verification. It never grants action
approval. A future high-impact action would additionally require a single-use,
expiring approval bound to owner, agent, run, canonical action/resource and exact
parameter digest; changed inputs invalidate approval. Recheck policy and consume
approval atomically with intent authorization. Writes remain disabled here even
with such an approval. Verification/OTP challenges belong in a dedicated trusted
surface; never in prompts, tool transcripts or logs.

Revocation or policy unavailability denies new dispatches, context reads and output,
requests cancellation of in-flight work, and withholds sensitive buffered output.
Already-started effects cannot be undone; wait for #7 stop evidence, otherwise use
#10's blocked/uncertain recovery metadata. Do not assert immediate revocation of
bytes already delivered or credentials already in flight. Every reconnect rechecks
service, binding, ownership, policy and audience; credential rotation preserves
owner-scoped idempotency only through an authorized mapping to the same owner.

## Proposed request/response contract

HTTP-shaped fixtures are illustrative; no routes exist. Operations: submit
`POST /v1/sessions/{session}/runs`, status `GET /v1/runs/{run}`, ordered lifecycle
`GET /v1/runs/{run}/events?after_sequence=N`, cancel `POST /v1/runs/{run}/cancel`.
Every operation resolves owner scope before lookup, including duplicates/cancel.
Cross-owner and absent objects both return sanitized 404, never existence hints.
Absent/invalid service credentials: 401. Own disallowed workspace/profile: 403.
Invalid shape/unknown authority fields: 400. Stale revision/conflicting RunID or
session busy: 409. Capacity: 429 with bounded retry guidance. Policy unavailable:
503 without execution. No raw provider errors or credentials in responses.

Submit body: `run_id`, `prompt`, `expected_revision`, `workspace_ref`,
`profile_ref`, `model_ref`; no principal or path. The refs are requested selectors
that must equal permitted server mappings. Response after #10 acceptance commits:
202 `{run_id, session_id, status:queued, accepted_revision, durable:true}`.
Current executor's queued callback alone is NOT this durability guarantee.
Identical retry returns 200 existing run; conflicting payload returns 409. Compare
canonical versioned semantic bytes (exact prompt, session, selectors, revision),
not request timestamps/credentials. Recheck authorization before returning either.
RunID uniqueness is owner-wide; reuse against another session conflicts. A deleted
run retains an idempotency tombstone; content expiry cannot permit replay.

Status returns last committed lifecycle and separate `recovery`/`cancel_requested`
metadata, never an invented lifecycle state. Events retain #4 IDs, contiguous
sequence and timestamps; reconnect replays committed events only. Duplicates with
identical sequence/content are ignored; gaps require refetch, conflicting duplicates
block consumption. Tokens are ephemeral, never promised replay.

Cancel persists a request flag then returns 202 `{cancel_requested:true,
terminal:false}` while active. This acknowledges intent, not stopped execution.
Repeated cancel is idempotent. Already terminal returns 200 existing outcome;
completion may win the race. Only executor stop/join proof permits `canceled`.
A lost worker is not a canceled run. #10 may finalize a stopped interrupted run as
`failed` with uncertainty metadata, never claim the tool did not take effect.

Initial execution: one process/host, one active executor instance, configurable
finite cap (proposed 4 active runs), no waiting queue, one active run per session.
Independent sessions can execute concurrently within the cap. Authenticate and check for an existing owned RunID before capacity/revision/busy
checks, so saturation cannot reject an authorized identical retry. Reserve capacity
only for new work, recheck uniqueness transactionally, and release unused/failed
reservations; #10 database constraints are
still required. Another instance may serve authorized reads/retries against the
same store but cannot claim execution. Restarts must prove the predecessor stopped;
lease timeout alone never frees a session or overlapping workspace resources.

Audit only opaque owner/service/binding/run/session IDs, canonical safe resource
IDs, action/decision reason codes, policy version, sequence, timing and outcome.
No prompt, token, OTP, raw path/URL, tool args/results, provider error, or credential
reference in general logs; protected content records use separate access/retention.
Use keyed digests where correlation is needed, not public hashes of sensitive text.
Audit access itself is authorized. #12 governs export; #11 governs provider work.

## Review gate and smallest later slice

First review #9/#10 vocabulary, private-audience restriction, recovery failure
mapping, and backend choice. Next authorize #10's store-only slice (no service or
provider calls). Then separately review an in-memory admission/auth-policy adapter
with fake identity/policy and these fixtures; no listener in that slice. Only after
company identity/RBAC contract, TTL/revocation owner, workload secret/TLS lifecycle,
workspace isolation and durable executor integration are agreed should a narrow
single-caller synthetic read-only end-to-end service slice be proposed. No personal
OpenCode migration. All future implementation/deployment needs new approval.

See [fixtures.json](fixtures.json) and [fixture_test.go](fixture_test.go).
Run `go test ./notes/design-9`; this validates fixture integrity and existing
lifecycle compatibility, not an implemented auth service or security assurance.
Rollback: remove this directory; no data/runtime changes.

Sources: [requester-aware idea](https://github.com/falbanese9484/knowledgebase/blob/main/ideas/requester-aware-agent-authorization.md),
issues [#5](https://github.com/falbanese9484/howard/issues/5),
[#6](https://github.com/falbanese9484/howard/issues/6),
[#7](https://github.com/falbanese9484/howard/issues/7),
[#8](https://github.com/falbanese9484/howard/issues/8).

Validation (2026-10-25, darwin-arm64): `go test ./notes/design-9 ./internal/domain`
passes with a writable temporary Go cache; `git diff --check` and local relative
links pass. Full runtime suite, SQL/infrastructure, auth integration and live
provider calls were not run for this documentation/fixture-only change.
