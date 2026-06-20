# Session: Project Review & SSRF Discussion

A capture of a review session covering an overall read of the **Howard**
agent-harness project and a deeper dive into the SSRF exposure in the
`fetch_url` tool. Private system/developer instructions are intentionally
excluded.

## Project

- Module: `github.com/falbanese9484/agent-harness`
- Howard: a minimal local AI coding-agent harness in Go (Bubble Tea TUI,
  pluggable Ollama/OpenRouter providers, sandboxed file tools, optional web
  tooling).

---

## Conversation Transcript

### User

> Take a look around this project and let me know what you think

### Assistant — Overall Review

This is a well-built Go coding-agent harness. Highlights:

**Strengths**
- Clean layering: `types.Provider` is a tight interface, `app.NewProvider` is
  the single wiring point, and the web service degrades safely (`Enabled()` →
  false fails closed).
- Careful sandbox containment: `resolveNewProjectPath` walks up to the nearest
  *existing* parent and re-resolves symlinks before allowing a create, closing
  the symlink-through-to-outside write hole. `edit_file`'s exact-and-unique
  match requirement is a smart, simple safety rail.
- The `notes/review-checklist.md` self-review is an asset — correctly triaged
  into correctness / robustness / polish, with `//TODO:` comments pointing back
  to it.

**Things to push on (beyond the existing checklist)**
1. **SSRF in `fetch_url`** — the allowlist gates the hostname, but `web.Fetch`
   uses `http.DefaultClient`, which follows redirects without re-checking the
   target. Higher priority than the checklist implies.
2. **Run `go test -race ./...` now** — the `o.messages`-mutated-from-goroutine
   item should be ground-truthed rather than deferred. Any current safety is a
   timing coincidence of the TUI's `isResponding` flag, not a guarantee.
3. **Dead cgo code** (`internal/database`, `internal/domain`) — `mattn/go-sqlite3`
   is cgo and changes the build/cross-compile story for code with no callers.
   Cuts against the "small and understandable" mission. Wire it up soon or
   pull it back out.

Suggested next hour: (1) `go test -race ./...`, (2) close the redirect/SSRF gap
in `Fetch`, (3) decide the fate of the cgo/database code.

### User

> SSRF?

### Assistant — SSRF Deep Dive

**What it is.** Server-Side Request Forgery: a feature makes HTTP requests to a
URL the caller controls (here, the *model*). The attacker can't reach internal
resources directly, but the server can — so they trick the server into making
the request on their behalf. The server is a "confused deputy."

**Why `fetch_url` is the classic target.** The model chooses the URL and the
server process makes the request. That process can reach things outsiders
can't:
- `http://localhost:11434` — the local Ollama instance.
- `http://169.254.169.254/latest/meta-data/...` — cloud metadata endpoint,
  often serving temporary IAM credentials.
- `http://192.168.x.x` / `10.x.x.x` — internal/admin services with no auth.
- `file://`, `gopher://`, etc. on permissive clients.

**The existing defense.** `AuthorizeHost` checks the hostname against
`web.domain_allowlist` and fails closed. This stops the *direct* attack.

**The gap: redirects bypass the check.** `executeFetchURL` authorizes the host,
then hands off to `web.Fetch`, which uses `http.DefaultClient.Do` — and the
default client follows redirects (up to 10) with no re-authorization:

1. Authorize an allowlisted host (or one with an open redirect). ✅
2. `Fetch` does the GET.
3. Server responds `302 Location: http://169.254.169.254/...`.
4. `DefaultClient` follows it — no second check.
5. Metadata/credentials land in `result.Body` → returned to the model → printed.

Front door locked; redirect is the open window next to it.

**Fix — the hook.** `http.Client.CheckRedirect` fires before each hop and can
refuse by returning an error:

```go
client := &http.Client{
    CheckRedirect: func(req *http.Request, via []*http.Request) error {
        // re-authorize req.URL.Host here; return an error to refuse the hop
    },
}
```

Open design question: `Fetch` is a free function that knows nothing about the
`Service`/sandbox. Decide how it gets access — build a client with a
`CheckRedirect` closure that calls back into the sandbox, or pass the sandbox in
so the allowlist check lives in one place.

**Deeper cousin: DNS rebinding (TOCTOU).** The check resolves a hostname, but
the actual TCP connection does a separate DNS lookup. An attacker's domain can
resolve to a public IP at check time and a private IP at connect time. Robust
fix: validate at the IP layer — reject private/link-local/loopback ranges
(`net.IP.IsPrivate()`, `IsLoopback()`, `IsLinkLocalUnicast()`) and pin the
connection to the validated IP via a custom `DialContext`.

**Priority order**
1. Redirects — `CheckRedirect` re-running `AuthorizeHost`. Biggest bang,
   smallest change.
2. Block private/link-local IP ranges even for allowlisted hosts (defense in
   depth against the metadata endpoint).
3. DNS rebinding / dial pinning — only if facing real users on real cloud boxes.

### User

> Please take this chat and save it in the notes directory as markdown

### Assistant

Saved this transcript as `notes/ssrf-review-session.md` (a new file, leaving the
existing `notes/conversation-context.md` untouched).

---

## Action Items

- [ ] Re-run `AuthorizeHost` on every redirect hop via `http.Client.CheckRedirect`.
- [ ] Reject private / link-local / loopback IPs even for allowlisted hosts.
- [ ] Consider dial pinning to mitigate DNS rebinding (if internet-facing).
- [ ] Run `go test -race ./...` to ground-truth the provider-state race.
- [ ] Decide the fate of the unwired cgo `internal/database` / `internal/domain` code.
