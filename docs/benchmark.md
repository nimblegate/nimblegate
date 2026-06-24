# Agent Cleanliness Benchmark: scoring harness

> Measures the hidden maintenance tax (recurring, objectively-wrong code
> mistakes) that functional benchmarks miss. Scores already-collected gate
> data; never executes an agent.

---

## What it is

Functional benchmarks (SWE-bench, HumanEval) measure *did the code pass the
tests*. They miss the hidden cost of adoption: code that works but ships small,
objective mistakes (hardcoded secrets, committed private keys, injection sinks,
command-injection) that don't fail a test but cost review time, rework, and
incidents. That accumulated drag is what teams actually pay when they adopt an
agent.

The gateway is a uniquely neutral instrument: it applies identical rules to
every agent and records every push deterministically. This harness turns that
recorded data into a comparison matrix (**how cleanly each agent works on
realistic tasks, per stack**) scored purely on rules nobody disputes.

---

## Governing principles

These are non-negotiable. Violating them collapses the comparison's credibility.

1. **Scored frames are the unimpeachable core only.** A frame may score the
   benchmark only if it would fail code review at any competent shop regardless
   of taste: hardcoded credentials, committed private keys, injection/XSS sinks
   with robust detection, command-injection, non-idempotent destructive
   migrations, invisible-Unicode / encoding corruption, HTML validity, and
   **presence** of required structural elements. Everything structural,
   stylistic, or directional is excluded.

2. **Presence/placement, never content quality.** Frontend frames check *is
   the element there and well-formed*, not whether the alt text or keywords
   are good. Quality is judgment; presence is fact.

3. **Three bars for neutrality**, all required: (a) universally-agreed-bad,
   (b) near-zero false positives, (c) stack-agnostic, catches the thing in
   every stack, not just one.

4. **Published, correctable false-positive whitelist.** Known FPs are excluded
   via `(frame, message-substring, reason)` entries, all published and
   justified. Anyone may submit a proven FP for inclusion. The whitelist
   converges in the open.

5. **Per-stack reporting, never cross-stack aggregation.** "5 findings in Go"
   and "5 in JS" are not comparable; no single combined score.

6. **Rates with variance.** Agents are stochastic; metrics are means ± stddev
   over many repetitions, never a single number.

7. **Many simple, tightly-specified tasks** over few complex ones. The cost-sink
   is a rate visible across volume; simple tasks are unambiguous, fast,
   reproducible, and representative of daily work.

8. **Everything open, no advertising.** Tasks, frame set, whitelist, harness
   code, and raw runs are all published. Results are presented as a benchmark,
   not a sales funnel. The scoring tool is disclosed (that is the transparency)
   but never pitched.

---

## Metric definitions

A **run** = one (agent, task, stack, repetition) = one gateway repo with an
ordered push sequence (the fix loop). Metrics are computed after removing
whitelisted findings and keeping only scored frames.

| Metric | Definition |
|---|---|
| **Clean push** | A push with zero scored findings. |
| **Convergence** | 1-based index of the first clean push. Lower is better. Not converged if none. |
| **Cleanliness** | Mean scored findings per push across the run. Lower is better. |
| **Recurrence** | Fraction of distinct scored fingerprints appearing in ≥ 2 pushes (the agent was shown the finding and it came back). Higher = more flailing. |
| **Observed (not scored)** | Counts from non-scored frames, reported descriptively per cell, never folded into the score. |

Across repetitions of the same (agent, task, stack): each metric becomes
mean ± stddev. The matrix cell aggregates an (agent, stack) over its
tasks and reps. Stacks are compared independently; no cross-stack number.

---

## How to run

**Prerequisites:** the repos you want to score must already be registered with
the gateway, the agents must have pushed to them, and the gate must have
recorded the audit logs. This harness is read-only: it scores what is already
there.

```bash
nimblegate gateway benchmark score \
  --config benchmark.toml \
  --policy-root /etc/nimblegate-gateway/repos
```

Flags:

| Flag | Default | Description |
|---|---|---|
| `--config` | `benchmark.toml` | Path to the benchmark config (TOML). |
| `--policy-root` | `/etc/nimblegate-gateway/repos` | Gateway per-repo config root: the directory that holds each repo's `audit.log`. |
| `--json` | off | Emit the matrix as JSON instead of a table. |

The table output groups agents by stack:

```
stack: go
agent        runs  clean/push  converged  conv@  recurrence
claude-code  5     0.12±0.08   80%        2.2±0.8  0.10±0.12
cursor       5     0.31±0.14   60%        3.4±1.2  0.38±0.21
```

The `--json` flag emits the full matrix as structured JSON, including per-frame
breakdowns (`by_frame`) and observed-but-not-scored frame counts (`observed`).

---

## Config format

See `examples/benchmark.toml` for a working example. The three sections:

### `[scored]`

```toml
[scored]
frames = [
  "security/no-hardcoded-credentials",
  "security/no-private-keys-in-repo",
  "commands/curl-pipe-shell",
]
```

Frame IDs must exactly match a real stdlib frame: the CLI validates this at
startup and exits with an error if any ID is unknown. This prevents a typo'd
ID from silently scoring nothing.

> **Use the frame's real ID (its frontmatter `category/name`), not its directory
> name.** The benchmark scores the gateway's audit log, whose findings carry each
> frame's `Frame.ID()` = the `category:` value from the frame's frontmatter plus
> `/name`. A frame's *directory* can differ from its category: the curl frame lives
> at `internal/stdlib/frames/command-safety/curl-pipe-shell.md`, but its ID is
> `commands/curl-pipe-shell` (frontmatter `category: commands`). Likewise the
> `fs-safety/` dir → `filesystem/…` IDs, `convention/` → `web/…` or
> `documentation/…`, `git-safety/` → `git/…`, `network-safety/` → `network/…`.
> Score on the ID form (`commands/curl-pipe-shell`); the directory-name form
> (`command-safety/…`) is **not** a valid ID and will validate-fail or match
> nothing. `security/…` and `encoding/…` IDs match their directory, so they are
> unaffected. Confirm any ID against the frame file's `category:` frontmatter (or
> the registry, see "Reproduce this yourself" below).

### `[[whitelist]]`

```toml
[[whitelist]]
frame   = "security/no-hardcoded-credentials"
contains = "test/fixtures"
reason  = "example tokens in test fixtures, not live credentials"
```

Each entry excludes findings of the named frame whose `message` contains the
`contains` substring (the message carries the file path and identifier). Omit
`contains` to exclude all findings of the frame. The `reason` field is
required: it is the published justification.

### `[[run]]`

```toml
[[run]]
repo  = "bench-claude-blog-go-1"
agent = "claude-code"
task  = "blog-crud"
stack = "go"
rep   = 1
```

One entry per gateway repo. `repo` is the name registered with the gateway
(the subdirectory under `--policy-root`). Each (agent, task, stack, rep) tuple
must map to exactly one repo: duplicates are rejected at load time.

---

## Reproduce this yourself

Everything needed to reproduce the score from scratch is public:

1. **The harness**: `nimblegate gateway benchmark score` (this repo).
2. **The config**: the `benchmark.toml` you ran, which records the exact
   scored frames, the whitelist with reasons, and the run-to-repo mapping.
3. **The raw audit logs**: each repo's `audit.log` under `--policy-root`.
   These are append-only JSONL files written by the gateway; they are the
   only data source the harness reads.
4. **The stdlib frames**: the frame definitions in `internal/stdlib/` that
   determined each gate decision.

Given those four things, any operator can re-run the harness and get the
identical matrix. No external service, no proprietary data, no hidden
post-processing.

To print the exact scored frame IDs the stdlib registry knows:

```bash
nimblegate gateway benchmark score --config /dev/null --policy-root /tmp 2>&1 || true
# or: inspect internal/stdlib/frames/ directly
```

The whitelist is machine-readable in the config: every exclusion has a reason
and is auditable via `git log`.

---

## Worked example (smoke evaluation, 2026-06-13)

A first hands-on run to validate the mechanism end-to-end. It is illustrative,
not a benchmark result: one hand-planted project and two hand-authored audit
logs (deliberately biased) used only to confirm the pipeline runs.

**How it was evaluated:**

1. **A throwaway project** was created with four planted mistakes: a hardcoded
   AWS key and a Stripe key (`app.js`), a `curl | sh` install line
   (`setup.sh`), a committed RSA private key (`deploy_key.pem`), and an XSS
   sink (user input straight into HTML).
2. **The gate's frame engine was run on it** via `nimblegate check` (the same
   engine the gateway runs on a push), with the `core` kit. Result: **4 BLOCK
   findings**: `commands/curl-pipe-shell`, `security/no-hardcoded-credentials`
   ×2 (AWS + Stripe), `security/no-private-keys-in-repo`. The XSS sink was **not**
   flagged, because the `core` kit does not enable an XSS frame, a direct
   illustration of *the frames are the rubric: the gate catches exactly what is
   selected, nothing more.* A web benchmark would enable the web/security-strict
   kit.
3. **The scorer was run** (`nimblegate gateway benchmark score`) over two
   hand-authored audit logs representing two agents (A messy, B cleaner),
   producing the comparison matrix:

   ```
   agent    runs  clean/push  converged  conv@  recurrence
   agent-A  1     1.00        100%       3.0    0.50
   agent-B  1     0.50        100%       2.0    0.00
   ```

   Agent A made more findings per push (1.00 vs 0.50), took longer to reach a
   clean push (3 vs 2), and **repeated a mistake** (the credential fingerprint
   reappeared on its second push → recurrence 0.50, where B never repeated).
   The `--json` output also broke this down `by_frame` (A: 2 credential + 1 key;
   B: 1 credential). This is the cleanliness tax made measurable.

**What was real vs. constructed:** the gate findings in step 2 are real (the
live frame engine on a real file tree). The two audit logs in step 3 were
hand-written to represent gate output (a full agent race through the live
gateway was not run), so the *numbers* are illustrative; only the *mechanism*
(project → objective findings → scored comparison) was being validated. A
genuine benchmark substitutes real tasks and real agents pushing through the
live gate; the scorekeeping demonstrated here is the part already built.
