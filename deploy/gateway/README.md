# nimblegate gateway - server-side deploy artifacts

This directory holds artifacts for running the gateway on a **separate host**
the dev machine cannot reach. The container image is built from the
**single `Dockerfile` at the repo root** - there is no separate
production-only image. Production and evaluation run the same artifact;
operators differ in how they place it on the network.

What's here:

- `docker-compose.yml` - runs the root `Dockerfile` with the three persistent
  volumes (`repos`, `cfg`, `ssh`) + the asymmetric port binding (dashboard on
  loopback, sshd on 2222 all-interfaces).
- `nimblegate-dashboard.service` - systemd unit for **bare-metal** deploys
  (no Docker). Pairs with the install-on-Debian guide in
  the [server guide](https://nimblegate.com/docs/server).

The full server guide - deploy, update, operate, and the security model - is at
[nimblegate.com/docs/server](https://nimblegate.com/docs/server). Read that first.

Quick start:

```sh
# from the repo root:
docker compose -f deploy/gateway/docker-compose.yml up -d --build
docker logs nimblegate | grep nbg-setup    # read the first-run setup token
```

For HTTPS-on-the-dashboard with auto-TLS + basic-auth in front,
see [`examples/with-caddy/`](../../examples/with-caddy/).
