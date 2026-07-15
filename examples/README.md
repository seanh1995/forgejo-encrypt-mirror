# Examples

Ready-to-adapt configuration and deployment examples. See
[docs/configuration.md](../docs/configuration.md) for the full reference.

| File | Purpose |
|------|---------|
| [docker-compose.yml](docker-compose.yml) | Run the service with Docker Compose + a persistent cache volume. |
| [systemd/forgejo-encrypt-mirror.service](systemd/forgejo-encrypt-mirror.service) | Hardened systemd unit for a from-source/binary install. |
| [.encryptignore](.encryptignore) | Exclude files from encryption; place at a source repo's root. |
| [config.local-only.yaml](config.local-only.yaml) | Encrypt locally, no GitHub push. |
| [config.github-multi-owner.yaml](config.github-multi-owner.yaml) | Fan many Forgejo owners into one private GitHub org. |

The canonical, fully-commented config template lives at
[../configs/config.example.yaml](../configs/config.example.yaml).

## Generating an age keypair

Every config needs an `encryption.recipient` (the age **public** key):

```sh
go run filippo.io/age/cmd/age-keygen@latest -o identity.txt
# Public key: age1ql3z7hjy54pw3hyww5ayyfg7zqgvc7w3j2elw8zmrj2kg5sfn9aqmcac8p
```

Put the `age1…` line in the config; keep `identity.txt` (the private key)
offline — it is the only thing that can decrypt your backups. See
[docs/security.md](../docs/security.md#key-management).
