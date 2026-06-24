# Publishing a release to ghcr.io

Step-by-step for cutting a public release. The `.github/workflows/release.yml`
workflow does the build and push; **two things still require manual action**:
making the container package public the first time (it's private by default
on GitHub Container Registry), and verifying anonymous `docker pull` works
before announcing the URL anywhere.

Don't skip the verification step. Until the package visibility is flipped,
the README quickstart (`docker compose up -d` against `ghcr.io/nimblegate/nimblegate:0.1.0`)
returns `manifest unknown` for every reader who isn't signed into a GitHub
account that owns the package.

## Prerequisites (one-time, only on the first release)

- The `nimblegate` GitHub organisation owns the `nimblegate` repo.
- The repo's `.github/workflows/release.yml` declares `permissions: { contents: write, packages: write }`. *(Verify with `grep -A2 permissions .github/workflows/release.yml`, already in tree as of v0.1.0.)*
- A real LICENSE file is in tree at the tag commit. GitHub Container Registry
  reads the licence label from the OCI image and surfaces it on the package
  page; the existing `Dockerfile` sets `org.opencontainers.image.licenses="PolyForm-Noncommercial-1.0.0"`.

## Release flow

### 1. Cut the tag

The tag is what the GitHub Action listens for (`on: { push: { tags: ['v*'] } }`).

```sh
# Make sure the branch you're cutting from is what you want (typically
# feat-public-launch-prep merged to main, or a release branch).
git checkout main && git pull
git tag -a v0.1.0 -m "v0.1.0 - first public release"
git push origin v0.1.0
```

### 2. Watch the Action run

```sh
gh run watch                          # if you have the gh CLI
# or open https://github.com/nimblegate/nimblegate/actions
```

Two jobs run in parallel:

- `binaries`: goreleaser builds platform binaries and uploads them as GitHub release assets.
- `container`: docker buildx builds multi-arch (`linux/amd64,linux/arm64`) and pushes to `ghcr.io/nimblegate/nimblegate:0.1.0`, `:0.1`, and `:latest`.

Expected runtime: ~3-5 minutes total. If either job fails, fix and re-tag
(`git tag -d v0.1.0 && git push origin :v0.1.0 && git tag v0.1.0 && git push origin v0.1.0`).

### 3. Flip the package to public: **this is the load-bearing step**

By default, ghcr.io packages are **private**, even if the repo that pushed
them is public. Until you flip this, `docker pull ghcr.io/nimblegate/nimblegate:0.1.0`
returns `manifest unknown` for any unauthenticated client (i.e. every reader
of the README).

1. Open `https://github.com/orgs/nimblegate/packages`.
2. Click the `nimblegate` package.
3. Click **Package settings** (right sidebar).
4. Scroll to **Danger Zone** → **Change visibility** → **Public**.
5. Confirm by typing the package name.
6. *(Optional, recommended)*: on the same Package settings page, **Connect repository** → select `nimblegate/nimblegate`. This makes the package appear in the repo's sidebar and inherit access rules from the repo for future releases.

This is one-time per package. Subsequent version pushes against the same
package stay public.

### 4. Verify anonymous pull works

From a machine that's **not signed in to GitHub** (or just log out / use a
fresh docker daemon):

```sh
docker logout ghcr.io                # in case credentials are cached
docker pull ghcr.io/nimblegate/nimblegate:0.1.0
docker inspect --format '{{.Config.Labels}}' ghcr.io/nimblegate/nimblegate:0.1.0
```

Expected: pull succeeds for both `linux/amd64` and `linux/arm64`, labels show
`org.opencontainers.image.title=nimblegate`, `org.opencontainers.image.source=https://github.com/nimblegate/nimblegate`.

If pull returns `denied` or `manifest unknown`, the package is still private.
Re-do step 3.

### 5. Smoke the compose quickstart

The README claim is that a stranger can run `docker compose up -d` and have a
working gateway. Verify against the just-pushed image:

```sh
mkdir /tmp/nimblegate-release-smoke && cd /tmp/nimblegate-release-smoke
curl -O https://raw.githubusercontent.com/nimblegate/nimblegate/main/compose.yaml
docker compose up -d
sleep 5
docker compose logs nimblegate | grep nbg-setup    # setup token printed
docker exec nimblegate nbg-status                  # both services up
curl -fsS -o /dev/null -w "%{http_code}\n" http://127.0.0.1:7900/login
# Expected: 200
docker compose down --volumes                      # cleanup
```

### 6. Update the release notes + announce

GitHub creates a draft release when the tag is pushed. Edit it:

- Paste the CHANGELOG.md `[0.1.0]` section as the body.
- Add a "Container image" line: `ghcr.io/nimblegate/nimblegate:0.1.0` (also `:0.1`, `:latest`).
- Publish.

Then announce per the launch playbook (`docs/superpowers/plans/2026-06-01-launch-playbook.md`).

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| `manifest unknown` on anonymous pull | Package is still private | Step 3 above: flip visibility to Public |
| `unauthorized: authentication required` | Action doesn't have `packages: write` permission | `.github/workflows/release.yml` → `permissions: { packages: write }` |
| Pull works for `:0.1.0` but not `:latest` | The container job didn't write the moving tags; check `docker/metadata-action` config | Re-tag or push a `:latest` manually with `docker pull ... && docker tag ... && docker push ...` |
| Anonymous pull works for amd64 only | Multi-arch buildx didn't complete; the arm64 layer is missing | Check the container job logs; re-tag if `setup-qemu-action` step was skipped |
| Package visibility flips back to private after every release | Repo-package linking missing | Step 3.6: Connect repository under Package settings |

## Subsequent releases (v0.2.0+)

Once v0.1.0 is published and the package is public, future releases are
simpler:

```sh
git tag -a v0.2.0 -m "v0.2.0 - <one-line summary>"
git push origin v0.2.0
# Action runs, image is pushed, visibility is already Public (sticky).
# Verify anonymous pull + smoke compose, edit + publish the draft release.
```

The visibility flip is only required for the first release of each package.
