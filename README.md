# forgejo-encrypt-mirror

Mirrors Forgejo/Gitea repositories into an age-encrypted git history, optionally pushing the encrypted result to GitHub.

## Running

Copy [configs/config.example.yaml](configs/config.example.yaml) to `configs/config.yaml`, fill in your Forgejo/GitHub/encryption settings, then run:

```
go run ./cmd/mirror
```

Configuration is validated at startup; invalid configuration causes the process to exit immediately with a description of every problem found.

### Logging

Structured logs are written to stderr via `log/slog`. Configure with environment variables:

- `LOG_FORMAT`: `json` (default) or `text`.
- `LOG_LEVEL`: `debug`, `info` (default), `warn`, or `error`.

Security-relevant events (webhook verification, replay detection, status endpoint access, key rotation) are recorded separately by the audit logger (`server.auditLogPath`), independent of `LOG_LEVEL`.

### Operations endpoints

- `GET /healthz` — liveness probe; returns 200 as long as the process is running.
- `GET /readyz` — readiness probe; returns 503 if a dependency (e.g. the `git` binary) isn't available.
- `GET /metrics` — Prometheus metrics (job counts/durations, queue depth, webhook outcomes, encryption/push activity, key rotations).
- `GET /status`, `GET /status/{id}` — job status; protected by `server.statusToken` if set.

### Docker

```
docker build -t forgejo-encrypt-mirror .
docker run -p 8080:8080 -v $(pwd)/configs/config.yaml:/app/configs/config.yaml -v mirror-cache:/app/cache forgejo-encrypt-mirror
```

The image includes a `HEALTHCHECK` against `/healthz` and bundles the `git` CLI, which the mirror shells out to.

### Backup recovery

To decrypt an encrypted mirror back into plaintext files, use `cmd/restore`:

```
go run ./cmd/restore -identity /path/to/identity.txt -encrypted cache/<owner>/<repo>.enc -out ./restored
```

Add `-commit <hash>` to restore the tree as of a specific historical commit in the encrypted repository, instead of its current HEAD.
