# ADR 10: durable sessions and recovery (proposed)

Design only, 2026-10-25. [Issue #10](https://github.com/falbanese9484/howard/issues/10).
Baseline main: `0c6e67be2251034f8a239c1e49285183c14c76e5`. No database,
object store, schema migration, scheduler or recovery implementation is added.

## Contract boundary

[Current lifecycle](../run-contract.md) and `internal/domain/run.go` are canonical:
queued → running → succeeded/failed/canceled, with queued → failed/canceled allowed.
`internal/executor` owns terminal decisions; `internal/app/executor.go` constructs
fresh providers/history per run. SessionID currently does not restore a conversation.
`internal/domain/session.go` is insufficient for ordered structured tool history;
`internal/database/connection.go` is SQLite connection scaffolding, not a session
store. Proposed durable acceptance cannot be implemented by merely logging callbacks:
callbacks cannot return persistence errors or gate launch/completion today.

[#9](https://github.com/falbanese9484/howard/issues/9) owns vocabulary and admission:
owner scope = `(tenant_id, requester_id, agent_id)`, immutable for a session;
service principal is separate from verified requester binding; workspace_ref is
logical; tool_profile is a server-owned ceiling; audience_ref is a verified private
destination. This independent main-based design uses that vocabulary, not a second
service API. Later integration depends on both designs, not stacked branches.

## Decision and candidate comparison

Decision: PostgreSQL is authoritative for all platform domain records and transactions. A managed PostgreSQL
service (e.g. RDS PostgreSQL or company-standard managed PostgreSQL) is the preferred
production category; provider, region, residency, availability/backup targets,
connection limits, cost and operator approval remain undecided. SQLite is useful
locally but a worker-local file cannot satisfy pod replacement without a separate
volume/recovery architecture. No provider is provisioned or selected for company use.

Durable artifact bytes may live in private object storage with PostgreSQL metadata;
this is an MVP capability delivered in a separate implementation unit. For example, S3 is an evaluated candidate, with the company-standard equivalent also
acceptable after consistency/versioning review. PostgreSQL inline bounded text
avoids cross-store commits for the first slice. Object stores do not replace
relational acceptance/completion transactions. S3 has strong read-after-write
consistency, but cannot atomically commit a PostgreSQL row and object; versioned
deletes may leave old versions. These require explicit reconciliation/deletion.
Sources: [PostgreSQL isolation](https://www.postgresql.org/docs/current/transaction-iso.html),
[partial unique indexes](https://www.postgresql.org/docs/current/indexes-partial.html),
[S3 consistency](https://docs.aws.amazon.com/AmazonS3/latest/userguide/Welcome.html),
[version deletion](https://docs.aws.amazon.com/AmazonS3/latest/userguide/DeletingObjectVersions.html).

## Platform domain ownership and optional caches

PostgreSQL also owns `agents` and immutable `agent_config_versions` (scope, capability
ceiling, context references, budget policy), tool/skill/MCP registry descriptors and
versions, context references/provenance, users, roles/permissions and assignments,
interface identity bindings (issuer/subject, verification expiry/revocation version),
audit decisions and single-use action-bound approvals. These are proposed domain
families, not executed DDL. Company RBAC/API provider remains unknown: authoritative
role mapping/synchronization and admin bootstrap are integration decisions, not a
reason to put platform records outside PostgreSQL. Credentials/OTP/token plaintext
are excluded; only restricted secret-manager references are stored. Raw content
bytes may use object storage, with authoritative ownership/version/availability
metadata in PostgreSQL. The role-to-agent intersection and management CRUD API/UI
are owned by [paired PR #20](https://github.com/falbanese9484/howard/pull/20).

All management changes atomically persist immutable config version, current pointer
and sanitized audit; execution pins version refs and rechecks current restrictions.
Agent identity/config survives pod replacement independently of a conversation or
provider object. Approval consumption/expiry is durable; neither restart nor a
restored history reactivates consumed/expired approval. Version and FK checks cover
control-plane records as well as run history; no orphan binding/config references.

Redis is **not required for MVP** and never owns correctness, sessions, approvals,
idempotency or active-run locks. Only evaluate it for a measured cache workload.
One possible candidate is non-secret registry descriptor reads: tenant/version-keyed,
invalidated on committed update/disable, bounded TTL, authorization rechecked against
PostgreSQL before use, protected transport/access and no cross-tenant cache results.
Cache outage falls back to PostgreSQL within budgets, otherwise fails closed; cache
staleness never grants execution. No cache is added or configured in this design.

## Minimal records and constraints (conceptual, not DDL)

| Record | Required fields/invariants |
| --- | --- |
| session | owner scope, session_id, revision, workspace_ref/checkpoint_ref, audience_ref, blocked reason; immutable owner |
| run | owner scope + RunID unique forever/tombstone, session FK, submission version/digest, accepted revision, status, active, worker generation, cancel request, recovery metadata, resolved backend/model/settings and credential **reference** |
| message | session/run FK, strictly increasing session ordinal, role, typed content parts, resource scope, context eligibility; user/assistant/tool roles are data, never authority |
| tool intent | run/call ID unique, assistant message FK, canonical action/resource, protected arguments or reference, digest, policy/approval decision refs, generation, dispatch marker |
| tool result | intent FK unique, outcome known/unknown, protected content/ref, stopped evidence ref; distinguish tool error from missing result |
| event | run FK, sequence unique, #4 event, contiguous sequence/time/transition checked under run lock |
| artifact metadata | owner/session/run FK, opaque object/version key, digest/size/media type, resource/audience scope, availability; no local path or public URL |

Execution references include owner scope in foreign keys; no cross-owner association.
Control-plane records below are tenant-scoped and can be referenced by several
authorized users; sharing agent configuration never shares confidential history.
Session ID unique within owner; RunID unique across that owner's sessions. A partial
unique index on `(owner scope, session_id) WHERE active` enforces one active run;
active includes recovery-blocked runs even if lease expired. Session-level blocked
state also denies new work after terminalization until uncertainty/workspace review.
Different sessions lock different rows. Overlapping mutable workspace/resource
claims require separate exclusion; new session IDs cannot bypass unresolved effects.
No distributed scheduling is implied by database uniqueness.

Canonical conversation uses ordered text, tool-call and tool-result parts with
stable call IDs and exact correlation. Keep complete pairs together when selecting
context; retained history is not automatically sent to a model. Snapshot selected
message IDs, policy version and context-selection version per run. Reauthorize
resource access, history and private audience before restoration, context selection,
result/artifact reads and delivery. Revoked content and dependent summaries are
withheld; no cross-requester cache or shared agent memory. Unknown policy fails
closed. Raw reasoning, verification codes and credentials are never retained.
Provider-specific reasoning/signatures, unsupported multimodal parts or unpaired
calls must block context translation, not be coerced into plain text. Current
providers need a separately reviewed canonical-history adapter; changing model does
not imply faithful translation. Credential refs resolve at execution under current
policy; no captured token can revive a revoked grant. Expired or consumed approvals cannot be replayed after restart; fresh identity
and current per-action authorization are required. Identity TTL is not action
approval (see #9 and the [authorization idea](https://github.com/falbanese9484/knowledgebase/blob/main/ideas/requester-aware-agent-authorization.md)).

## Transaction boundaries and retries

**Accept:** after trusted #9 checks, look up existing owner+RunID before
capacity/revision/busy/readiness checks so an authorized identical retry succeeds
even at capacity or on a blocked session. For absent keys reserve bounded executor
capacity, then lock the session row and recheck owner+RunID in the transaction.
Release an unused reservation if a concurrent acceptance won. For genuinely new
work check blocked/workspace readiness and expected revision. Compare versioned canonical semantic request bytes/digest (exact
prompt, session, selectors, expected revision; no auth/timestamps). Conflict = 409.
Recheck current access even for a duplicate. For new work, atomically append user
message, create active run with resolved immutable settings, append queued sequence
1, increment session revision. Commit before acknowledging or launching. Rollback
releases capacity and creates no accepted run. Unknown commit result: query same
owner+RunID; retry same key, never invent a replacement. Serialization/deadlock
retry may repeat only this database transaction, never execution. Tombstones after
retention forbid replay while avoiding retention of prompt content.

**Incremental durability:** accepted input, queued/running transitions, completed
structured messages/tool boundaries and saved artifact metadata commit during work,
not only at shutdown. Persist complete assistant/tool-call messages before their
intents; persist correlated results before the next round. Terminal completion
adds only remaining final messages. Token deltas remain ephemeral; checkpoints
are explicit durable objects, never assumed from scratch files.

**Start and tools:** persist running before provider launch. Persist intent and
its current policy decision before dispatch; record dispatch marker before invoking
tool. Neither marker proves an effect occurred. Check cancellation, binding freshness,
current policy and generation before each call. There is no atomic transaction
with an external side effect. Failed persistence means stop before further dispatch;
uncertain commit means block, not retry tool. Results are correlated and committed
before further model rounds; missing results remain explicit uncertainty.

**Complete:** only after all execution/output joins, lock run/session and verify
current generation; atomically append validated final structured messages/results,
append exactly one terminal event, record outcome, set active=false and increment
session revision. Tool results already committed are referenced, not duplicated.
The terminal outcome is not delivered until commit. Known terminal commit with lost
response is readable; unknown commit queries by run key. If completion cannot be
committed, retain last durable state and recovery block. Never manufacture success
from partial text. No success response or final artifact delivery before this commit.

Lifecycle event insertion uses run-row locking and `ValidateAfter`; unique sequence
alone cannot detect gaps. Delivery deduplicates equal sequence+content, refetches
on a gap, and quarantines conflicting duplicates/time regression. Reconnect returns
only authorized committed lifecycle/history. Token deltas are ephemeral and may all
be lost; no token offsets or exactly-once output delivery guarantee. Event replay
never invokes tools. Session revisions are concurrency controls, event sequences
are per-run ordering; neither is a token cursor.

## Crash decision table

Every row prohibits automatic tool replay or automatic provider continuation,
including reads (their external behavior is not assumed pure).

| Crash point | Durable evidence / decision |
| --- | --- |
| Before acceptance | No record after query: same-key submit may accept; no work launched |
| Acceptance commit/ack lost | Query owner+RunID; return existing or retry transaction only |
| queued persisted, before start | Block recovery; after proving no execution survives, fail interrupted run; explicit new run required |
| running persisted, before intent | Partial output may be lost; prove old work stopped, fail interrupted run |
| intent committed, before dispatch marker | Intent alone is not completion; conservative unknown if ordering cannot be proved; no replay |
| marker committed, before side effect | Indistinguishable from effect without result after crash: unknown |
| side effect completed, before result | Unknown, surface possible effect; inspect external evidence manually |
| result committed, before terminal | Preserve known result but run incomplete; do not rerun remaining tools/model |
| terminal transaction rolled back/unknown | Query; if no terminal, block and reconcile after stop proof |
| terminal committed, response lost | Return existing terminal/history; no execution |
| lease expired, old worker alive | Keep active/session/resource blocked; reject replacement dispatch |
| missing/corrupt record/object | Quarantine affected record; no fabricated history/result or silent skip |

**Stop and fencing:** initially one active executor instance, no automatic failover.
A durable monotonic worker generation fences store mutations via transactional
comparison. Checking generation before dispatch reduces stale launches but is not
an external-effect fence: a worker can pause after checking, then issue a call
after revocation. A DB fence neither prevents that race nor stops an already-issued
shell/network request. Only target-enforced generations or a proven atomic dispatch
gate could supply that stronger guarantee. Operator-controlled
predecessor termination/join evidence is required before clearing active claims or
starting conflicting work; process/pod loss or lease expiry is insufficient. If
termination cannot be established, retain block indefinitely and expose uncertainty.
For future multi-worker execution, require atomic generation claims, resource-level
fences enforced by effect targets or proven process isolation, and verified takeover;
do not add a lease-based scheduler first. External requests accepted before stop
may still finish remotely; unresolved effects keep resources blocked pending
reconciliation even after local stop evidence.

#4 has no interrupted/unknown lifecycle state. Proposed recovery coordinator, after
stop proof and generation fencing, may append `failed` with fixed error
`execution interrupted; outcome may be unknown`, plus separate recovery metadata
`tool_outcome:unknown` and `session_blocked:true`. It does not rewrite terminal runs.
Before proof, leave queued/running as last durable lifecycle with recovery blocked.
This requires explicit later terminal-ownership coordination with #6/#7; no current
callback behavior supplies it. `canceled` requires actual cancellation and stopped
execution evidence, never just cancellation request or process loss. Human-reviewed
resource reconciliation and workspace readiness are required before unblocking;
unknown effects are never relabeled as absent. Full terminal history remains valid
under #4 without pretending it contains enough recovery information by itself.

## Workspace and content continuity

Choose **no automatic filesystem restoration** initially. Stable workspace_ref maps
to a server-approved immutable source revision plus optional checkpoint metadata;
worker paths are never durable identity. Completed history/results stored inline
remain readable on a replacement without provider objects or local disk. Running
work is blocked as above. A missing workspace blocks any new execution even if
conversation history is available.

Fresh checkout plus policy-approved persisted diff/checkpoint is portable and
reviewable but needs explicit rules for untracked/ignored/generated files, secrets,
size and verification. Persistent volume/session storage preserves more files but
adds attachment, consistency, isolation and backup concerns; it does not prove old
workers stopped. Defer both mechanisms. An operator may authorize a fresh workspace
from a verified source revision for a new run only after reviewing lost modifications
and outstanding effects; no assumption of identical files. Fixtures show read-only
history, unknown run outcome and unavailable working files separately. #12 alone
owns selected artifact export/transfer; no automatic workspace export here.

Object publication later: write immutable scoped object, verify digest/size, then
commit metadata as available. Failed DB commit leaves an unreferenced object for
bounded garbage collection; missing object or digest mismatch marks unavailable,
never follows arbitrary URLs. Essential context missing => block restoration;
optional artifact missing => readable text with explicit artifact-unavailable marker.

## Temporary worker disk and pressure handling

Each worker pod has temporary scratch storage. Isolate directories and access by
owner/session/run inside the pod; one pod does not imply one shared working folder.
Filesystem staging belongs to a narrowly scoped host provisioner, never a new model
write/shell grant. Invoke a skill/tool/MCP only if needed, and still enforce the
current role/agent/resource intersection; loading a skill grants no capability.
No EFS dependency is introduced just because cluster integration exists. Existing
EFS configuration is neither changed nor removed; shared disk is not durability.

Require per-run/session and pod aggregate byte/inode budgets, bounded downloads
(including expanded archive size/count), output/temp limits, rotated bounded logs,
cleanup of completed/abandoned scratch after stop proof, and reserved admission
headroom for active work and cleanup. Budget values are workload/ops review inputs,
not an invented 10 GiB guarantee. Check/reserve headroom before accepting new work,
recheck during writes/downloads and stop before exceeding a budget. Never delete
another active run's files to make space. Existing-key reads/retries do not need a
scratch reservation. Checkpoint/export upload also needs bounded staging space.

Disk-full can cause a write failure while the process is still alive; eviction is
a different event. On ENOSPC/quota/download-limit failure, stop new tool dispatch,
join current work, persist a sanitized failed terminal when safe; if persistence or
stop evidence is unavailable keep the last durable state and recovery block. Local
log failure must not erase durable run state or trigger retries; protected audit
commit failure denies further actions. Eviction/kill follows the existing unknown-
outcome protocol; neither guarantees the remote effect stopped. Cleanup failure
keeps the pod out of new admission until headroom is verified. No graceful-shutdown
hook is relied on to save accepted conversation or artifact metadata.

Acceptance fixture: kill a worker mid-task after user/assistant/tool boundaries and
one artifact's verified bytes+metadata commit. Replacement loads the same durable
agent/config, authorized conversation and saved artifact without the old provider or
disk. Saved-artifact readability here means authorized inspection of committed
content; it is not successful run completion or #12 export/delivery. Unfinished
scratch and uncommitted token output are lost unless an explicit
checkpoint was committed and verified. An unresolved external tool effect remains
unknown and is never automatically rerun; further work stays blocked until required
stop/reconciliation/workspace gates pass. This is restoration of durable records,
not a claim to restore every file or resume execution.

## Retention, schema and next review

Before real data, company owners must set content/audit/object/tombstone retention,
residency, encryption and backup deletion windows. Default no real confidential
content until approved. Delete access immediately with a tombstone; asynchronously
purge content, object versions, derived indexes/caches and backup copies according
to documented retention. Legal holds must be explicit. Audit IDs/reason codes only;
protected content and credential refs never flow to general logs. Unknown schema
versions, broken FK/call correlation or sequence gaps quarantine execution.

Version records and request canonicalization. Future migrations use expand/verify/
contract with backup/restore rehearsal. Old readers must reject unsupported versions.
Rollback application only while schema remains backward compatible; otherwise stop
writes and restore a reviewed snapshot with reconciliation of subsequent effects.
Never roll back durable run identity so a completed action can replay. This design
rolls back by removing the directory; no schema exists to migrate.

Smallest NEXT implementation proposal: one PostgreSQL store-only PR with `Accept`,
`GetRun`, `ReadEvents`, `ReadHistory`, `RecordIntent/Result`, `Complete` and explicit
block/reconcile operations, owner scope required on every method. Bounded inline
content, synthetic/fake callers, no object adapter, provider, service, credentials,
queue or automatic recovery. Test transaction rollback, duplicate/concurrent
acceptance, same-session constraint and independent sessions against an explicitly
approved disposable test DB; no live DB in this design phase. PostgreSQL schema/interface review precede authorization of that PR. After its review, separately
approve executor persistence gates/history adapter and #9 admission integration;
only then consider a single-executor synthetic service. A store fake cannot certify
SQL atomicity. The full MVP additionally includes the platform-domain store, admin
CRUD API/small UI, registry/role enforcement, temporary-disk budgets, durable artifact
bytes and verified interface integration described in #9; these are follow-up units,
not omissions from MVP or additions to this implementation-free PR. Production
identity/RBAC, retention/residency and storage budgets require owner review. #11
provider and #12 export remain separate contracts; this does not redefine delivery.

[MVP restart/disk examples](mvp-fixtures.json) and [fixtures.json](fixtures.json)
supply records and crash cases;
[fixture_test.go](fixture_test.go) checks IDs, cases and #4 event compatibility.
`go test ./notes/design-10` proves fixture integrity, not SQL constraints, fencing,
recovery correctness or cloud readiness. No live provider/DB tests are included.

Validation (2026-10-25, darwin-arm64): `go test ./notes/design-10 ./internal/domain`
passes with a writable temporary Go cache; `git diff --check` and local relative
links pass. Full runtime suite, SQL/infrastructure, auth integration and live
provider calls were not run for this documentation/fixture-only change.
