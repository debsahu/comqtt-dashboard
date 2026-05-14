# syntax=docker/dockerfile:1.7

# ----- builder ----------------------------------------------------------------
# Pinned to the Go version in go.mod (currently 1.26 after the comqttauth v0.2.0
# bump). Alpine keeps the build image small; CGO_ENABLED=0 below makes the
# produced binaries copyable into distroless.
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Module cache layer: copy only go.mod/go.sum first so docker can reuse the
# downloaded modules layer when source files change but deps don't.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

# Source. .dockerignore filters out tests-only files, the Makefile-built
# tailwind binary, IDE configs, and local data/log directories.
COPY . .

# Build both binaries (single + cluster). CGO disabled so the binaries are
# fully static and can run on distroless/static. Buildvcs=false avoids
# warnings about VCS info in environments without git metadata.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
        -ldflags="-s -w" -o /out/comqtt-dashboard ./cmd/comqtt-dashboard && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -buildvcs=false \
        -ldflags="-s -w" -o /out/comqtt-cluster-dashboard ./cmd/comqtt-cluster-dashboard

# ----- runtime ----------------------------------------------------------------
# alpine (not distroless): the cluster-mode Helm chart's entrypoint script
# uses /bin/sh + sed + awk + mkdir + cat for runtime config templating, so
# distroless/static would force us to rewrite that as a Go binary or use a
# debug variant. Alpine is ~5MB and gives us a clean POSIX environment with
# all four tools out of the box.
FROM alpine:3.20

LABEL org.opencontainers.image.source="https://github.com/debsahu/comqtt-dashboard"
LABEL org.opencontainers.image.description="comqtt MQTT broker with the dashboard add-on (single + cluster mode binaries)"
LABEL org.opencontainers.image.licenses="MIT"

# Run as a fixed non-root uid that matches the Helm chart's
# podSecurityContext.runAsUser (1000). The /data mount is chowned at startup
# by Kubernetes via fsGroup; for direct `docker run` users we leave /data
# writable to the user.
RUN addgroup -g 1000 -S comqtt && \
    adduser -u 1000 -S -G comqtt -h /home/comqtt comqtt && \
    mkdir -p /data && chown -R comqtt:comqtt /data

COPY --from=builder /out/comqtt-dashboard /comqtt-dashboard
COPY --from=builder /out/comqtt-cluster-dashboard /comqtt-cluster-dashboard

# Ports (informational; K8s manifests publish their own):
#   1883 — MQTT TCP
#   1882 — MQTT WebSocket
#   2000 — MQTT QUIC (single-mode only)
#   8080 — REST API + dashboard
#   7946 — Gossip (cluster)
#   8946 — Raft (cluster)
#   17946 — gRPC (cluster, when enabled)
EXPOSE 1883 1882 2000 8080 7946 8946 17946

USER comqtt

# Default to single-mode. K8s users override via `command:` to invoke
# /comqtt-cluster-dashboard or /bin/sh entrypoint scripts.
ENTRYPOINT ["/comqtt-dashboard"]
