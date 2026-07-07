---
name: no-kubeconfig-in-repo
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
  positives: 2/2
  negatives: 2/2
  last-run: 2026-07-07T18:42:23Z
---

# No kubeconfig in repo

Detect Kubernetes client credentials committed to the repository. A
kubeconfig with `client-key-data` is cluster access in one file - the
base64 payload is a private key, and whoever holds it is whoever the
certificate says they are.

## What's detected

| Path | Signal |
|---|---|
| Content | A line containing `client-key-data:` - base64-encoded client private key material; fires in any file, whatever its name |
| Filename | Conventional basenames (`kubeconfig`, `.kubeconfig`, `kubeconfig.yaml`, `kubeconfig.yml`) when the file also has kubeconfig document shape (`clusters:` + `users:`) |

## Severity

`BLOCK`. Client key material has no committed-on-purpose variant; manifests
and Helm charts do not carry `client-key-data` and stay quiet.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to every scanned path. Binary files (NUL byte in content) and
  files over 1 MiB are skipped; noise dirs are excluded uniformly.

## Failure message

Key material is **never echoed**.

```
✗ security/no-kubeconfig-in-repo (security)
   Kubernetes client credentials committed (content redacted):
   - deploy/creds.yaml:11 - kubeconfig client key material (client-key-data)
   fix: remove the kubeconfig from the push and revoke the client
        certificate / rotate the cluster credentials (assume compromised);
        distribute cluster access via your identity provider or short-lived
        tokens
```

## Override

Per-file disable:
```
# appframes:disable security/no-kubeconfig-in-repo
```

Per-line disable (suppresses the line that follows the marker), for
documentation showing the field with a fake payload:
```yaml
# appframes:disable-next-line security/no-kubeconfig-in-repo
client-key-data: RkFLRQ==
```

## What's NOT detected

- **Kubernetes manifests / Helm charts** - `apiVersion` + `kind` alone is
  every manifest; only credential material and kubeconfig-shaped files fire.
- **Bearer tokens in kubeconfigs** (`token:`) - the key overlaps too many
  unrelated YAML fields; token *values* are the value-matching frames' job.
- **Service-account key JSON for cloud providers** - different shape,
  covered by `security/no-hardcoded-credentials` prefixes where applicable.
