FROM golang:1.25-alpine AS builder

WORKDIR /build

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-w -s -X main.version=$(git rev-parse --short HEAD 2>/dev/null || echo dev)" \
      -trimpath \
      -o /open-db-mcp ./cmd/server

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /open-db-mcp /open-db-mcp
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt

EXPOSE 3000
USER nonroot:nonroot

ENTRYPOINT ["/open-db-mcp"]
