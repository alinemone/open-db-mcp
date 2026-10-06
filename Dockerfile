# syntax=docker/dockerfile:1.7

# --- Build stage ---
FROM golang:1.25-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git ca-certificates

# Packages compiled in parallel. Compiling the pure-Go SQLite driver takes
# ~1.5 GB per job, so keep this low on small Docker VMs; raise it for speed.
ARG BUILD_JOBS=2

# The module and build caches persist across builds (BuildKit cache mounts):
# later builds only recompile changed packages — much faster and far less
# memory than a cold build.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -p ${BUILD_JOBS} \
      -ldflags="-w -s" \
      -trimpath \
      -o /open-db-mcp ./cmd/server


# --- Runtime stage ---
# alpine (Docker Hub) is used instead of gcr.io/distroless to avoid GCR
# blockage in some regions. Result image is ~12 MB.
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata wget \
    && addgroup -S app && adduser -S -G app app

COPY --from=builder /open-db-mcp /usr/local/bin/open-db-mcp

USER app
EXPOSE 3000

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget --quiet --tries=1 --spider http://localhost:3000/health || exit 1

ENTRYPOINT ["/usr/local/bin/open-db-mcp"]
