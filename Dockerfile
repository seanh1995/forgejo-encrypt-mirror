# syntax=docker/dockerfile:1

# ---- Build stage -----------------------------------------------------
FROM golang:1.23-alpine AS builder

WORKDIR /src

# Cache module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
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
