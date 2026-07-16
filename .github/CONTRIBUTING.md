# Contributing

Thanks for your interest in improving `forgejo-encrypt-mirror`. This document
covers how to set up, build, test, and submit changes.

## Getting started

Prerequisites:

- **Go 1.26.5+** (matching the `go` directive in [go.mod](go.mod)).
- **`git` CLI** on `PATH` (the service and its tests shell out to it).

```sh
git clone https://github.com/seanh1995/forgejo-encrypt-mirror.git
cd forgejo-encrypt-mirror
go build ./...
go test ./...
```

## Project layout

```
cmd/mirror     — the mirror service (main entrypoint)
cmd/restore    — the decrypt/restore CLI
internal/
  config       — YAML config loading, validation, destination routing
  webhook      — webhook parsing, signature verification, replay cache
  queue        — job queue and worker pool
  git          — git engine, encrypted-history construction, name validation
  encrypt      — age encryption, manifest, ignore, key rotation
  github       — minimal GitHub REST client
  server       — HTTP server and endpoints
  logging      — slog setup
  metrics      — Prometheus metrics
  audit        — security audit logger
```

Development follows a phased roadmap summarized in [CLAUDE.md](CLAUDE.md).

## Development workflow

1. Fork and create a topic branch off `main`.
2. Make your change with accompanying tests.
3. Ensure the checks below pass.
4. Open a pull request describing the change and its motivation, and tick the
   matching "Type of change" checkbox in the PR template — that's read
   automatically by [pr-labels.yml](.github/workflows/pr-labels.yml) to apply
   the label that groups the PR correctly in the auto-generated release notes
   (see [release-drafter.yml](.github/release-drafter.yml)). Reference any
   related issue. Add the `skip-changelog` label to leave a PR out of release
   notes entirely.

### Checks to run before submitting

```sh
gofmt -l .          # must print nothing (formatting)
go vet ./...        # static analysis
go build ./...      # compiles
go test ./...       # all tests pass
```

## Coding standards

- **Idiomatic, clean Go.** Match the style of the surrounding code; run
  `gofmt`.
- **Tests.** Add or update tests for behavior changes. Most packages have a
  `_test.go` companion; follow the existing table-driven patterns.
- **Comments** explain *why*, not *what*; keep the existing density.
- **Security first.** Never log or embed secrets/tokens; pass credentials via
  headers, not URLs or argv. Validate anything used to build filesystem paths
  or URLs. Preserve the guarantee that no plaintext leaves the machine unencrypted.
- **Dependencies.** Keep the dependency footprint small; discuss new
  dependencies in an issue first.

## Reporting bugs and requesting features

Open a GitHub issue with a clear description, reproduction steps, expected vs.
actual behavior, and your version (from the startup log or
`forgejo_mirror_build_info`). For **security** issues, follow
[SECURITY.md](SECURITY.md) instead — do not open a public issue.

## Code of conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md).

## License

By contributing, you agree that your contributions are licensed under the
project's [MIT License](LICENSE).
