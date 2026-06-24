# nimblegate security model

This document describes what nimblegate does and does NOT defend against,
the trust boundary between the binary and project-local files, and the
specific defenses that protect each surface.

## TL;DR: can a malicious frame execute code?

**No.** Frame markdown files are pure metadata + documentation. They cannot
contain executable code. Check logic lives in compiled Go code inside the
nimblegate binary; project frames can only declare *which* check to apply,
not *what* the check does.

The realistic threats are integrity-level, not execution-level:

- A malicious frame can **downgrade severity** of an existing rule
  (BLOCK → WARN/INFO).
- A malicious frame can **fail to apply** a rule (frame metadata + ID
  collision causes the legitimate rule to lose).
- A malicious frame's metadata can **pollute terminal output** with ANSI
  escape sequences if echoed unsanitized.

The defenses below address each.

---

## Trust boundary

Inside the binary:
- **Stdlib frames** (`internal/stdlib/frames/`): shipped with nimblegate,
  embedded via `//go:embed` at compile time. Cannot be replaced at runtime
  without rebuilding from source. Treated as trusted.
- **Go check functions** (`internal/checks/*.go`): the actual logic that
  evaluates rules. Bound to frame IDs in `internal/commands/builtin.go`.
  Compiled into the binary. Treated as trusted.
- **Go std lib** (path/filepath, regexp, os/exec, etc.): standard Go runtime.

Outside the binary (the trust boundary):
- **`appframes.toml`**: project config. Provides frame-enable patterns and
  severity overrides. Read at every invocation.
- **`.appframes/<category>/<name>.md`**: project-local frames. Metadata
  only; no execution path.
- **`.appframes/_canonical/*.toml`**: project canonical tables. Read by
  check functions for project-specific data (e.g. allowed branch names,
  expected website IDs).
- **`.git/hooks/pre-commit`**: written by `nimblegate init`, but the user
  can replace it (and the next `init` won't overwrite it).
- **User's shell environment**: nimblegate does NOT read or trust env vars
  for anything security-critical.

The boundary: anything outside the binary should be treated as untrusted
input. The binary should not crash or escalate privilege based on its
contents.

---

## Frame file: what it can contain

A frame's frontmatter has exactly these fields (full schema:
`docs/schemas/frame-frontmatter.schema.json`):

| Field | Type | Effect on runtime |
|---|---|---|
| `name` | string (kebab-case) | Identity, displayed in output |
| `category` | enum (7 values) | Display ordering, identity |
| `severity` | enum (BLOCK / WARN / INFO) | Outcome treatment |
| `triggers` | list of enum (cli / pre-commit / git-wrap / watcher / server) | When the frame is fired |
| `applies-to.files` | list of glob strings | **Documented only: read by Go check function, not the engine** |
| `applies-to.commands` | list of strings | **Documented only: same as above** |
| `canonical-refs` | list of TOML filenames | **Documented only: same as above** |

The frame's markdown body (everything after the closing `---`) is **never
parsed or rendered**. It exists for human readers (`cat`, IDE preview).
The body is not echoed by `lint` or `check`; it cannot inject anything.

## Invariant: frame body never reaches the terminal

A frame's markdown body and certain frontmatter fields (`applies-to.files`,
`applies-to.commands`, `canonical-refs`) are populated by the parser but
**never echoed by any V0 command**. A malicious frame body containing:

- phishing links (`[click](https://attacker.example.com)`)
- raw ANSI escape sequences (`\x1b[2J\x1b[H`)
- HTML / `<script>` tags
- `javascript:` or `data:` URI links
- clickable terminal hyperlinks (OSC 8 sequences)

…has no path to your terminal in V0. The invariant is enforced by
`internal/commands/body_invariant_test.go`, which embeds a sentinel
string in a project frame body and asserts every read-only command
(`check`, `lint`, `status`, `shell print`) produces output that does
not contain the sentinel.

If a future command starts displaying frame bodies (e.g. a hypothetical
`nimblegate explain <id>`, a web UI editor, or an LSP hover provider),
the implementer MUST:

1. Sanitize the body with `frames.SanitizeForOutput` before any
   terminal echo (strips ANSI, control bytes).
2. Refuse to auto-fetch or auto-resolve URLs found in the body.
3. If rendering as HTML, escape `<`/`>` and reject `javascript:`,
   `data:`, and `vbscript:` schemes from `<a href>` attributes.
4. Emit clickable terminal hyperlinks (OSC 8) only if the user opted
   in; never auto-link arbitrary URLs found in frame metadata.

The regression test will fail when that command leaks the sentinel;
treat the failure as the prompt to add the four protections above.

## Frame file: what it CANNOT do

- Execute shell commands.
- Run Go code.
- Read arbitrary files (even via canonical-refs, those are filenames
  joined to a hardcoded base path; traversal blocked by `filepath.Join`).
- Network access.
- Persistent state (frames are stateless; the only writable file is the
  audit log, written by the engine, not by frames).
- Affect frames in *other* projects: paths are computed from the project
  root discovered via the `appframes.toml` walk-up.

---

## Realistic attack scenarios

### Scenario 1: Severity downgrade (highest impact)

User copies `frames/awesome-extra-checks.md` from an internet source. The
frame declares:

```yaml
name: no-innerHTML-user-input
category: security
severity: INFO  # <-- attacker swapped from BLOCK
triggers: [cli, pre-commit]
```

Since this frame has the same ID as the stdlib `security/no-innerHTML-user-input`,
the project frame replaces the stdlib one (intentional design: projects can
tune built-in rules). The result: real innerHTML violations now report as
INFO and don't block commits.

**Defense:** `nimblegate lint` now flags severity downgrades when a project
frame overrides a stdlib frame. The user sees the downgrade explicitly.

### Scenario 2: Identity collision sabotage

Attacker contributes a PR that adds `.appframes/security/no-innerHTML-user-input.md`
with valid frontmatter but no canonical-refs and a check ID that the binary
doesn't have a Go function for. With the stdlib version replaced, real XSS
detection is silently disabled.

**Defense:** the `runOne` runtime catches "no check function bound" and emits
an `ERROR` outcome: the gate fails loudly rather than silently passing.

### Scenario 3: Terminal injection via metadata

Attacker creates a frame with:

```yaml
name: "evil\x1b[2J\x1b[H"
```

When the loader or lint command echoes the name in an error message,
the terminal interprets the escapes and clears the screen / hides text.

**Defense:** field values are sanitized (printable ASCII + UTF-8 only;
control bytes replaced with `\xNN`) when echoed in error output.

### Scenario 4: Symlink escape

Attacker drops `.appframes/security/x.md` as a symlink to `/etc/passwd`.
The loader opens it, fails to parse (no opening fence), and reports an
error. No data is exfiltrated (the parser's error message doesn't echo
the file's contents).

**Defense:** the loader explicitly skips symlinks (`lstat` check before
opening) and reports them as load warnings. Users see exactly which files
they have that aren't real frames.

### Scenario 5: Resource exhaustion

Huge frontmatter (multi-MB), 100k frame files, deeply nested YAML, all
covered by `internal/frames/edge_test.go` and `internal/frames/loader_edge_test.go`.
The parser uses bounded buffers; the walker terminates on symlink cycles
(filepath.WalkDir uses lstat, doesn't follow directory symlinks).

**Defense:** existing stress tests + bounded buffers in the YAML scanner.

---

## Defenses summary

| Defense | Location | Test |
|---|---|---|
| Frames cannot execute code | Architectural | N/A, by construction |
| Stdlib frames cannot be replaced at runtime | `internal/stdlib/embed.go` (`//go:embed`) | `internal/stdlib/loader_test.go` |
| Project frame ID collisions reported | `internal/frames/loader.go` | `internal/frames/dedupe_test.go` |
| Unknown frame IDs (no bound Go check) → `ERROR` outcome | `internal/engine/runner.go` | `internal/engine/runner_test.go` |
| YAML parser bounded buffer | `internal/frames/parser.go` | `internal/frames/edge_test.go` |
| Symlink frames skipped + reported | `internal/frames/loader.go` | `internal/frames/loader_edge_test.go` |
| Severity downgrades flagged | `internal/commands/lint.go` | `internal/commands/lint_test.go` |
| Metadata sanitized in error output | `internal/frames/sanitize.go` | `internal/frames/sanitize_test.go` |
| Path traversal blocked | `filepath.Join` + hardcoded canonical names | `internal/checks/folderbranchlock_test.go` |
| Audit log atomic appends | `internal/engine/audit.go` (`O_APPEND`) | `internal/engine/stress_test.go` |

---

## Out of scope (V0/V0.5)

These threats are not addressed by V0; users running with elevated privilege
or in shared-tenancy contexts should think carefully:

- **Compromise of the nimblegate binary itself**: if `nimblegate` on disk is
  replaced with a malicious binary, all bets are off. Verify checksums when
  installing.
- **Malicious `appframes.toml`**: the file is trusted; a hostile commit
  that disables enforcement is a code-review problem, not a runtime
  defense. Mitigation: code review + branch protection.
- **Malicious canonical tables**: read by check functions; their content
  is treated as data, not code. But a hostile `.appframes/_canonical/folder-branch-map.toml`
  could trick `folder-branch-lock` into allowing commits to the wrong
  branch by remapping folders. Same mitigation: code review.
- **Side-channel through audit log**: a hostile frame could (in theory)
  encode data into its check output that ends up in audit.log. The audit
  log is local; users own it. Not exploitable beyond what already exists.
- **Supply chain on Go dependencies**: `gopkg.in/yaml.v3`, `github.com/BurntSushi/toml`,
  `github.com/fsnotify/fsnotify` (post-V0). Pinned in go.sum. Verify with
  `go mod verify` before building releases.

## Threat-model assumptions

For the above defenses to hold, these assumptions must remain true:

1. The binary is built from the nimblegate source you trust.
2. The user's shell, terminal, and OS are not compromised.
3. The user's filesystem permissions on `.appframes/` reflect intent.
   A world-writable `.appframes/` is its own problem.
4. The user reviews `nimblegate lint` output before pushing changes that
   alter project frames.
