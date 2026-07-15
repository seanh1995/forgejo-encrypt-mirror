# Security

This document describes the security model of `forgejo-encrypt-mirror`: what it
protects, what it does not, how encryption and keys work, and how to operate it
safely. For **reporting** a vulnerability, see [SECURITY.md](../SECURITY.md).

- [Design goal](#design-goal)
- [Threat model](#threat-model)
- [What is and isn't protected](#what-is-and-isnt-protected)
- [Encryption details](#encryption-details)
- [Key management](#key-management)
- [Key rotation](#key-rotation)
- [Webhook authentication](#webhook-authentication)
- [Credential handling](#credential-handling)
- [Network & endpoint hardening](#network--endpoint-hardening)
- [Restoring and disaster recovery](#restoring-and-disaster-recovery)
- [Hardening checklist](#hardening-checklist)

## Design goal

The destination (GitHub) is treated as an **untrusted store**. The service's
core guarantee is:

> No plaintext git objects, file contents, or working-tree files are ever
> pushed to the destination. Everything pushed is age ciphertext plus a
> structural manifest. Only a holder of an age private key in the recipient set
> can recover plaintext.

This lets you use a third-party host (GitHub) for durable, off-site,
version-preserving backups without trusting that host — or anyone who
compromises it — with your source.

## Threat model

**Adversaries considered:**

| Adversary | Mitigation |
|-----------|-----------|
| Attacker who reads the GitHub backup repo | Contents are age-encrypted; without a private key they get ciphertext, file sizes, and directory structure only. |
| Compromised GitHub account/token | Same as above; the token can push/delete backups (integrity/availability) but cannot decrypt them. Keep backups private and the token least-privilege. |
| Attacker forging webhook deliveries | HMAC-SHA256 signature verification with constant-time comparison; replay cache; 10 MiB body cap. |
| Attacker replaying a captured webhook | Delivery IDs are remembered (default 24h TTL) and duplicates rejected. |
| Path-traversal / injection via repo names | Owner/repo names are validated against `^[A-Za-z0-9_.-]+$` and rejected if `.`/`..`; tar extraction rejects entries escaping the destination. |
| Token leakage via process listing / logs | Tokens are passed as git `http.extraHeader`, never in URLs or argv. |
| Reader of the status API | Optional bearer token, constant-time comparison, access audited. |

**Explicitly out of scope:**

- **Confidentiality of metadata.** See below — file sizes, structure, count of
  commits, author identities, timestamps, and commit *messages* are not hidden.
- **Protecting the machine running the service.** It necessarily handles
  plaintext and the Forgejo/GitHub tokens; treat it as trusted and harden it
  accordingly.
- **Loss of the age private key.** If every private key in the recipient set is
  lost, backups are unrecoverable **by design**. See [key management](#key-management).
- **Availability of the destination.** The service does not detect or prevent an
  attacker with the GitHub token from deleting the backup.

## What is and isn't protected

The encrypted history preserves enough structure to faithfully restore a repo,
which means some metadata is intentionally *not* encrypted.

**Encrypted (confidential):**

- The **contents of every file** in every mirrored commit (age-encrypted,
  per-file, stored as `<path>.age`).

**Not encrypted (visible to anyone who can read the backup):**

- **File and directory names / paths** — recorded in `manifest.json` and encoded
  in the `.age` filenames.
- **File sizes** (approximately — ciphertext length reveals plaintext length)
  and **file permissions**, and **symlink targets**.
- **Commit graph and metadata** — author/committer names, emails, and
  timestamps are reproduced verbatim on the encrypted commits, and **commit
  messages are copied in plaintext** (plus an appended `Source-Commit` trailer).

> **Important:** if your commit messages, file paths, or branch names are
> themselves sensitive, this tool does not hide them. Treat the backup's
> metadata as readable by anyone with access to the destination repository, and
> keep the destination private.

## Encryption details

- **Algorithm:** [age](https://age-encryption.org) with X25519 recipients
  (`filippo.io/age`). Each file is encrypted independently to all configured
  recipients.
- **Per-commit flow:** for each new source commit, the tree is exported to a
  scratch directory (created `0700`, owner-only), every regular file is
  encrypted into the `.enc` working repo, and the result is committed. The
  scratch directory holds plaintext only transiently and is removed afterward.
- **Manifest:** `manifest.json` records, per entry, the original path,
  permission bits, and — for symlinks — the target. It is what makes a faithful
  restore possible.
- **`.git` is never encrypted** and never copied into the encrypted tree; the
  encrypted history is a *separate* git repository whose commits mirror the
  source's, not a re-encryption of the source's git objects.
- **Determinism:** encrypted commits use `--allow-empty` so every source commit
  maps to exactly one encrypted commit, keeping resume tracking exact even when
  a commit's encrypted output is unchanged.

## Key management

You control two halves of an age keypair:

- **Recipient (public key, `age1…`)** — goes in `encryption.recipient`. Safe to
  store in config/secrets management. Used to *encrypt*.
- **Identity (private key)** — the contents of the `age-keygen` output file.
  Used to *decrypt* (restore). **This is the crown jewel.**

Guidance:

- **Store the private key offline**, separate from the service and from the
  GitHub backup. If an attacker has both the backup and the private key, the
  encryption provides no protection.
- **Never commit the private key** to any repository (including the source
  repos being mirrored — it would then end up encrypted in the backup, but also
  in your Forgejo history).
- **Back up the private key** independently and durably. Losing it makes every
  backup permanently unrecoverable.
- **Consider multiple recipients** (see rotation) so more than one key can
  decrypt — e.g. an operational key plus a cold escrow key held by someone else.
- Restrict `configs/config.yaml` to `chmod 600`; it holds tokens even though it
  only holds the *public* age key.

## Key rotation

Because files are encrypted to a *set* of recipients, rotation is additive:

1. Add the new recipient **alongside** the old one in `encryption.recipient`
   (newline-separated or one-per-line in a recipients file):

   ```yaml
   encryption:
     recipient: |
       age1NEWKEY...
       age1OLDKEY...
   ```

2. Restart. The service detects the change (comparing against
   `<cacheDir>/.encryption-recipients`), logs a warning, records an
   `encryption.key_rotation` audit event, and increments the
   `forgejo_mirror_key_rotations_total` metric.

3. **New** encrypted commits are now readable by both keys. **Existing** history
   remains encrypted to whatever recipients produced it — the old key can still
   decrypt old commits until they are re-encrypted.

To fully re-encrypt existing history to the new recipient set, delete that
repo's `.enc` working directory (and let it rebuild) or the whole cache; the
next run replays the source history and re-encrypts it to the current
recipients. Only drop the old key from the recipient set once you no longer need
to decrypt the pre-rotation portion of any live backup.

> Rotation detection is **not** triggered on first run (there is nothing to
> rotate from), and any add/remove/replace of recipients counts as a rotation.

## Webhook authentication

- Signatures are verified as **HMAC-SHA256** over the raw request body, using
  `crypto/hmac.Equal` (constant-time). Both Forgejo/Gitea hex headers
  (`X-Forgejo-Signature`, `X-Gitea-Signature`) and the GitHub-style
  `X-Hub-Signature-256: sha256=…` header are accepted.
- Verification **fails closed**: an empty secret or missing signature header is
  rejected (when any secret is configured).
- **Multiple secrets** are supported for zero-downtime rotation
  (`forgejo.webhookSecrets`); a delivery is accepted if it matches any.
- **Replay protection** remembers delivery IDs (default 24h) and rejects
  duplicates.
- The request body is capped at **10 MiB**.
- Configure the same secret in Forgejo's webhook settings. Prefer HTTPS
  (terminate TLS at a reverse proxy) so the secret and payload aren't sent in
  clear text.

## Credential handling

- **Forgejo and GitHub tokens are never placed in remote URLs or command-line
  arguments.** They are injected as an HTTP `Authorization` header via
  `git -c http.extraHeader=…`, so they don't appear in process listings, git's
  error/log output, or the mirror's own logs.
- `GIT_TERMINAL_PROMPT=0` is set on all git invocations so a bad credential
  fails fast instead of blocking on an interactive prompt.
- Use **least-privilege tokens**: read-only on the Forgejo side; on GitHub, a
  fine-grained token scoped to only the destination org/repos with `Contents`
  and (for `autoCreate`) `Administration` write.

## Network & endpoint hardening

| Endpoint | Auth | Notes |
|----------|------|-------|
| `POST /webhook` | HMAC signature | The only endpoint that mutates state. |
| `GET /status`, `GET /status/{id}` | Optional bearer token | Exposes job details; **set `server.statusToken`**. |
| `GET /healthz` | none | Liveness; returns 200 while running. |
| `GET /readyz` | none | Readiness; 503 if `git` is unavailable. |
| `GET /metrics` | none | Prometheus metrics; restrict via network policy if sensitive. |

Recommendations:

- Put the service behind a reverse proxy that terminates **TLS**.
- Expose only `/webhook` publicly (to reach Forgejo); keep `/status`,
  `/metrics`, `/healthz`, `/readyz` on an internal network or behind auth.
- Always set `server.statusToken` when the status API is reachable off-host.
- Run as a **non-root** user (the Docker image and systemd unit already do).

## Restoring and disaster recovery

Restoring requires only the encrypted backup and an age **identity** (private
key) in the recipient set — no running service and no Forgejo access.

**Restore the latest state:**

```sh
go run ./cmd/restore \
  -identity identity.txt \
  -encrypted cache/<owner>/<repo>.enc \
  -out ./restored
```

**Restore a point in time** (any historical commit of the encrypted repo):

```sh
# find the commit in the encrypted repo's log (messages/authors are preserved)
git -C cache/<owner>/<repo>.enc log --oneline

go run ./cmd/restore \
  -identity identity.txt \
  -encrypted cache/<owner>/<repo>.enc \
  -out ./restored-at-abc123 \
  -commit abc123
```

**Restoring from GitHub** (cache lost): clone the encrypted backup repo from
GitHub, then point `-encrypted` at the clone:

```sh
git clone https://github.com/<owner>/<repo>.git ./enc-clone
go run ./cmd/restore -identity identity.txt -encrypted ./enc-clone -out ./restored
```

**Test your restores regularly.** A backup you have never restored is a backup
you don't know you have. Periodically run a restore against a scratch directory
and diff it against the source.

## Hardening checklist

- [ ] age **private key stored offline** and backed up independently.
- [ ] Destination GitHub repos are **private** (`github.private: true`).
- [ ] `configs/config.yaml` is `chmod 600`.
- [ ] `server.statusToken` set if `/status` is reachable off-host.
- [ ] `forgejo.webhookSecrets` set (verification not skipped).
- [ ] Forgejo webhook delivered over **HTTPS**.
- [ ] Forgejo token is **read-only**; GitHub token is **least-privilege**.
- [ ] Service runs as **non-root**; cache dir owned by that user.
- [ ] `/metrics` and `/status` not exposed to the public internet.
- [ ] `auditLogPath` configured and shipped to your log store.
- [ ] A **test restore** has been performed successfully.
