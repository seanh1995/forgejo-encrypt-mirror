# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project
adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2026-07-15

First stable release. `forgejo-encrypt-mirror` mirrors Forgejo/Gitea
repositories into an age-encrypted git history and optionally pushes the
encrypted result to GitHub, so a third-party host can hold durable, off-site,
version-preserving backups without ever seeing plaintext.

### Added

- **Core service** (`cmd/mirror`) — HTTP server, job queue, and a 3-worker pool
  that clones, encrypts, and pushes on demand, with graceful shutdown on
  SIGINT/SIGTERM.
- **Forgejo/Gitea integration** — signed push webhooks (`/webhook`) with
  HMAC-SHA256 verification (Forgejo, Gitea, and GitHub header formats),
  constant-time comparison, replay protection, and a 10 MiB body cap.
- **Git engine** — bare mirror clones with incremental fetch/prune; credentials
  passed via `http.extraHeader` (never in URLs or argv); repo/owner name
  validation to prevent path traversal and injection.
- **Encryption engine (age)** — per-file X25519 encryption to one or more
  recipients, a `manifest.json` capturing paths/permissions/symlinks, and
  `.encryptignore` support for excluding files.
- **History protection** — encrypted commits reproduce the source's
  author/committer identity and timestamps and carry a `Source-Commit` trailer,
  enabling exact incremental resume with no external state.
- **GitHub integration** — auto-create the destination repository (user or org),
  private by default, and push the encrypted history over token auth.
- **Multi-repository & multi-owner support** — `ownerMap` and `repoNameFormat`
  route many Forgejo owners into configurable GitHub destinations from one
  installation.
- **Security hardening** — audit log of security-relevant events, config
  file-permission warning, secure (0700) scratch directories for transient
  plaintext, token-protected status API with constant-time bearer check.
- **Key & secret rotation** — multiple age recipients with startup rotation
  detection; multiple webhook secrets for zero-downtime rotation.
- **Operations & metrics** — Prometheus metrics at `/metrics`, structured
  `slog` logging (`LOG_FORMAT`/`LOG_LEVEL`), and `/healthz` + `/readyz` probes.
- **Restore tool** (`cmd/restore`) — decrypt a mirror back to plaintext, either
  its current state or a specific historical commit (point-in-time recovery).
- **Docker publishing** — multi-arch images (`linux/amd64`, `linux/arm64`,
  `linux/arm/v7`) built via a non-root, multi-stage Alpine image and published
  to GHCR by CI on tags.
- **Documentation** — installation, configuration, security, operations, and
  upgrade guides, plus deployment examples (Docker Compose, systemd,
  Kubernetes) and configuration scenarios.

[1.0.0]: https://github.com/seanh1995/forgejo-encrypt-mirror/releases/tag/v1.0.0
