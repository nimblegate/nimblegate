---
name: no-terraform-state-in-repo
category: security
subcategory: credentials
platform: []
framework: []
severity: BLOCK
severity-source: frame
tier: 1
dedup-key: file:line
triggers:
  - pre-commit
  - cli
applies-to:
  files:
    - "**/*"
pattern: secret-in-source
lifecycle: active
selection-grade: passing
selection-stats:
  positives: 3/3
  negatives: 2/2
  last-run: 2026-07-07T18:42:23Z
---

# No Terraform state in repo

Detect Terraform state committed to the repository. State files hold every
provider credential, connection string, and resource attribute in
**plaintext** - including values marked `sensitive` in configuration.
State belongs in a remote backend with locking, never in git.

## What's detected

Two detection paths, content first:

| Path | Signal |
|---|---|
| Content | The document carries both `"terraform_version"` and `"lineage"` - present in every real state file regardless of Terraform version, so renamed state (`backup/state.json`) still fires |
| Filename | Basename ending `.tfstate` or `.tfstate.backup` (the automatic backup Terraform writes next to the state) |

## Severity

`BLOCK`. Committed state is a credential leak the moment it lands;
`terraform { backend }` configuration files are unaffected.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to every scanned path. Binary files (NUL byte in content) and
  files over 1 MiB are skipped for the content path; noise dirs are
  excluded uniformly.

## Failure message

State content is **never echoed**.

```
✗ security/no-terraform-state-in-repo (security)
   Terraform state committed (content redacted):
   - infra/terraform.tfstate:0 - Terraform state content (terraform_version + lineage)
   fix: remove the state from the push and rotate every credential it
        contains; move state to a remote backend (S3 + locking, Terraform
        Cloud, or your host's equivalent) and add `*.tfstate*` to .gitignore
```

## Override

Per-file disable (for a verified-sanitized fixture):
```
// appframes:disable security/no-terraform-state-in-repo
```

## What's NOT detected

- **Terraform configuration** (`*.tf`, `*.tfvars`) - configuration is meant
  to be committed; secrets inside `.tfvars` are the value-matching frames'
  job.
- **Other IaC state** (Pulumi checkpoints, CDK context) - candidates for a
  follow-up frame once their stable markers are catalogued.
- **State inside archives** - archive contents are not unpacked.
