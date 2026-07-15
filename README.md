# forgejo-encrypt-mirror

An automated, encrypted backup service for Forgejo/Gitea repositories.

`forgejo-encrypt-mirror` mirrors your Forgejo (or Gitea) repositories into an
**[age](https://age-encryption.org)-encrypted git history** and — optionally —
pushes that encrypted history to GitHub (or any git remote). GitHub becomes an
off-site backup that holds a full, versioned copy of your repositories while
never seeing a single byte of your plaintext code, commit contents, or file
names' contents.

> **Security-first by design:** no plaintext git objects, file contents, or
> working-tree files are ever pushed to the destination. Only holders of the
> corresponding age private key can decrypt a backup.

[![Docker Publish](https://github.com/seanh1995/forgejo-encrypt-mirror/actions/workflows/docker-publish.yml/badge.svg)](https://github.com/seanh1995/forgejo-encrypt-mirror/actions/workflows/docker-publish.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

---

## Contents

- [How it works](#how-it-works)
- [Features](#features)
- [Quick start](#quick-start)
- [Restoring a backup](#restoring-a-backup)
- [Documentation](#documentation)
- [Contributing](#contributing)
- [Security](#security)
- [License](#license)

## How it works

```
                webhook (push event)
  Forgejo  ─────────────────────────►  forgejo-encrypt-mirror
     ▲                                        │
     │ git clone --mirror (token in header)   │  for each new source commit:
     └────────────────────────────────────────┤    1. export tree
                                              │    2. encrypt every file with age
                                              │    3. commit with original
                                              │       author/date + Source-Commit
                                              ▼
                                   encrypted git history (cache/)
                                              │
                                              │ git push (token in header)
                                              ▼
                                      GitHub (private repo)
                                   ── ciphertext + manifest only ──
```

1. Forgejo sends a signed webhook when a branch is pushed.
2. The service verifies the HMAC-SHA256 signature, then mirrors the source
   repository into a local bare clone.
3. For every source commit not yet mirrored, it exports the commit's tree,
   encrypts each file to your age recipient(s), and commits the ciphertext into
   a separate working repository — **preserving the original author, committer,
   and timestamps**, and recording a `Source-Commit` trailer so mirroring can
   resume exactly where it left off.
4. The resulting encrypted history is pushed to GitHub. The destination repo is
   created automatically (private by default) if it doesn't exist.

The encrypted repository stores each file as `<path>.age` alongside a
`manifest.json` describing the original paths, permissions, and symlinks — so a
backup can be decrypted back into an exact copy of the tree at any commit. See
[docs/security.md](docs/security.md) for the full threat model.

## Features

- **Encrypted history** — every file in every commit is age-encrypted before it
  leaves the machine. Plaintext never reaches the destination.
- **Full history preservation** — original author/committer identity and
  timestamps are reproduced on each encrypted commit; incremental mirroring
  resumes from the last mirrored commit with no external state.
- **Point-in-time restore** — decrypt the current state or any historical commit
  with the bundled `restore` tool.
- **Multi-repository & multi-owner routing** — mirror any number of repos, and
  map different Forgejo owners to different GitHub destinations from a single
  installation.
- **Webhook security** — HMAC-SHA256 signature verification (Forgejo, Gitea, and
  GitHub header formats), constant-time comparison, replay protection, and a
  10 MiB body cap.
- **Key rotation** — multiple age recipients and rotation detection; multiple
  webhook secrets for zero-downtime secret rotation.
- **Observability** — Prometheus metrics, structured `slog` logging, and a
  dedicated security audit log.
- **Operations-ready** — `/healthz` and `/readyz` probes, graceful shutdown,
  token-protected status API.
- **Portable containers** — multi-arch images (`linux/amd64`, `linux/arm64`,
  `linux/arm/v7`) built from a small, non-root Alpine image.

## Quick start

### 1. Generate an age keypair

The **public key** (recipient) goes in the service config. Keep the **private
key** (identity) offline and safe — it is the only thing that can decrypt your
backups.

```sh
go run filippo.io/age/cmd/age-keygen@latest -o identity.txt
# Public key: age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p
```

### 2. Configure

```sh
cp configs/config.example.yaml configs/config.yaml
chmod 600 configs/config.yaml
```

Fill in your Forgejo URL and token, a webhook secret, the age recipient (public
key), and — if you want off-site push — your GitHub token and destination. See
[docs/configuration.md](docs/configuration.md) for every option.

### 3. Run

With Docker:

```sh
docker run -p 8080:8080 \
  -v "$(pwd)/configs/config.yaml:/app/configs/config.yaml:ro" \
  -v mirror-cache:/app/cache \
  ghcr.io/seanh1995/forgejo-encrypt-mirror:v1.0.0
```

Or from source:

```sh
go run ./cmd/mirror
```

### 4. Point Forgejo at it

In your repository (or org) settings, add a webhook:

- **Target URL:** `http://<host>:8080/webhook`
- **HTTP Method:** `POST`
- **Content type:** `application/json`
- **Secret:** the value you set in `forgejo.webhookSecrets`
- **Trigger:** *Push events*

Push a commit and watch the logs — the encrypted mirror appears under `cache/`
and (if configured) on GitHub.

See the full [installation guide](docs/installation.md) for systemd,
Docker Compose, and Kubernetes deployments.

## Restoring a backup

Decrypt an encrypted mirror back into plaintext files with your age identity:

```sh
go run ./cmd/restore \
  -identity identity.txt \
  -encrypted cache/<owner>/<repo>.enc \
  -out ./restored
```

Add `-commit <hash>` to restore the tree as of a specific historical commit.
See [docs/security.md](docs/security.md#restoring-and-disaster-recovery) for a
full disaster-recovery walkthrough.

## Documentation

| Guide | What it covers |
|-------|----------------|
| [Installation](docs/installation.md) | Source, prebuilt binary, Docker, Compose, systemd, Kubernetes |
| [Configuration](docs/configuration.md) | Every config field, environment variables, `.encryptignore` |
| [Security](docs/security.md) | Threat model, key management, rotation, restore, hardening |
| [Operations](docs/operations.md) | Endpoints, Prometheus metrics, logging, audit log, alerting |
| [Upgrade](docs/upgrade.md) | Version policy and upgrade procedures |
| [Examples](examples/) | Docker Compose, systemd unit, `.encryptignore`, config scenarios |
| [Changelog](CHANGELOG.md) | Release history |

## Contributing

Contributions are welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) for the
development workflow, coding standards, and how to run the test suite.

## Security

To report a vulnerability, please follow the process in
[SECURITY.md](SECURITY.md). Do **not** open a public issue for security reports.

## License

Released under the [MIT License](LICENSE).
