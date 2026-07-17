# Getting Started

A single, ordered walkthrough for a correct first-time setup: from nothing to
a verified, encrypted, off-site mirror. Every step spells out exactly what to
click or type — no prior familiarity with age, Forgejo tokens, or GitHub
tokens assumed.

Commands below use a Unix-style shell (macOS, Linux, WSL, or **Git Bash on
Windows** — the "Git Bash" that ships with [Git for
Windows](https://gitforwindows.org/)). If you're on Windows, run them in Git
Bash or WSL rather than PowerShell/cmd.

- [Prerequisites](#prerequisites)
- [0. Decide your topology](#0-decide-your-topology)
- [1. Generate an age keypair](#1-generate-an-age-keypair)
- [2. Create a Forgejo access token](#2-create-a-forgejo-access-token)
- [3. Create a GitHub token (optional, for off-site push)](#3-create-a-github-token-optional-for-off-site-push)
- [4. Write the config](#4-write-the-config)
- [5. Run the service](#5-run-the-service)
- [6. Add the Forgejo webhook](#6-add-the-forgejo-webhook)
- [7. Verify end-to-end](#7-verify-end-to-end)
- [8. Harden before relying on it](#8-harden-before-relying-on-it)
- [Common mistakes](#common-mistakes)

## Prerequisites

- **Docker** installed on the machine that will run the service (recommended
  path — [Docker Desktop](https://docs.docker.com/get-docker/) on
  Windows/macOS, `docker.io`/`docker-ce` on Linux). Check with `docker
  --version`. If you'd rather run from source instead, you need **Go
  1.26.5+** — see [installation.md#from-source](installation.md#from-source).
- Admin/owner access to the **Forgejo (or Gitea) instance** hosting the repos
  you want to back up — enough to create an access token and add webhooks.
- *(Optional)* A **GitHub account** if you want an off-site copy, not just a
  local encrypted backup.
- **Network reachability from Forgejo to this service.** Forgejo has to be
  able to reach the mirror's `/webhook` endpoint over HTTP(S). If your
  Forgejo instance is on the public internet but you're running the mirror
  on your own laptop for a first test, it won't be reachable — use a tunnel
  such as [`ngrok`](https://ngrok.com/) (`ngrok http 8080`) or
  [`cloudflared`](https://github.com/cloudflare/cloudflared) to get a public
  URL for testing, then move the service somewhere permanently reachable
  (a VPS, the same network as Forgejo, etc.) before relying on it.

## 0. Decide your topology

- **Local-only backup** — encrypt and keep the history in `git.cacheDir`, no
  GitHub push. Leave `github.token` empty. See
  [examples/config.local-only.yaml](../examples/config.local-only.yaml).
- **Off-site backup to GitHub** — same as above, plus push the encrypted
  history to a private GitHub repo. Needs a GitHub token.
- **Multiple Forgejo owners → one GitHub org** — set `github.owner` and
  `github.repoNameFormat`. See
  [examples/config.github-multi-owner.yaml](../examples/config.github-multi-owner.yaml).

This guide uses plain `docker run` for the run step. For Compose, systemd, or
Kubernetes instead, see [installation.md](installation.md) — everything else
in this guide (tokens, config, webhook, verification) is identical either way.

## 1. Generate an age keypair

Do this **first and offline** — it's the only key that can ever decrypt your
backups, and nothing else in this guide depends on how you get it.

**Option A — you already have Go installed:**

```sh
go run filippo.io/age/cmd/age-keygen@latest -o identity.txt
```

**Option B — no Go, install `age` directly:**

- macOS: `brew install age`
- Debian/Ubuntu: `sudo apt install age`
- Windows (in Git Bash, via [Scoop](https://scoop.sh/)): `scoop install age`
- Or download a prebuilt binary from the [age
  releases page](https://github.com/FiloSottile/age/releases).

Then run:

```sh
age-keygen -o identity.txt
```

Either way, you'll see output like:

```
Public key: age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p
```

- The `age1…` line is the **recipient** (public key) — copy it now, it goes
  into the config in step 4.
- `identity.txt` (created in your current directory) is the **private key**.
  Move it somewhere durable and offline right now: a password manager, an
  encrypted USB drive, a printed copy in a safe. Do **not** leave it on the
  machine running the service, and never commit it to any git repository —
  including the repos being mirrored (it would then end up encrypted *inside*
  the backup, but also sitting in plaintext in your Forgejo history). If
  every copy of this file is lost, every backup becomes permanently
  unrecoverable — there is no recovery path around that by design. See
  [security.md#key-management](security.md#key-management).

## 2. Create a Forgejo access token

1. Log into Forgejo as an account that has (at least read) access to the
   repositories you want to mirror.
2. Click your **avatar** (top right) → **Settings**.
3. In the left sidebar, click **Applications**.
4. Under **Manage Access Tokens**, enter a name (e.g. `forgejo-encrypt-mirror`).
5. Set the permission for **repository** to **Read** (that's all this service
   ever needs — it only clones/fetches, never pushes to Forgejo).
6. Click **Generate Token**.
7. **Copy the token immediately** — Forgejo shows it exactly once and you
   cannot retrieve it again later (you'd have to delete it and make a new
   one).

You only need this token if any of the repos you're mirroring are **private**.
Public repos can be mirrored without one.

## 3. Create a GitHub token (optional, for off-site push)

Skip this step entirely for a local-only backup — leave `github.token` blank
in step 4 and come back to this later if you change your mind.

1. Decide the destination now: a **dedicated org or account** for backups is
   easier to lock down than mixing backups into your main account.
2. On GitHub: click your avatar (top right) → **Settings** → (scroll down)
   **Developer settings** → **Personal access tokens** → **Fine-grained
   tokens** → **Generate new token**.
3. Give it a name and expiration.
4. **Resource owner**: pick the org/account you decided on in step 1.
5. **Repository access**: if you'll set `github.autoCreate: true` (the
   service creates the destination repo for you), choose **All repositories**
   for that owner — the specific repos don't exist yet, so you can't select
   them individually. Otherwise, create the destination repos yourself first
   and choose **Only select repositories**.
6. **Permissions** → **Repository permissions**:
   - **Contents**: Read and write.
   - **Administration**: Read and write — only needed if `autoCreate` is on.
7. Click **Generate token** and **copy it immediately** — like Forgejo,
   GitHub only shows it once.

*(Prefer a classic token instead? **Developer settings** → **Personal access
tokens** → **Tokens (classic)** → **Generate new token** → check the `repo`
scope.)*

## 4. Write the config

```sh
cp configs/config.example.yaml configs/config.yaml
chmod 600 configs/config.yaml   # it holds tokens
```

You need two random secrets: `server.statusToken` and a webhook secret for
`forgejo.webhookSecrets`. Generate each with:

```sh
openssl rand -hex 32
```

(No `openssl`? Any password manager's "generate password" feature works —
just make it 32+ random characters.) Run the command twice, once for each
secret, and keep them straight — they're different values.

Now edit `configs/config.yaml` with your editor of choice. At minimum:

```yaml
server:
  address: ":8080"
  statusToken: "paste-first-random-value-here"   # protects /status

forgejo:
  url: "https://forge.example.com"               # your Forgejo's base URL
  token: "paste-forgejo-token-from-step-2"
  webhookSecrets:
    - "paste-second-random-value-here"           # used again in step 6

github:                                          # omit this whole block for local-only
  owner: "backup"                                # the org/account from step 3
  token: "paste-github-token-from-step-3"
  autoCreate: true
  private: true

encryption:
  recipient: "age1...-the-public-key-from-step-1"

git:
  cacheDir: "cache"
```

Every field, including the routing/rename options for multi-owner setups, is
documented in [configuration.md](configuration.md). The config is validated
at startup — a typo here fails loudly (with the exact problem printed) rather
than silently misbehaving, so don't worry about getting every field perfect
on the first try.

## 5. Run the service

```sh
docker run -d --name forgejo-encrypt-mirror \
  -p 8080:8080 \
  -v "$(pwd)/configs/config.yaml:/app/configs/config.yaml:ro" \
  -v mirror-cache:/app/cache \
  ghcr.io/seanh1995/forgejo-encrypt-mirror:latest
```

Check it started cleanly:

```sh
docker logs forgejo-encrypt-mirror
curl -fsS http://localhost:8080/healthz   # should print: healthy
curl -fsS http://localhost:8080/readyz    # should print: ready
```

If the container immediately crash-loops with `load configuration: permission
denied`, the bind-mounted config isn't readable by the image's non-root
`mirror` user — this is the single most common first-run problem. Fix it with
the `chown` steps in
[installation.md#docker-recommended](installation.md#docker-recommended).

If you're testing on your own machine (not a server Forgejo can already
reach), start your tunnel now (see [Prerequisites](#prerequisites)) and note
the public URL it gives you (e.g. `https://abcd1234.ngrok.io`) — you'll need
it in the next step.

## 6. Add the Forgejo webhook

You can add this per-repository, or once for a whole organization/user (which
covers every repo under it).

1. In Forgejo, open the **repository** (or **organization**) you want to
   mirror.
2. Click the **Settings** tab.
3. In the left sidebar, click **Webhooks**.
4. Click **Add Webhook** and choose **Forgejo** from the dropdown (the
   **Gitea** option also works — the formats are compatible).
5. Fill in the form:

   | Field | Value |
   |-------|-------|
   | Target URL | `https://<your-host-or-tunnel>/webhook` — use HTTPS in anything but a local test |
   | HTTP Method | `POST` |
   | POST Content Type | `application/json` |
   | Secret | the **second** random value from step 4 (the one in `forgejo.webhookSecrets`) |
   | Trigger On | select **Push Events** only (leave the rest unchecked) |
   | Branch filter | leave blank to match all branches, or restrict as needed |

6. Click **Add Webhook**.
7. Optionally click the webhook you just created, then **Test Delivery** to
   fire a connectivity check. Note this sends a `ping`, not a `push` event —
   it confirms Forgejo can reach the service and the connection succeeds, but
   the real test is pushing an actual commit (next step).

## 7. Verify end-to-end

1. Push a commit to a mirrored repository (a trivial change is fine — e.g.
   edit the README and `git push`).
2. Watch the logs:
   ```sh
   docker logs -f forgejo-encrypt-mirror
   ```
   You should see, in order: `processing job` → `mirrored repository` →
   `encrypted commits` → (if GitHub is configured) `pushed encrypted history
   to github`.
3. Check the encrypted history landed locally:
   ```sh
   docker exec forgejo-encrypt-mirror ls /app/cache/<owner>/<repo>.enc
   ```
4. If GitHub is configured, open the destination repo in your browser and
   confirm it contains only `*.age` files and a `manifest.json` — **never**
   plaintext source files.
5. Do a **test restore** now, while it's easy to compare against the source.
   This needs to run somewhere with access to both `identity.txt` and the
   cache — typically not inside the container:
   ```sh
   docker cp forgejo-encrypt-mirror:/app/cache/<owner>/<repo>.enc ./repo.enc
   go run ./cmd/restore \
     -identity identity.txt \
     -encrypted ./repo.enc \
     -out ./restored
   diff -r ./restored /path/to/your/local/checkout
   ```

If step 5 doesn't produce an exact match, stop and fix it before trusting
this as a backup — nothing else here matters if restore doesn't work. See
[security.md#restoring-and-disaster-recovery](security.md#restoring-and-disaster-recovery).

**Still stuck?** If nothing shows up in the logs after pushing, or the webhook
delivery fails, check
[operations.md#troubleshooting](operations.md#troubleshooting) first — it
covers the issues people actually hit at this stage, including Forgejo's
`ALLOWED_HOST_LIST` blocking webhooks to private/internal addresses, DNS
resolution failing inside the container, and webhook 401s from a secret
mismatch.

## 8. Harden before relying on it

Run through the full checklist in
[security.md#hardening-checklist](security.md#hardening-checklist) — in short:
private key offline and backed up, destination repos private, config
`chmod 600`, `statusToken` set, webhook over HTTPS, least-privilege tokens,
non-root, `/metrics`/`/status` not public, audit log shipped somewhere, and a
successful test restore.

## Common mistakes

- **Skipping the test restore.** A backup that's never been restored isn't a
  verified backup.
- **Losing the age private key.** There is no backdoor — back it up
  independently of the service and the GitHub backup itself.
- **Leaving `webhookSecrets` empty.** Signature verification is skipped
  entirely if no secret is configured — never do this in production.
- **Not `chown`-ing the bind-mounted config for Docker.** `chmod 600` alone
  isn't enough since the container reads the file as its own UID, not yours.
- **Forgejo can't reach the mirror.** If you never see `processing job` in
  the logs after pushing, this is almost always a network-reachability
  problem between Forgejo and the service, not a config error — check the
  webhook's delivery history/response in Forgejo's UI first.
- **Making the destination repo public.** `github.private` only affects
  repos this service *creates* — check manually if the repo already existed.

Next: [configuration.md](configuration.md) · [operations.md](operations.md) ·
[security.md](security.md)
