# Scan-ignore: telling nimblegate "don't open this"

By default, nimblegate's file-scanning frames (private-key detection, credential scan, link checker, etc.) walk your project and look at every file. Projects that **serve arbitrary user-facing content** (downloads, uploads, generated archives, sample data) need to tell nimblegate "ignore this whole tree" so those files don't trip the gates.

V0.6 ships three composable mechanisms. Use the one that fits your scope.

## Mechanism overview

| Mechanism | Where it lives | Scope | Use when |
|-----------|----------------|-------|----------|
| `[scan] exclude` | `appframes.toml` | Directory NAMES (any depth) | Skip every directory of that name everywhere (e.g. all `node_modules/`) |
| `[scan] exclude-paths` | `appframes.toml` | Specific PATH globs (root-relative) | Skip one tree without losing identically-named dirs elsewhere |
| `.appframes-ignore` | One file per directory | gitignore-style patterns scoped to the marker's dir | Distributed ownership: the directory's owner declares the policy in place |

All three are **pre-scan**: the matcher decides before the file is opened. No audit-log noise, no per-file cost on ignored paths.

## 1. `[scan] exclude`: segment names

The historical mechanism. Matches directory NAMES anywhere in the tree:

```toml
[scan]
exclude = ["node_modules", "dist", "build", "vendor"]
```

Defaults (when the section is omitted): `.git`, `node_modules`, `dist`, `build`, `.appframes`. Setting `exclude` here **replaces** the defaults: list everything you want skipped including the built-ins.

Limitation: it's name-based, so `vendor` matches every directory called "vendor" anywhere in the tree. If you want to ignore `lib/vendor/` but NOT `src/vendor/`, you need a path glob (next).

## 2. `[scan] exclude-paths`: specific paths

Doublestar globs evaluated against paths relative to the project root:

```toml
[scan]
exclude       = ["node_modules", "dist"]
exclude-paths = [
    "public/downloads/**",      # served files; never scan
    "static/uploads/**",        # user-uploaded content
    "examples/**/*.zip",        # bundled examples
]
```

Glob syntax:

| Pattern | Matches |
|---------|---------|
| `**` | Any number of path segments including zero |
| `*` | One path segment (any chars except `/`) |
| `?` | One character (except `/`) |
| Other | Literal |

Examples:

- `public/downloads/**`: skips everything under `public/downloads/`
- `**/*.zip`: skips every `.zip` anywhere
- `src/generated/*.go`: skips top-level `.go` files in `src/generated/`, but not deeper
- `dist/**`: skips everything under `dist/`

`exclude-paths` **composes with** `exclude`: both are checked. A path is skipped if either matches.

## 3. `.appframes-ignore`: distributed marker files

A `.appframes-ignore` file anywhere in your tree contributes gitignore-style patterns scoped to the file's containing directory. Discoverable, local, version-controlled.

```bash
# public/downloads/.appframes-ignore
*
```

```bash
# user-uploads/.appframes-ignore
# We host these as-is; nimblegate should never look inside.
*.pdf
*.zip
sample-data/
```

### Pattern semantics

- Lines starting with `#` are **comments**
- Blank lines are skipped
- Patterns are doublestar globs (same syntax as `exclude-paths`)
- Patterns **without `/`** match recursively under the marker dir (gitignore-style; `*.zip` skips zips at any depth below)
- Patterns **with `/`** are anchored to the marker dir

A marker in `served/` with `big/**` skips `served/big/data.json` but NOT `other/big/data.json`. A marker in `served/` with `*.pdf` skips `served/a.pdf` AND `served/sub/b.pdf` (recursive).

### Nesting

Marker files compose. A pattern in `a/.appframes-ignore` applies to everything under `a/` (subject to its scope rule). A pattern in `a/b/.appframes-ignore` adds more rules just for `a/b/` and below. Both run.

### A marker file inside an excluded segment is ignored

If `node_modules` is in `[scan] exclude`, putting an `.appframes-ignore` inside `node_modules/` does nothing: nimblegate doesn't descend into excluded segments, so the marker is never discovered. Segment excludes are the outer ring; you can't punch a hole back in from inside them.

## Pick one

| If… | Use |
|-----|-----|
| You want to skip every directory of name X anywhere | `[scan] exclude` |
| One specific path, central declaration | `[scan] exclude-paths` |
| Several directories with different owners / per-directory policy | `.appframes-ignore` marker files |

These compose: you can use all three at once. A path is skipped if any one of them says so.

## Lint surfacing

`nimblegate lint` surfaces malformed patterns from both surfaces as non-fatal warnings:

```
⚠️  Scan-ignore warnings (1):
   - /repo/public/.appframes-ignore: invalid pattern "[broken": error parsing regexp...
   (fix or remove the malformed patterns; other patterns still apply)
```

Good patterns continue to apply; only the broken pattern is dropped.

## Sample project layout

```
my-project/
├── appframes.toml
├── public/
│   ├── downloads/                 # excluded via [scan] exclude-paths
│   │   └── installer.zip
│   └── css/                       # scanned normally
│       └── site.css
├── user-uploads/
│   ├── .appframes-ignore          # `*` - everything here is user content
│   ├── photo-1.jpg
│   └── sub/photo-2.jpg
├── src/                           # fully scanned
│   ├── app.js
│   └── downloads/                 # NOT excluded - "downloads" name alone doesn't trigger exclude-paths
│       └── handler.go
└── node_modules/                  # excluded via [scan] exclude (segment)
    └── ...
```

```toml
[scan]
exclude       = ["node_modules", "dist", "build", ".appframes", ".git"]
exclude-paths = ["public/downloads/**"]
```

`src/downloads/handler.go` is scanned (good, it's source code). `public/downloads/installer.zip` and everything under `user-uploads/` is skipped.

## What this does NOT replace

- **Whitelist** (`.appframes/_canonical/whitelist.toml`) is for VETTED exemptions to specific findings. Use it when a check fires on something you've reviewed and decided to allow, e.g. a test fixture that intentionally contains a fake-looking credential. The whitelist suppresses at the hit level; scan-ignore suppresses at the file-open level.
- **In-source markers** (`# appframes:disable <frame-id>`) are for single-file or single-line opt-outs where the exemption belongs in the code itself.
- **Frame `applies-to.files`** is the frame author's intent ("I only care about these files"). Scan-ignore is the PROJECT'S response ("but skip these served paths").
