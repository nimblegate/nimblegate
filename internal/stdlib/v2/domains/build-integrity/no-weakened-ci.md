---
name: no-weakened-ci
category: app-correctness
subcategory: build-integrity
platform: []
framework: []
severity: WARN
tier: 6
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/.github/workflows/*.yml"
    - "**/.github/workflows/*.yaml"
    - "**/.gitlab-ci.yml"
    - "**/.circleci/config.yml"
    - "**/bitbucket-pipelines.yml"
    - "**/azure-pipelines.yml"
    - "**/.woodpecker.yml"
    - "**/Jenkinsfile"
pattern: silent-safety-bypass
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 5/5
  negatives: 3/3
  last-run: 2026-09-27T16:23:22Z
---

# CI must not be told to ignore failures

Flags CI settings that let a failing step or job pass:

- `continue-on-error: true` (GitHub Actions)
- `allow_failure: true` (GitLab CI)
- `|| true` and `|| exit 0` after a command
- `set +e` in a script step

Scanned files: `.github/workflows/*.yml`, `.gitlab-ci.yml`,
`.circleci/config.yml`, `bitbucket-pipelines.yml`, `azure-pipelines.yml`,
`.woodpecker.yml` and `.woodpecker/*.yml`, `.drone.yml`,
`.buildkite/pipeline.yml`, `Jenkinsfile`. Comment lines are ignored.

Why it matters with coding agents: when CI fails, making the step "not fail"
is a one-line change that turns the pipeline green without fixing anything.

WARN only: nothing is blocked. The frame sees the files as pushed, so it
reports every unmarked case present, not only new ones; after a one-time
pass to fix or mark the existing ones, each warning is a new shortcut.

## Fix

Let the step fail and fix the cause. If a step may fail by design (an
optional upload, a best-effort cache step), record why: a whitelist entry with
a reason, or `# appframes:disable-next-line app-correctness/no-weakened-ci` on
the line above.
