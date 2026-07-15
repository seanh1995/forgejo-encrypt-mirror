# Operations

How to run, observe, and troubleshoot `forgejo-encrypt-mirror` in production.

- [Lifecycle](#lifecycle)
- [HTTP endpoints](#http-endpoints)
- [Status API](#status-api)
- [Logging](#logging)
- [Audit log](#audit-log)
- [Prometheus metrics](#prometheus-metrics)
- [Suggested alerts](#suggested-alerts)
- [Job processing model](#job-processing-model)
- [Troubleshooting](#troubleshooting)

## Lifecycle

On startup the service:

1. Initializes logging (`LOG_FORMAT`/`LOG_LEVEL`).
2. Loads and **validates** `configs/config.yaml`, exiting non-zero with every
   problem listed if invalid.
3. Loads age recipients and checks for key rotation.
4. Starts the job queue, worker pool (3 workers), and HTTP server.

It shuts down **gracefully** on `SIGINT`/`SIGTERM`, giving in-flight HTTP
requests up to 10 seconds to drain. Send `SIGTERM` (the default for
`docker stop`, systemd, and Kubernetes) to stop it cleanly.

## HTTP endpoints

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| `POST` | `/webhook` | HMAC signature | Receive Forgejo/Gitea push events. |
| `GET` | `/healthz` | none | Liveness — 200 while the process runs. |
| `GET` | `/readyz` | none | Readiness — 503 if the `git` binary isn't on `PATH`. |
| `GET` | `/metrics` | none | Prometheus metrics. |
| `GET` | `/status` | bearer (optional) | List all known job status records (JSON). |
| `GET` | `/status/{id}` | bearer (optional) | One job's status record (JSON). |

Use `/healthz` for liveness probes and `/readyz` for readiness probes — they are
intentionally distinct: a process can be alive but not ready (e.g. `git`
missing). The Docker image's `HEALTHCHECK` targets `/healthz`.

## Status API

If `server.statusToken` is set, pass it as a bearer token:

```sh
curl -fsS -H "Authorization: Bearer $TOKEN" http://localhost:8080/status | jq .
curl -fsS -H "Authorization: Bearer $TOKEN" http://localhost:8080/status/<job-id> | jq .
```

Status records include the repository, branch, commit, current state, retry
count, timestamps, and any error message. Because that is sensitive, **set a
token** whenever the endpoint is reachable off-host; access attempts
(successful and denied) are written to the audit log.

## Logging

Structured logs are emitted to **stderr** via `log/slog`.

| Variable | Values | Default |
|----------|--------|---------|
| `LOG_FORMAT` | `json`, `text` | `json` |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` |

Use `json` for log aggregation; `text` for local debugging. Key job events
(`processing job`, `mirrored repository`, `encrypted commits`, `pushed
encrypted history to github`) carry structured fields like `job_id`, `owner`,
`repo`, `branch`, and `commit`.

## Audit log

Security-relevant events are recorded **independently of `LOG_LEVEL`** as
JSON lines, either to `server.auditLogPath` or (if unset) stderr:

| Action | Result(s) | Emitted when |
|--------|-----------|--------------|
| `webhook.verify` | `success`, `failure` | Signature verification outcome. |
| `webhook.replay` | `denied` | Duplicate delivery ID seen. |
| `webhook.enqueue` | `success`, `failure` | Push event enqueued (or not). |
| `status.access` | `success`, `denied` | Status API access attempt. |
| `encryption.key_rotation` | `detected` | Recipient set changed since last run. |

Ship this file to your SIEM/log store. Repeated `webhook.verify` failures or
`status.access` denials are worth alerting on.

## Prometheus metrics

Served at `/metrics`, namespace `forgejo_mirror`:

| Metric | Type | Labels | Meaning |
|--------|------|--------|---------|
| `forgejo_mirror_build_info` | gauge | `version` | Always 1; exposes build version. |
| `forgejo_mirror_webhook_requests_total` | counter | `outcome` | Webhook requests by outcome (`accepted`, `invalid_signature`, `replay`, `ignored`, `error`). |
| `forgejo_mirror_jobs_total` | counter | `status` | Completed jobs by terminal status (`succeeded`, `failed`). |
| `forgejo_mirror_job_retries_total` | counter | — | Job retry attempts. |
| `forgejo_mirror_job_duration_seconds` | histogram | — | End-to-end job processing time. |
| `forgejo_mirror_queue_depth` | gauge | `status` | Jobs currently in the queue by status. |
| `forgejo_mirror_stage_duration_seconds` | histogram | `stage` | Per-stage time (`clone_fetch`, `encrypt`, `github_push`). |
| `forgejo_mirror_commits_encrypted_total` | counter | — | Source commits encrypted. |
| `forgejo_mirror_github_pushes_total` | counter | `outcome` | GitHub pushes (`success`, `failure`). |
| `forgejo_mirror_key_rotations_total` | counter | — | Recipient rotations detected at startup. |

Example scrape config:

```yaml
scrape_configs:
  - job_name: forgejo-encrypt-mirror
    static_configs:
      - targets: ["forgejo-encrypt-mirror:8080"]
```

## Suggested alerts

```yaml
groups:
  - name: forgejo-encrypt-mirror
    rules:
      - alert: MirrorJobsFailing
        expr: increase(forgejo_mirror_jobs_total{status="failed"}[15m]) > 0
        for: 5m
        annotations: { summary: "Mirror jobs are failing" }

      - alert: GithubPushesFailing
        expr: increase(forgejo_mirror_github_pushes_total{outcome="failure"}[15m]) > 0
        annotations: { summary: "Pushes to GitHub are failing" }

      - alert: WebhookSignatureFailures
        expr: increase(forgejo_mirror_webhook_requests_total{outcome="invalid_signature"}[15m]) > 5
        annotations: { summary: "Repeated invalid webhook signatures" }

      - alert: MirrorQueueBacklog
        expr: forgejo_mirror_queue_depth{status="queued"} > 50
        for: 10m
        annotations: { summary: "Mirror job backlog building up" }
```

## Job processing model

- Webhook push events are normalized to a job (`owner`, `repo`, `branch`,
  `commit`) and enqueued (queue capacity 100).
- A pool of **3 workers** processes jobs concurrently. Each job:
  clone/fetch → resolve commit → encrypt new commits → (optional) push to GitHub.
- Completed job status records are retained and periodically cleaned up
  (entries older than 24h are pruned every 10 minutes).
- **Run a single instance per cache directory** — the local mirrors and
  encrypted repos are not safe for concurrent writers across processes.

## Troubleshooting

| Symptom | Likely cause / fix |
|---------|-------------------|
| Exits immediately with `invalid configuration` | Fix every problem listed in the error; the config is validated up front. |
| `/readyz` returns 503 | `git` not found on `PATH`. Install git (bundled in the Docker image). |
| Webhooks return 401 `invalid signature` | Secret mismatch between Forgejo and `forgejo.webhookSecrets`; check `webhook.verify` audit events. |
| Webhooks return 200 `ignored` | Non-push event (e.g. ping/tag) or non-branch push — expected, acknowledged so Forgejo doesn't mark delivery failed. |
| Encrypted history built but nothing on GitHub | `github.token` empty (push skipped by design) or destination/permission error — check `github_pushes_total{outcome="failure"}` and logs. |
| `owner … contains invalid characters` | Repo/owner name outside `[A-Za-z0-9_.-]`; not mirrored (path-traversal guard). |
| Warning: config readable by group/other | `chmod 600 configs/config.yaml`. |
| Warning: status endpoints unauthenticated | Set `server.statusToken`. |
| Restart re-mirrors everything | The cache directory was not persisted; mount `git.cacheDir` on a durable volume. |

See also: [configuration.md](configuration.md) · [security.md](security.md)
