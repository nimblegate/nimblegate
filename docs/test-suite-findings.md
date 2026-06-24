# Test suite findings: `test-suite-stress` branch

113 tests across 11 packages, all green with `go test -race -count=1 ./...`.
This document records what the stress + edge tests caught, what was already
correct, and where the surface still has untested edges.

## Test inventory

| Area | File | Purpose |
|---|---|---|
| Engine concurrency | `internal/engine/stress_test.go` | 500-frame parallel runner, mixed panic/slow/fast checks, 50-goroutine audit writer, rapid override bookkeeping |
| Parser edges | `internal/frames/edge_test.go` | empty/huge/CRLF/BOM/unicode/binary/multi-block/tabs/deeply-nested YAML |
| Walker edges | `internal/frames/loader_edge_test.go` | broken symlinks, symlink cycles (cwd-loop), unreadable files, 50-level deep nesting, hidden `.md` files |
| Frame linking | `internal/engine/linking_test.go` | wildcard `category/*` matching, severity overrides, stdlib↔project ID collisions, duplicate project frames |
| Canonical TOML | `internal/canonical/edge_test.go` | malformed TOML, empty file, top-level scalars, non-string values, nested subtables, unicode keys |
| Per-check edges | `internal/checks/edge_test.go` | nested cwd, project-root cwd, empty/whitespace commands, substring matches, huge JS files, binary-as-JS, 1000 changed files, concurrent invocation |
| End-to-end CLI | `internal/commands/integration_test.go` | init twice, check without init, all `--trigger` values, status before any log, watch+check race, shell print, 10-process concurrent check |
| Benchmarks | `internal/{engine,checks,frames}/bench_test.go` | Run with 100/500 frames, audit Write seq + parallel, parser, 1k-file scans |

## Things confirmed correct under stress

- **Parallel runner is goroutine-safe**: 500 frames including panickers run cleanly with `-race` and no deadlock; results never lost.
- **Audit log writer is concurrency-safe**: 50 goroutines × 200 writes = 10 000 lines all valid JSON, none torn, none missing.
- **Engine `runOne` panic recovery works**: one frame panic does not affect peers; outcome is `ERROR`, not crash.
- **Filesystem walker terminates on symlink cycles**: `filepath.WalkDir` uses `lstat`, doesn't follow directory symlinks; tested cycle exits in <100ms.
- **Multi-process audit writes don't tear lines**: 10 concurrent `nimblegate check` processes produce 20 valid JSON lines (Linux `O_APPEND` atomicity holds for small writes).

## Behaviours documented, worth deciding on

These tests pass today but the behaviour might surprise users. Decide deliberately whether to keep or change:

### 1. A single malformed frame aborts the whole project-frame load
`internal/frames/loader_edge_test.go::TestLoadFromDir_OneInvalidFrameFailsLoudly`
- One bad `.md` file in `.appframes/` causes `LoadFromDir` to return an error, dropping all other project frames.
- **Pro:** loud failure; bad frame is obvious.
- **Con:** a user dropping a `README.md` into `.appframes/` breaks every other project frame until they remove it.
- **Recommendation:** consider partial-load + warning printout for V0.5 (collect bad frames, return both list + errors).

### 2. Non-frame `.md` files inside `.appframes/` fail the load
`internal/frames/loader_edge_test.go::TestLoadFromDir_NonFrameDotMdFailsParse`
- Same problem as above; the loader treats every `.md` file as a frame.
- **Possible fix:** only consider files that have YAML frontmatter; silently skip pure markdown notes.

### 3. Hidden `.md` files (e.g. `.draft.md`) are loaded
`internal/frames/loader_edge_test.go::TestLoadFromDir_HiddenDotPrefixedFile`
- The walker doesn't filter dotfiles; a `.draft-frame.md` is treated as production.
- **Possible fix:** skip files starting with `.` by convention.

### 4. `--force-with-lease` is blocked the same as `--force`
`internal/checks/edge_test.go::TestNoForcePushMain_ForceWithLeaseToMain`
- `git push --force-with-lease` is the safer variant (atomic compare-and-set against remote).
- Current check treats it identically to `--force` and BLOCKs both.
- **Pro:** safer default.
- **Con:** users who know what they're doing will end up using `--force-yes` more often.
- **Recommendation:** consider WARN (not BLOCK) for `--force-with-lease` to protected branches.

### 5. Branch name SUBSTRING does not trigger the protected-branch block
`internal/checks/edge_test.go::TestNoForcePushMain_BranchNameSubstring`
- A branch called `main-rewrite` is NOT blocked when force-pushed (exact-string compare).
- This is correct, but worth knowing the heuristic is conservative; a malicious branch named `main` somewhere in a multi-arg command will be caught, but `notmain` won't.

### 6. Nested TOML subtables stringify as Go-style `map[...]` literals
`internal/canonical/edge_test.go::TestLoad_NestedSubtableBehavior`
- `[branches.protected]` produces a key `protected` with value `"map[no-force:true]"` when materialised through `fmt.Sprintf("%v", ...)`.
- Frame check functions can't usefully consume this. Document that canonical tables should stay flat: one `[section]` deep.
- **Recommendation:** add validation that rejects nested subtables in canonical tables, or properly expose them as nested maps.

### 7. Empty command strings SKIP rather than ERROR
`internal/checks/edge_test.go::TestNoForcePushMain_EmptyCommandSkips`
- A `CheckContext{Command: ""}` produces SKIP from command-based checks.
- This is reasonable for CLI-trigger invocations where Command is unset; but a programming bug that forgets to set Command would silently disable command-safety checks. Worth a sanity-log at the runner level.

## Possible bugs / weak spots (not failures yet)

- **`FolderBranchLock` reloads the canonical table on every invocation**: bench shows ~20µs per call, mostly disk read. Negligible for one check but adds up if other checks adopt the same pattern. Consider a per-engine canonical-table cache.

- **`NoInnerHTMLUserInput` walks the entire project on CLI trigger when `ChangedFiles` is empty**: ~10ms per 1000 files; fine for V0 but doesn't scale to multi-thousand-file repos. Pre-commit trigger already uses staged files; CLI could use `git diff HEAD` or honor a `--since=HEAD~1` flag.

- **`isFrameEnabled` only supports trailing `/*` wildcards**: no negation (`!convention/*`), no glob (`security/no-*`). Tests confirm current behaviour but the limitation is undocumented for end users.

- **`AddProjectOverride` silently overwrites a previous project frame with the same ID**: `internal/engine/linking_test.go::TestRegistry_DuplicateProjectFrames`. Two project frames at different paths with the same name+category will silently replace each other in registry order. Worth either ERROR or WARN.

- **Parser allocates ~78 KB and 169 allocs per frame**: driven by `yaml.v3` decoder. Only at startup so cheap in absolute terms, but a high-frame project will pay this. Consider lazy parsing (parse frontmatter only when matched).

## Benchmark baselines

Captured on Intel i5-10400T @ 2.00GHz, single-threaded test process, `-race` off:

| Operation | Latency | Notes |
|---|---|---|
| `engine.Run` with 100 frames | 133 µs | parallel goroutines |
| `engine.Run` with 500 frames | 620 µs | scales linearly |
| `engine.Audit.Write` (seq) | 1.8 µs | mutex + RFC3339Nano format |
| `engine.Audit.Write` (parallel) | 2.0 µs | minor contention |
| `frames.Parse` per frame | 58 µs | dominated by `yaml.v3` |
| `NoInnerHTML` scan 1000 files | 9.7 ms | regex per line |
| `CrossBranchID` scan 500 files | 5.3 ms | regex over file bytes |
| `FolderBranchLock` single call | 20 µs | canonical table reload each time |

V0 latency budget: a check across a small repo should complete in single-digit milliseconds. We're well within that for the common case.

## Untested edges (future test work)

- **Project frame whose `triggers:` list contains an unknown trigger**: what does the registry do?
- **Frame with `canonical-refs:` pointing to a missing TOML file**: currently the check function decides; no engine-level validation.
- **Engine init when `audit.log`'s parent directory is unwritable**: surfaces as `OpenAudit` error; user-facing message could be friendlier.
- **`nimblegate check` invoked through a symlink to the binary**: does `os.Args[0]` resolution affect anything?
- **`nimblegate git` invoked outside any git repo**: currently passes through to real git; no extra coverage for that branch.
- **Audit log file growth**: no rotation in V0; multi-day project will accumulate. Worth a stress test with a >100MB log to confirm `status`/`watch` still work.
- **Concurrent `nimblegate shell install`**: should be idempotent; test not yet written.

These can land in V0.5 along with the candidate frames once the test suite finishes settling.
