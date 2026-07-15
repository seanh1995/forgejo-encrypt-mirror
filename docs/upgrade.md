# Upgrade guide

How to move between versions of `forgejo-encrypt-mirror` safely.

- [Versioning policy](#versioning-policy)
- [Before you upgrade](#before-you-upgrade)
- [General upgrade procedure](#general-upgrade-procedure)
- [Docker](#docker)
- [Docker Compose](#docker-compose)
- [systemd / from source](#systemd--from-source)
- [Kubernetes](#kubernetes)
- [Rolling back](#rolling-back)
- [Compatibility notes](#compatibility-notes)

## Versioning policy

Releases follow [Semantic Versioning](https://semver.org):

- **MAJOR** (`2.0.0`) — breaking changes to config, CLI flags, on-disk cache
  layout, or the encrypted-repo format. Migration steps will be documented here
  and in the [changelog](../CHANGELOG.md).
- **MINOR** (`1.1.0`) — backwards-compatible features. Existing configs and
  caches keep working.
- **PATCH** (`1.0.1`) — backwards-compatible bug/security fixes.

Docker tags: pin to an exact `vX.Y.Z` in production. `vX.Y` tracks the latest
patch of a minor line; `latest` is not published for pinning.

Within a major version:

- **Config is forward-compatible.** New fields have safe defaults; unset fields
  behave as before.
- **The cache and encrypted-repo format are stable.** Upgrading does not require
  re-mirroring, and existing backups remain decryptable with the same age
  identity.

## Before you upgrade

1. **Read the [changelog](../CHANGELOG.md)** for the target version.
2. **Verify a restore works** on the current version (see
   [security.md](security.md#restoring-and-disaster-recovery)) — the surest way
   to know your backups are intact before changing anything.
3. **Note your current version** (from the startup log or
   `forgejo_mirror_build_info`) so you can roll back.
4. The **age private key is independent of the software version** — keep it
   safe; no version has ever required changing it.

## General upgrade procedure

1. Pull/build the new version.
2. Apply any config changes noted in the changelog (usually none within a major
   line).
3. Stop the old instance (`SIGTERM` for a graceful drain).
4. Start the new instance against the **same** `cacheDir`.
5. Confirm health and a test push:

   ```sh
   curl -fsS localhost:8080/healthz
   curl -fsS localhost:8080/metrics | grep forgejo_mirror_build_info
   ```

Because mirroring is incremental and resumes from the `Source-Commit` trailer in
the encrypted history, a restart picks up exactly where it left off — no repo is
re-processed unnecessarily.

## Docker

```sh
docker pull ghcr.io/seanh1995/forgejo-encrypt-mirror:v1.0.0
docker stop forgejo-encrypt-mirror && docker rm forgejo-encrypt-mirror
docker run -d --name forgejo-encrypt-mirror \
  -p 8080:8080 \
  -v "$(pwd)/configs/config.yaml:/app/configs/config.yaml:ro" \
  -v mirror-cache:/app/cache \
  ghcr.io/seanh1995/forgejo-encrypt-mirror:v1.0.0
```

Reuse the **same** `mirror-cache` volume to keep incremental state.

## Docker Compose

```sh
# bump the image tag in examples/docker-compose.yml, then:
docker compose -f examples/docker-compose.yml pull
docker compose -f examples/docker-compose.yml up -d
```

## systemd / from source

```sh
git fetch --tags
git checkout v1.0.0
go build -ldflags "-s -w -X main.version=v1.0.0" -o mirror ./cmd/mirror
sudo systemctl stop forgejo-encrypt-mirror
sudo install -o mirror -g mirror -m 0755 mirror /opt/forgejo-encrypt-mirror/mirror
sudo systemctl start forgejo-encrypt-mirror
sudo journalctl -u forgejo-encrypt-mirror -f
```

## Kubernetes

```sh
kubectl set image deployment/forgejo-encrypt-mirror \
  mirror=ghcr.io/seanh1995/forgejo-encrypt-mirror:v1.0.0
kubectl rollout status deployment/forgejo-encrypt-mirror
```

Keep `strategy: Recreate` and `replicas: 1` so two versions never write to the
same cache PVC at once.

## Rolling back

Roll back by redeploying the previous tag against the same cache:

```sh
docker run ... ghcr.io/seanh1995/forgejo-encrypt-mirror:<previous-version>
```

Within a major version the cache format is stable in both directions, so a
rollback is safe. If a **major** upgrade migrated the cache format, follow the
rollback steps in that version's changelog entry (or start from a fresh cache —
which triggers a full, safe re-mirror).

## Compatibility notes

- **`forgejo.webhookSecret` (singular)** is deprecated in favor of
  `forgejo.webhookSecrets` (list). The old field still works and is treated as
  an additional valid secret, but migrate to the list for rotation support.
- **`github.private`** defaults to `true` when unset. If you rely on that
  default, no action is needed; set it explicitly to document intent.
