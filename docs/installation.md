# Installation

This guide covers every supported way to install and run
`forgejo-encrypt-mirror`. All deployment methods share the same
[configuration](configuration.md); only the packaging differs.

- [Prerequisites](#prerequisites)
- [Docker (recommended)](#docker-recommended)
- [Docker Compose](#docker-compose)
- [From source](#from-source)
- [systemd](#systemd)
- [Kubernetes](#kubernetes)
- [Verifying the install](#verifying-the-install)

## Prerequisites

- **`git` CLI** on `PATH`. The service shells out to git for clone/fetch/push.
  (The Docker image bundles it.)
- An **age keypair**. Generate one with:

  ```sh
  go run filippo.io/age/cmd/age-keygen@latest -o identity.txt
  ```

  The **public key** (`age1…`) is the *recipient* — put it in the config. The
  **private key** in `identity.txt` is the *identity* — keep it offline; it is
  the only key that can decrypt your backups. See
  [security.md](security.md#key-management).
- A **Forgejo/Gitea instance** with permission to add webhooks and an access
  token with read access to the repositories you want to mirror.
- *(Optional, for off-site push)* a **GitHub personal access token** with `repo`
  scope (classic) or `Contents: read/write` + `Administration: read/write`
  (fine-grained) so the service can create the destination repo and push.

To build from source or use `go run`, you also need **Go 1.26.5+** (matching the
`go` directive in [go.mod](../go.mod)).

## Docker (recommended)

Multi-arch images are published to GitHub Container Registry for `linux/amd64`,
`linux/arm64`, and `linux/arm/v7`.

```sh
docker pull ghcr.io/seanh1995/forgejo-encrypt-mirror:latest
```

Prepare a config file (see [configuration.md](configuration.md)) and run:

```sh
docker run -d --name forgejo-encrypt-mirror \
  -p 8080:8080 \
  -v "$(pwd)/configs/config.yaml:/app/configs/config.yaml:ro" \
  -v mirror-cache:/app/cache \
  ghcr.io/seanh1995/forgejo-encrypt-mirror:latest
```

Notes:

- The config is mounted read-only at `/app/configs/config.yaml` (the path the
  binary loads by default).
- `/app/cache` **must** be a persistent volume. It holds the local mirror
  clones, the encrypted history, and the key-rotation state file. Losing it
  forces a full re-mirror on the next run (which is safe, just slower).
- The image runs as the non-root `mirror` user and ships a `HEALTHCHECK`
  against `/healthz`.

Image tags follow the git tags: `vX.Y.Z`, `vX.Y` (moving minor tag), plus
`sha-<commit>`. Pin to an exact `vX.Y.Z` in production.

### Building the image yourself

```sh
docker build -t forgejo-encrypt-mirror --build-arg VERSION=dev .
```

The multi-stage build cross-compiles with the native Go toolchain per target
platform. To build all published platforms locally with buildx:

```sh
docker buildx build \
  --platform linux/amd64,linux/arm64,linux/arm/v7 \
  -t forgejo-encrypt-mirror .
```

## Docker Compose

A ready-to-edit Compose file lives at
[examples/docker-compose.yml](../examples/docker-compose.yml):

```sh
cp configs/config.example.yaml configs/config.yaml
# edit configs/config.yaml
docker compose -f examples/docker-compose.yml up -d
```

## From source

```sh
git clone https://github.com/seanh1995/forgejo-encrypt-mirror.git
cd forgejo-encrypt-mirror
cp configs/config.example.yaml configs/config.yaml
chmod 600 configs/config.yaml
# edit configs/config.yaml
go run ./cmd/mirror
```

To build standalone binaries:

```sh
# Service
go build -ldflags "-s -w -X main.version=v1.0.0" -o mirror ./cmd/mirror
# Restore tool
go build -o restore ./cmd/restore
```

`main.version` is wired through to the `/metrics` `build_info` gauge and the
startup log line, so set it via `-ldflags` for release builds.

The service loads `configs/config.yaml` relative to its **working directory**,
so run it from the repository/deploy root (or place the config accordingly).

## systemd

A unit template is provided at
[examples/systemd/forgejo-encrypt-mirror.service](../examples/systemd/forgejo-encrypt-mirror.service).

```sh
sudo useradd --system --home /opt/forgejo-encrypt-mirror --shell /usr/sbin/nologin mirror
sudo install -d -o mirror -g mirror /opt/forgejo-encrypt-mirror /opt/forgejo-encrypt-mirror/cache
sudo install -o mirror -g mirror -m 0755 mirror /opt/forgejo-encrypt-mirror/mirror
sudo install -o mirror -g mirror -m 0600 configs/config.yaml /opt/forgejo-encrypt-mirror/configs/config.yaml

sudo cp examples/systemd/forgejo-encrypt-mirror.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now forgejo-encrypt-mirror
sudo journalctl -u forgejo-encrypt-mirror -f
```

The unit sets `WorkingDirectory=/opt/forgejo-encrypt-mirror` so the default
`configs/config.yaml` path resolves, and applies several sandboxing directives
(`ProtectSystem`, `NoNewPrivileges`, etc.). Ensure `git` is installed on the
host.

## Kubernetes

A minimal Deployment + Service + Secret example:

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: forgejo-encrypt-mirror-config
stringData:
  config.yaml: |
    server:
      address: ":8080"
      statusToken: "REPLACE_ME"
    forgejo:
      url: "https://forge.example.com"
      token: "REPLACE_ME"
      webhookSecrets: ["REPLACE_ME"]
    github:
      owner: "backup"
      token: "REPLACE_ME"
      autoCreate: true
      private: true
    encryption:
      recipient: "age1REPLACE_ME"
    git:
      cacheDir: "/app/cache"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: forgejo-encrypt-mirror
spec:
  replicas: 1              # run a single replica: see note below
  strategy:
    type: Recreate         # never run two writers against the same cache
  selector:
    matchLabels: { app: forgejo-encrypt-mirror }
  template:
    metadata:
      labels: { app: forgejo-encrypt-mirror }
    spec:
      securityContext:
        runAsNonRoot: true
      containers:
        - name: mirror
          image: ghcr.io/seanh1995/forgejo-encrypt-mirror:v1.0.0
          ports: [{ containerPort: 8080 }]
          livenessProbe:
            httpGet: { path: /healthz, port: 8080 }
          readinessProbe:
            httpGet: { path: /readyz, port: 8080 }
          volumeMounts:
            - { name: config, mountPath: /app/configs, readOnly: true }
            - { name: cache, mountPath: /app/cache }
      volumes:
        - name: config
          secret:
            secretName: forgejo-encrypt-mirror-config
            items: [{ key: config.yaml, path: config.yaml }]
        - name: cache
          persistentVolumeClaim:
            claimName: forgejo-encrypt-mirror-cache
---
apiVersion: v1
kind: Service
metadata:
  name: forgejo-encrypt-mirror
spec:
  selector: { app: forgejo-encrypt-mirror }
  ports: [{ port: 80, targetPort: 8080 }]
```

> **Single writer only.** The service is designed to run as **one instance** per
> cache directory. The local mirror clones and encrypted repositories are not
> safe for concurrent writers. Use `replicas: 1` with `strategy: Recreate`, and
> mount a `ReadWriteOnce` PVC for the cache.

## Verifying the install

```sh
curl -fsS http://localhost:8080/healthz   # -> "healthy"
curl -fsS http://localhost:8080/readyz    # -> "ready" (503 if git is missing)
curl -fsS http://localhost:8080/metrics | grep forgejo_mirror_build_info
```

On startup the service validates the config and logs every problem found before
exiting non-zero, so a clean start means the config is valid. Trigger a test
push in Forgejo and confirm a job appears in the logs and (if configured) an
encrypted repo appears on GitHub.

Next: [configuration.md](configuration.md) · [operations.md](operations.md)
