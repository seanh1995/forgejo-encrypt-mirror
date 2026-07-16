# syntax=docker/dockerfile:1

# ---- Build stage -----------------------------------------------------
# --platform=$BUILDPLATFORM pins the builder to the host arch so Go's
# native cross-compilation (GOOS/GOARCH below) is used instead of running
# the whole toolchain under QEMU emulation for the target arch.
# Must match (or exceed) the `go` directive in go.mod (currently 1.26.5),
# otherwise `go mod download`/`go build` fail with "go.mod requires go >= ...".
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS builder

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# TARGETOS/TARGETARCH/TARGETVARIANT are populated automatically by
# buildx for each platform in --platform (see docker/setup-qemu-action +
# docker/build-push-action in .github/workflows/pre-release.yml).
# Cross-compiling via native Go toolchain instead of relying purely on
# QEMU keeps multi-arch builds fast and avoids emulated-build flakiness.
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=dev
RUN set -eux; \
    export GOOS="${TARGETOS}" GOARCH="${TARGETARCH}"; \
    if [ "${TARGETARCH}" = "arm" ] && [ -n "${TARGETVARIANT}" ]; then \
        export GOARM="${TARGETVARIANT#v}"; \
    fi; \
    CGO_ENABLED=0 go build \
        -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/mirror ./cmd/mirror

# ---- Runtime stage -----------------------------------------------------
# alpine (not scratch/distroless) because the app shells out to the git
# CLI (internal/git/engine.go) to mirror/clone/push repositories.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates git curl && \
    addgroup -S mirror && adduser -S mirror -G mirror

WORKDIR /app

COPY --from=builder /out/mirror /app/mirror
COPY configs/config.example.yaml /app/configs/config.example.yaml

# Cache/working directory for git mirrors and encrypted output; also used
# for the encryption rotation-state file. Persist this as a volume in
# production.
RUN mkdir -p /app/cache && chown -R mirror:mirror /app

USER mirror

EXPOSE 8080

# Liveness probe: the process is up and serving HTTP. Readiness
# (dependencies such as the git binary being available) is checked
# separately at /readyz by the orchestrator, if configured to do so.
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/app/mirror"]
