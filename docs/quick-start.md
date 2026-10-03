# Quick start: your first blocked push in 10 minutes

Run the gateway in Docker, point one repo at it, and watch it stop a leaked
key. Need bare metal, a LAN setup or TLS? The
[full setup guide](getting-started.md) covers every option.

You need: a machine with **Docker** (the gateway), your dev machine with
**git** and an SSH key, and a **personal access token** that can write to your
repo on GitHub, GitLab or Gitea.

## 1. Start the gateway

On the gateway machine:

```bash
curl -O https://raw.githubusercontent.com/nimblegate/nimblegate/main/compose.yaml
docker compose up -d
```

## 2. Claim your admin login

Print the one-time setup token:

```bash
docker logs nimblegate | grep nbg-setup
```

Open **http://localhost:7900/setup**, paste the token and choose a password.
Gateway on another machine? Tunnel to it first, then open the same address:

```bash
ssh -L 7900:127.0.0.1:7900 <user>@<gateway-host>
```

## 3. Add your SSH key

On your dev machine, print your public key:

```bash
cat ~/.ssh/id_ed25519.pub
```

In the dashboard: **Keys -> Add a key**, paste the line, save.
No key yet? Run `ssh-keygen -t ed25519` first.

## 4. Register your repo

In the dashboard: **Repos -> Add a repo**.

- **Name:** what you'll push to, e.g. `my-app`
- **Upstream URL:** your real repo over HTTPS, e.g. `https://github.com/you/my-app.git`
- **Upstream credential:** the access token (GitHub fine-grained: Contents read and write)

Click **Register**. Existing history is mirrored down, and the **core** rule
kit is applied.

## 5. Push through the gateway

On your dev machine, in your clone of the repo:

```bash
git remote set-url origin ssh://git@<gateway-host>:2222/~/my-app.git
git push
```

A clean push is checked and forwarded to your real repo in about a second.
Keep the `~/` in the URL; it's required on the Docker image.

## 6. Watch it block a leaked key

Commit a random fake AWS key and push:

```bash
echo "AWS_KEY=AKIA$(LC_ALL=C tr -dc 'A-Z0-9' < /dev/urandom | head -c 16)" > leak-test.env
git add leak-test.env && git commit -m "test: leak a fake key"
git push
```

The push is rejected, and nothing reaches your real repo:

```
remote:   refs/heads/main: BLOCK [security/no-hardcoded-credentials] credentials detected (raw bytes redacted): leak-test.env:1 - AWS access key
 ! [remote rejected] main -> main (pre-receive hook declined)
```

Undo the test commit with `git reset --hard HEAD~1`. Open **Feed** in the
dashboard to see both pushes and why each was decided.

## Next

- [Run your AI agent through the gateway](quick-agents.md) - nothing to configure in the agent, two things to check.
- [Choose what gets checked](quick-frames.md) - one-click kits on top of core.
- [Full setup guide](getting-started.md) - bare metal, LAN, TLS, other git hosts, and why each step matters.
