# nimblegate behind Caddy (HTTPS + auto-TLS)

Opt-in recipe for running nimblegate's dashboard behind Caddy with a
real HTTPS cert. The core nimblegate image does NOT bundle Caddy - most
operators already have a reverse proxy. This is here if you don't.

## What this gives you

- Public HTTPS on `:443` with Let's Encrypt auto-TLS
- ACME http-01 challenge handled on `:80`
- Dashboard port `7900` removed from the host - only Caddy on the docker
  network can reach it
- nimblegate's own setup-token + login still active (defence-in-depth)
- sshd still exposed on host port `2222` for git push (unchanged)

## How to use

1. Point your DNS A/AAAA record at the host before bringing it up
   (Caddy needs to complete the ACME http-01 challenge on `:80`).
2. Edit `Caddyfile` - replace `nimblegate.example.com` with your real hostname.
3. From the repo root:
   ```sh
   docker compose -f examples/with-caddy/docker-compose.yml up -d --build
   ```
4. Watch Caddy fetch the cert on first start:
   ```sh
   docker logs -f nimblegate-caddy
   ```
5. Visit `https://your-host.example.com/`. nimblegate's `[nbg-setup]` token
   line is in the OTHER container's logs:
   ```sh
   docker logs nimblegate | grep nbg-setup
   ```

## Optional: basic-auth at the Caddy layer too

Uncomment the `basicauth` block in `Caddyfile` and generate a password hash:

```sh
docker run --rm caddy:2-alpine caddy hash-password
```

That gets you two login prompts (one Caddy, one nimblegate). Most operators
don't need both - nimblegate's own auth is the load-bearing one. Use the
proxy basic-auth only if you want a second factor.

## Why this is NOT in the core image

- Bundling Caddy would conflict with operators who already run a reverse
  proxy (nginx, Traefik, Caddy elsewhere, AWS ALB, Cloudflare).
- Caddy doubles the image size and adds an entire process tree we don't
  need for the "evaluate it on localhost" path that the README quickstart
  documents.
- Operators who DO want HTTPS in front have many options; we pick one (Caddy
  because of auto-TLS) and ship it as an opt-in example rather than as
  policy.
