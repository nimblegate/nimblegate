---
name: no-private-keys-in-repo
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
  last-run: 2026-05-20T14:37:33Z
---

# No private keys in repo

Detect private cryptographic keys committed to the repository. Two
detection paths run in parallel:

1. **Content scan** - match PEM-armored block headers anywhere in any
   text file. These headers are standardized across OpenSSL, OpenSSH,
   and GnuPG and have effectively zero false-positive rate.

2. **Filename scan** - flag file paths that conventionally hold
   private keys (`id_rsa`, `*.pem`, `*.key`, …) so binary key formats
   without a PEM header still get caught.

Once a private key is pushed to a remote, regeneration is the only
remedy. Pre-commit is the cheapest place to stop the leak.

## BLOCK - private keys, rotate immediately

| Detection | Source | Examples |
|---|---|---|
| `-----BEGIN RSA PRIVATE KEY-----` | content | PKCS#1 RSA |
| `-----BEGIN PRIVATE KEY-----` | content | PKCS#8 unencrypted |
| `-----BEGIN ENCRYPTED PRIVATE KEY-----` | content | PKCS#8 encrypted |
| `-----BEGIN OPENSSH PRIVATE KEY-----` | content | OpenSSH format |
| `-----BEGIN DSA PRIVATE KEY-----` | content | DSA |
| `-----BEGIN EC PRIVATE KEY-----` | content | elliptic curve |
| `-----BEGIN PGP PRIVATE KEY BLOCK-----` | content | GnuPG private key |
| SSH private key filename | filename | `id_rsa`, `id_dsa`, `id_ed25519`, `id_ecdsa` (without `.pub` suffix) |
| Key/keystore extension | filename | `*.pem`, `*.key`, `*.p12`, `*.pfx`, `*.jks` |

The filename `.pub` suffix is treated as a strong signal that the
file is a PUBLIC key, not a private one (`id_rsa.pub` → PASS).

## INFO - public certificates, catalogued

| Detection | Source | Why INFO |
|---|---|---|
| `-----BEGIN CERTIFICATE-----` | content | X.509 certs are public by design; sometimes legitimately committed (CA chains, public server certs in test fixtures). Inventory without blocking. |
| `.crt`, `.cer` extension | filename | Same rationale. |

If a commit contains both BLOCK and INFO detections, BLOCK wins; the
reason lists both.

## Failure message

```
❌ security/no-private-keys-in-repo (security)
   private keys detected (content redacted):
   - keys/server.pem:1 - PEM private key header
   - id_rsa:0 - SSH private key filename
   fix: remove the key, REGENERATE IT (assume compromised), store
        via secret manager / KMS, and add ONLY the corresponding
        `.pub` file if a public counterpart is needed in the repo.
```

INFO output ('certificates catalogued') uses non-rotation language.

## Override

Per-file (suppresses every detection in the file):
```
# appframes:disable security/no-private-keys-in-repo
```

Per-line (suppresses the line that follows):
```
# appframes:disable-next-line security/no-private-keys-in-repo
-----BEGIN RSA PRIVATE KEY-----
```

Use per-line ONLY for genuine test fixtures (e.g. a unit test that
needs a known-bad key to exercise parsing). For systematic
suppression (e.g. a `fixtures/` directory of test keys), use a
per-file disable OR add the directory to `[scan].exclude` in
`appframes.toml`.

The filename-pattern detection is also suppressed by either marker
when present in the file's content - but a zero-byte `id_rsa` can't
hold a comment, so it'll fire regardless. That's intentional: a
literal `id_rsa` file is unambiguously suspicious.

## What's NOT detected

Documented so a future "why didn't it catch X?" has an answer:

- **Self-generated random secrets** that aren't PEM-armored and don't
  use a key-suggesting filename. Use the `security/no-hardcoded-credentials`
  frame for prefix-known tokens; entropy-based detection of arbitrary
  random strings is V0.6+ territory (false-positive prone on UUIDs /
  hashes / minified output).
- **DER-encoded keys** without a recognized extension (e.g. a key
  saved as `keyfile.bin`). DER is binary; we don't currently parse
  ASN.1.
- **JWK (JSON Web Key) format private keys** - JSON wrapping with
  `"d"` field. Easy to add in a future revision once we see one in
  the wild.
- **Public keys** (`-----BEGIN PUBLIC KEY-----`, `*.pub`) - public
  by definition, no need to surface.
