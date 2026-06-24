# deploy/container/

Build assets for the v0.1.0 combined nimblegate container - sshd (git push
endpoint) and the dashboard (HTTP control plane) in a single image, supervised
by s6-overlay v3.

The Dockerfile lives at the repo root (`/Dockerfile`). This directory holds
everything the Dockerfile `COPY`s into the image.

## Layout

```
deploy/container/
└── s6-rc.d/                          → /etc/s6-overlay/s6-rc.d/  inside image
    ├── user/contents.d/              services enabled in the default bundle
    │   ├── init-host-keys
    │   ├── sshd
    │   └── dashboard
    ├── init-host-keys/               oneshot: generate ssh host keys + sshd_config
    │   ├── type                      "oneshot"
    │   └── up                        script (runs once at container start)
    ├── sshd/                         longrun: openssh-server, auto-restart
    │   ├── type                      "longrun"
    │   ├── run                       exec sshd -D -e
    │   └── dependencies.d/init-host-keys   sshd waits for host keys
    └── dashboard/                    longrun: nimblegate gateway dashboard, auto-restart
        ├── type                      "longrun"
        ├── run                       exec s6-setuidgid git nimblegate …
        └── dependencies.d/init-host-keys   dashboard waits for init (chown of /srv/gateway/cfg)
```

## Service dependencies

```
init-host-keys ──┬──→ sshd
                 └──→ dashboard
```

`init-host-keys` runs once and exits; `sshd` and `dashboard` start in parallel
after it completes. They have no dependency on each other - if one crashes,
s6 restarts only that one. The other keeps running.

## Volumes (persistent across container recreation)

- `/srv/gateway/repos`  - bare git repos (the user's actual code)
- `/srv/gateway/cfg`    - per-repo policy + upstream credential + audit log
- `/srv/gateway/ssh`    - sshd host keys + the authorized_keys file (TOFU)

Bind-mount these on the host (not just docker named volumes) if you want
direct file access; either works.

## Adding a service later

1. Create `deploy/container/s6-rc.d/<service>/` with `type` and `run` (or `up`).
2. Add empty marker file `deploy/container/s6-rc.d/user/contents.d/<service>`.
3. Declare dependencies via `dependencies.d/<other-service>` (empty file).
4. Rebuild image.

No Dockerfile change required for new services (the whole tree is `COPY`d).
