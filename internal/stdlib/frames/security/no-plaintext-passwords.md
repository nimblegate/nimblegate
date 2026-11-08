---
name: no-plaintext-passwords
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
  negatives: 3/3
  last-run: 2026-11-08T09:38:06Z
---

# No plaintext passwords

Detect plain passwords committed to the repository, in the shapes where the
surrounding text proves the value is a password. Passwords have no prefix
for `security/no-hardcoded-credentials` to match, so they need context: a
URL that carries one, a command that is handed one, or a credential file
whose format names the field. Once pushed, the password must be treated as
leaked and changed.

A common source is an agent's own settings: when you approve a command in
Claude Code with "Yes, and don't ask again", the exact command is saved to
`.claude/settings.local.json`, including any password typed into it.

## What's detected

| Shape | Example (value shown as `<pw>`) |
|---|---|
| Password in a URL | `postgres://app:<pw>@db.internal/app`, `https://deploy:<pw>@git.corp.io/x.git` |
| MySQL / MariaDB client | `mysql -u root -p<pw>`, `mysqldump --password=<pw>` |
| sshpass | `sshpass -p <pw> ssh root@host` |
| curl basic auth | `curl -u admin:<pw> https://...` |
| Password environment variables | `PGPASSWORD=<pw>`, `MYSQL_PWD=<pw>`, `SSHPASS=<pw>`, `REDISCLI_AUTH=<pw>` |
| docker login | `docker login -u bot -p <pw> registry` |
| redis-cli | `redis-cli -a <pw>` |
| `.pgpass` | `host:port:db:user:<pw>` |
| `.netrc` / `_netrc` | `machine api login bot password <pw>` |
| `.npmrc` | `_authToken=<pw>`, `_auth=`, `_password=` |
| `.pypirc` | `password = <pw>` |
| `.my.cnf`, `my.cnf`, `my.ini` | `password = <pw>` |
| Docker `config.json` (in `.docker/`) | `"auth": "<pw>"` |
| `.vault-token` | any token in the file |
| Rails `config/master.key` | the 32-character key |

The command shapes are found in any text file: scripts, CI config, docs,
and agents' approved-command lists.

## Never fires

- Environment and template references: `${DB_PASSWORD}`, `$MYSQL_PASSWORD`,
  `%s`, `{{ .Pass }}`, `<password>`.
- Placeholder words and marked fakes: `password`, `changeme`, `secret`,
  values containing `fake`, `dummy`, `example`, `placeholder`, `redacted`.
- Dev defaults: a URL whose host is `localhost`, `127.*`, `*.local`,
  `*.test`, `example.com` and similar, and `user:user` pairs such as
  `postgres:postgres`.
- `mysql -p` with no value (it prompts), `sshpass -e`, `docker login
  --password-stdin`.
- Docs that only name a flag, such as `sshpass -p` or `mysql -p` followed by
  a comma or full stop, in markdown or generated HTML (`&lt;pw&gt;` counts as
  the placeholder `<pw>`). Values that are only markup or punctuation, or
  shorter than three characters, are ignored; a real password inside a code
  span still fires.
- Third-party code: `vendor/`, `third_party/`, `.venv/`, `venv/`,
  `site-packages/`, `bower_components/` and `.svelte-kit/`, whose docs and tests
  carry example URLs.
- Documentation stand-ins: `UPPER_SNAKE` names (`GITEA_TOKEN`) and the
  RFC 5737 example IP ranges (`192.0.2.*`, `198.51.100.*`, `203.0.113.*`).
- URL and command shapes inside test files and `testdata/` or `fixtures/`
  directories, where tests build credential strings on purpose. Credential
  files (`.pgpass`, `.npmrc`, ...) still fire there.

## Severity

`BLOCK`. Each shape names the value as a password, so a match is a leak,
not a guess. Guesses belong to `security/possible-secret`, which only warns.

## Detection scope

- Triggers: `pre-commit`, `cli`.
- Applies to every scanned path. Binary files (NUL byte in content) and
  files over 1 MiB are skipped; noise dirs (`node_modules/`, `dist/`) are
  excluded uniformly.

## Failure message

The reason names the file, line and kind of finding - values are **never
echoed**; the audit log must not re-leak them.

```
✗ security/no-plaintext-passwords (security)
   plaintext password committed (values redacted):
   - .claude/settings.local.json:4 - mysql password flag
   fix: remove the password and ROTATE it (assume leaked); pass it through
        an environment variable or secret manager instead
```

## Override

Per-line, on the line above a known-fake fixture:
```
# appframes:disable-next-line security/no-plaintext-passwords
```

Per-file:
```
# appframes:disable security/no-plaintext-passwords
```

## What's NOT detected

- **Passwords with no telling context** - `connect("db", "<pw>")`. Nothing
  marks the string as a password; `security/possible-secret` warns about
  credential-named fields and random-looking strings near secret words.
- **Prefixed API keys and tokens** (`ghp_`, `sk-ant-`, `AKIA`) -
  `security/no-hardcoded-credentials`.
- **Whole secret files** - `.env` is `security/no-env-file-in-repo`; private
  keys are `security/no-private-keys-in-repo`.
