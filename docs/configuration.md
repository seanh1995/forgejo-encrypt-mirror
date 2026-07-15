# Configuration

The service reads a single YAML file, `configs/config.yaml`, resolved relative
to its working directory. It is validated at startup: **every** problem found is
reported at once and the process exits non-zero, so an invalid config never runs
partially.

Start from [configs/config.example.yaml](../configs/config.example.yaml), which
documents every field inline:

```sh
cp configs/config.example.yaml configs/config.yaml
chmod 600 configs/config.yaml   # the file holds tokens and secrets
```

> The config contains secrets (API tokens, webhook secrets). Restrict it to the
> owner (`chmod 600`). On POSIX systems the service logs a warning at startup if
> the file is group/other-readable.

- [Full reference](#full-reference)
- [`server`](#server)
- [`forgejo`](#forgejo)
- [`github`](#github)
- [`encryption`](#encryption)
- [`git`](#git)
- [Environment variables](#environment-variables)
- [`.encryptignore`](#encryptignore)
- [Minimal configs](#minimal-configs)

## Full reference

| Field | Type | Default | Required | Description |
|-------|------|---------|----------|-------------|
| `server.address` | string | — | yes | Listen address, e.g. `:8080`. |
| `server.statusToken` | string | `""` | no | Bearer token to protect `/status`. Unauthenticated if empty (warned). |
| `server.auditLogPath` | string | `""` | no | Path to JSON-lines audit log. Writes to stderr if empty. |
| `forgejo.url` | string | — | yes | Base URL of the Forgejo/Gitea instance (`http`/`https`). |
| `forgejo.token` | string | `""` | no* | Access token for cloning private repos, sent via HTTP header. |
| `forgejo.webhookSecret` | string | `""` | no | **Deprecated** single secret; use `webhookSecrets`. |
| `forgejo.webhookSecrets` | list | `[]` | recommended | Ordered list of valid webhook secrets (enables rotation). |
| `github.owner` | string | `""` | no | Default destination owner/org. |
| `github.ownerMap` | map | `{}` | no | Per-source-owner destination overrides. |
| `github.repoNameFormat` | string | `{repo}` | no | Destination repo name template (`{owner}`, `{repo}`). |
| `github.token` | string | `""` | no | GitHub PAT. If empty, encrypted history is built but **not** pushed. |
| `github.autoCreate` | bool | `false` | no | Create the destination repo if missing. |
| `github.private` | bool | `true` | no | Visibility of auto-created repos. Defaults to private. |
| `encryption.recipient` | string | — | yes | age recipient(s): a key, newline-list, or file path. |
| `git.cacheDir` | string | `cache` | no | Root directory for mirrors, encrypted history, and rotation state. |

\* `forgejo.token` is only required to mirror private repositories.

## `server`

```yaml
server:
  address: ":8080"
  statusToken: ""
  auditLogPath: ""
```

- **`address`** — host:port to bind. Required.
- **`statusToken`** — if set, `GET /status` and `GET /status/{id}` require
  `Authorization: Bearer <token>` (compared in constant time). These endpoints
  expose job details (repo names, branches, commit hashes, error messages), so
  **set a token for any deployment reachable outside a trusted network.** If
  empty, the endpoints are open and a warning is logged once at startup.
- **`auditLogPath`** — path to append a JSON-lines
  [audit log](operations.md#audit-log) of security-relevant events. If empty,
  audit events go to stderr.

## `forgejo`

```yaml
forgejo:
  url: "https://forge.example.com"
  token: ""
  webhookSecrets:
    - "current-secret"
```

- **`url`** — base URL of your Forgejo/Gitea instance. Must be `http(s)`.
- **`token`** — access token used to clone/fetch. Sent as a git
  `http.extraHeader` **Authorization** header, never embedded in the remote URL,
  so it can't leak via process listings or git error output. Needs read access
  to the repositories you mirror; only required for private repos.
- **`webhookSecrets`** — ordered list of currently-valid HMAC secrets. A
  delivery is accepted if its signature matches **any** entry, which is what
  enables zero-downtime rotation:
  1. Add the new secret to the front of the list, keep the old one.
  2. Deploy, then update Forgejo's webhook to sign with the new secret.
  3. Once no more deliveries arrive signed with the old secret, remove it.

  If the list is empty (and the deprecated `webhookSecret` is also unset),
  signature verification is **skipped** — do not do this in production.
- **`webhookSecret`** — deprecated single-secret field, treated as an additional
  valid secret if set. Prefer `webhookSecrets`.

## `github`

Optional. If `github.token` is empty, the service still mirrors and encrypts
locally under `cacheDir` but skips the push — useful for local-only backups or
for staging before enabling off-site push.

```yaml
github:
  owner: "backup"
  ownerMap:
    alice: "backup"
    team:  "backup-org"
  repoNameFormat: "{owner}-{repo}"
  token: ""
  autoCreate: true
  private: true
```

**Destination routing.** For a source repo `sourceOwner/sourceRepo`:

- The destination **owner** is resolved in order:
  `ownerMap[sourceOwner]` → `github.owner` → `sourceOwner` unchanged.
- The destination **repo name** is `repoNameFormat` with `{owner}`/`{repo}`
  substituted, defaulting to `{repo}` (unchanged).

To funnel many Forgejo owners into one GitHub org without name collisions, set
`owner: backup-org` and `repoNameFormat: "{owner}-{repo}"`:

```
alice/app  -> backup-org/alice-app
bob/game   -> backup-org/bob-game
team/docs  -> backup-org/team-docs
```

- **`token`** — GitHub PAT. Sent via HTTP Basic auth (`x-access-token:<token>`)
  for git pushes and as a bearer token for the REST API. Needs `repo` scope
  (classic) or `Contents` + `Administration` read/write (fine-grained).
- **`autoCreate`** — create the destination repository via the GitHub API if it
  doesn't exist. Handles both user and organization accounts.
- **`private`** — visibility of auto-created repos. **Defaults to `true`.** Only
  affects repos this service creates; pre-existing repos are left as-is. Leaving
  encrypted backups private is strongly recommended.

## `encryption`

```yaml
encryption:
  recipient: "age1ql3z...aqmcac8p"
```

- **`recipient`** — one or more age X25519 **public** keys. Accepts:
  - a single recipient string (`age1…`),
  - multiple recipients separated by newlines, or
  - a path to a file containing one recipient per line (blank lines and `#`
    comments ignored).

  Listing multiple recipients encrypts every file so that **any** of the
  corresponding private keys can decrypt it — use this for key rotation and for
  break-glass/escrow keys. See
  [security.md](security.md#key-rotation) for the rotation workflow.

Generate a keypair with:

```sh
go run filippo.io/age/cmd/age-keygen@latest -o identity.txt
```

The private key stays in `identity.txt` (offline); only the `age1…` public line
goes into the config.

## `git`

```yaml
git:
  cacheDir: "cache"
```

- **`cacheDir`** — root directory holding, per repo:
  - `<owner>/<repo>.git` — the bare mirror clone of the source,
  - `<owner>/<repo>.enc` — the encrypted working repository (ciphertext +
    `manifest.json`) that gets pushed,
  - `.encryption-recipients` — the key-rotation state file.

  **Persist this directory.** It is safe to lose (a fresh run re-mirrors from
  scratch), but keeping it makes restarts incremental and fast.

## Environment variables

Logging is configured via environment variables (not the YAML file):

| Variable | Values | Default | Effect |
|----------|--------|---------|--------|
| `LOG_FORMAT` | `json`, `text` | `json` | Structured log output format (stderr). |
| `LOG_LEVEL` | `debug`, `info`, `warn`, `error` | `info` | Minimum log level. |

The build version is injected at compile time via
`-ldflags "-X main.version=..."` and surfaced in the startup log and the
`forgejo_mirror_build_info` metric.

## `.encryptignore`

Place an optional `.encryptignore` file at the root of a **source** repository
to exclude files from encryption (and therefore from the backup). Syntax is a
practical subset of `.gitignore`:

- Blank lines and `#` comments are ignored.
- A leading `/` anchors the pattern to the repo root.
- `*` and `?` match within a single path segment.
- A trailing `/` is accepted (and stripped).
- **Not supported:** `**`, negation (`!`).

Example:

```gitignore
# exclude build output anywhere
node_modules/
dist/
*.log

# exclude a root-level secrets dir only
/secrets/
```

`.git` is always skipped automatically. See
[examples/.encryptignore](../examples/.encryptignore).

## Minimal configs

**Local-only backup (no GitHub push):**

```yaml
server:
  address: ":8080"
forgejo:
  url: "https://forge.example.com"
  token: "forgejo-token"
  webhookSecrets: ["shared-secret"]
encryption:
  recipient: "age1ql3z...aqmcac8p"
```

**Off-site backup to GitHub:**

```yaml
server:
  address: ":8080"
  statusToken: "long-random-token"
forgejo:
  url: "https://forge.example.com"
  token: "forgejo-token"
  webhookSecrets: ["shared-secret"]
github:
  owner: "backup"
  repoNameFormat: "{owner}-{repo}"
  token: "github-pat"
  autoCreate: true
  private: true
encryption:
  recipient: "age1ql3z...aqmcac8p"
git:
  cacheDir: "/var/lib/forgejo-encrypt-mirror/cache"
```

See more in [examples/](../examples/).
