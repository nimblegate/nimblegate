# Upstream adapters: author guide

Adapters are the host-specific shims between the gateway's orchestrator and the various git-host APIs (Gitea, GitHub, future GitLab / Bitbucket / on-prem). All adapters implement one interface; the orchestrator is host-agnostic.

This guide is for someone adding a new adapter. For the design rationale, see [`docs/superpowers/specs/2026-06-04-auto-pr-and-webhook-design.md`](superpowers/specs/2026-06-04-auto-pr-and-webhook-design.md) §8.

---

## The interface

```go
package upstream

type Upstream interface {
    Name() string

    FindPRForRef(ctx context.Context, repo, ref string) (*PullRequest, error)
    ReadPRPeople(ctx context.Context, pr *PullRequest) (PRPeople, error)

    FindStickyComment(ctx context.Context, pr *PullRequest, commentID string) (*Comment, error)
    ScanForMarker(ctx context.Context, pr *PullRequest, marker string) (*Comment, error)

    CreateComment(ctx context.Context, pr *PullRequest, body string) (*Comment, error)
    UpdateComment(ctx context.Context, c *Comment, body string) error
}
```

Source: `internal/gateway/upstream/upstream.go`. Seven methods cover everything the notification rail needs: finding the PR, reading its people, locating + creating + updating the sticky comment, and recovering a sticky by marker scan when the stored ID is lost.

## File layout

Drop new adapters into `internal/gateway/upstream/<host>.go` alongside the existing `gitea.go` and `github.go`. Each adapter is one file plus its test file:

```
internal/gateway/upstream/
├── upstream.go          # interface + shared types
├── registry.go          # adapter registry + URL→adapter lookup
├── stub.go              # in-memory fake for orchestrator tests
├── gitea.go             # Gitea adapter
├── github.go            # GitHub adapter
├── gitlab.go            # ← your new adapter goes here
└── scenario_test.go     # shared scenario suite (run by every adapter)
```

## Method semantics

### `FindPRForRef(ctx, repo, ref)`

Return the PR open on the given ref. Two success outcomes:

- `(*PullRequest, nil)`: PR found
- `(nil, nil)`: no PR on the ref (the gateway treats this as the no-PR fallback; webhook still fires, comment is skipped)

Return `(nil, err)` only on transport / auth failure. Use `fmt.Errorf("%w: ...", upstream.ErrTransient, ...)` for retryable failures (5xx, network) and `upstream.ErrPermanent` for non-retryable (403 PAT scope, 404 repo).

### `ReadPRPeople(ctx, pr)`

Return assignees + reviewers as login handles (no `@` prefix; orchestrator adds it when rendering). Empty slices are valid ("no humans assigned"). 404 on the PR means the PR was deleted between FindPR and this call: log + return empty, not an error.

### `FindStickyComment(ctx, pr, commentID)`

Look up a specific comment by ID. Return `(nil, nil)` on 404: the orchestrator's fallback is `ScanForMarker`, not an error.

### `ScanForMarker(ctx, pr, marker)`

List all comments on the PR, return the most recent one whose body contains `marker`. The marker the orchestrator passes is `"<!-- nimblegate-data:"`, the prefix of our hidden HTML data block. This is the recovery primitive when the stored sticky comment ID is lost (state file deleted, fresh gateway box).

Newest-first matters: there may be older nimblegate comments from previous PR loops; the operator-relevant one is the latest.

### `CreateComment(ctx, pr, body)`

POST a new comment with the given markdown body. Return the new comment with its upstream-native ID populated so the orchestrator can persist it for next time.

### `UpdateComment(ctx, c, body)`

PATCH the body of an existing comment. 404 means the comment was deleted between FindSticky and Update: return as transient so the orchestrator retries (and on retry, ScanForMarker may find a different recoverable comment).

## Authentication

Adapters use the **existing per-repo `credential` file** at `<policy-root>/<repo>/credential` (mode 0600). The orchestrator passes the credential string to the adapter constructor; the adapter sends it in whatever auth header the host expects:

- Gitea: `Authorization: token <pat>`
- GitHub: `Authorization: Bearer <pat>`
- GitLab: `PRIVATE-TOKEN: <pat>` (or `Authorization: Bearer <pat>`)

Required PAT scope: enough to read PRs + create issue comments. For most hosts: `write:repository` or equivalent. **Never log the credential** (existing relay code already redacts; follow the same pattern via `redactCred` from `internal/gateway/relay.go`).

## URL → adapter mapping

Adapters are registered on the gateway's `*upstream.Registry` (built via
`upstream.NewRegistry()`) at wiring time: `Register`/`RegisterHost`/
`RegisterOverride` are **methods on the registry instance**, not package-level
`init()` functions. Give each adapter a `New<Host>Adapter(baseURL, token)`
constructor (like `NewGiteaAdapter` / `NewGitHubAdapter`), then map its hosts:

```go
reg.Register("gitlab", NewGitLabAdapter(baseURL, token))
reg.RegisterHost("gitlab.com", "gitlab")
reg.RegisterHost("salsa.debian.org", "gitlab")                      // other public GitLab hosts
reg.RegisterOverride("https://git.internal.example.com/", "gitlab") // pin a self-hosted instance by URL prefix
```

`reg.LookupByURL(upstreamURL)` resolves the adapter at push/notification time:
URL-prefix overrides first, then the host map (`internal/gateway/upstream/registry.go:47`).

There is **no per-repo adapter pin in config**. The per-repo key is `upstream-url`
(`internal/gateway/policy.go:30`), and the adapter "kind" is derived from that URL
by `LookupByURL` (`UpstreamKind` is left empty in config and filled in at wiring time):

```toml
upstream-url = "https://git.internal.example.com/team/repo"
```

A self-hosted host that isn't in the host map is handled with `RegisterOverride`
(above), not a config key.

## Error classification

The orchestrator decides retry vs deadletter based on the error type:

| Wrap with | Meaning | Common cases |
|---|---|---|
| `upstream.ErrTransient` | Retry with backoff | Network failure, 5xx, 429 rate-limit, 404 sticky-deleted-mid-flight |
| `upstream.ErrPermanent` | Deadletter immediately | 403 PAT lacks scope, 401 PAT revoked, 422 malformed request, unknown host |

Example:

```go
func (a *GitLabAdapter) FindPRForRef(ctx context.Context, repo, ref string) (*PullRequest, error) {
    resp, err := a.client.Do(req)
    if err != nil {
        return nil, fmt.Errorf("%w: %v", upstream.ErrTransient, err)
    }
    defer resp.Body.Close()
    if resp.StatusCode == 200 {
        // parse + return
    }
    if resp.StatusCode >= 500 || resp.StatusCode == 429 {
        return nil, fmt.Errorf("%w: HTTP %d", upstream.ErrTransient, resp.StatusCode)
    }
    return nil, fmt.Errorf("%w: HTTP %d", upstream.ErrPermanent, resp.StatusCode)
}
```

## Adapter invariants

1. **Methods never panic.** All failure modes returned as errors.
2. **Adapters are stateless.** No internal caches, no connection pools (Go's `http.Client` handles that). Safe to call from concurrent daemon polls.
3. **Adapters don't render markdown.** Comment body comes from the orchestrator (which uses `internal/gateway/notification/render`). Adapter is pure I/O: receives `body string`, returns success/error.
4. **Adapters don't log credentials.** Use `redactCred` (or local equivalent) when building error strings.

## Testing

Each adapter has its own `<host>_test.go` covering host-specific behaviors (auth header format, pagination, rate-limit handling). All adapters also pass the **shared scenario suite** in `scenario_test.go`: table-driven tests that exercise the interface methods through identical fixtures:

```go
// scenario_test.go
func runAdapterSuite(t *testing.T, name string, factory func(baseURL, token string) Upstream) {
    t.Run(name+"/FindPR_NoOpenPRReturnsNil", func(t *testing.T) { ... })
    t.Run(name+"/CreateComment_BodyIncluded", func(t *testing.T) { ... })
    // ... 20+ scenarios
}

func TestGitLab_RunSharedSuite(t *testing.T) {
    runAdapterSuite(t, "gitlab", func(url, token string) Upstream {
        return NewGitLabAdapter(url, token)
    })
}
```

If the shared suite passes, the adapter is considered conformant. New adapters pass the suite → land cleanly.

## Submitting a new adapter

1. Implement the 7-method interface in `internal/gateway/upstream/<host>.go`.
2. Add `<host>_test.go` with host-specific tests.
3. Register the adapter + its host(s) in `init()`.
4. Run the shared scenario suite: `PATH=$HOME/go/bin:$PATH go test ./internal/gateway/upstream/... -run YourAdapter`.
5. Document any host quirks in this file's "Method semantics" section.
6. Open a PR following the standard nimblegate contribution flow.

External contributors welcome: the adapter layer is intentionally pluggable so new hosts can land without touching the gateway core or other adapters.
