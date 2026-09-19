# syntax=docker/dockerfile:1.27

# Build stage
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

ARG VERSION=dev
ARG TARGETOS
ARG TARGETARCH

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} \
    go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o gitea-mcp

# Final stage
FROM gcr.io/distroless/static-debian12:nonroot

ARG VERSION=dev

WORKDIR /app
COPY --from=builder --chown=nonroot:nonroot /app/gitea-mcp .

USER nonroot:nonroot

LABEL org.opencontainers.image.version="${VERSION}"
LABEL org.opencontainers.image.source="https://gitea.com/gitea/gitea-mcp"

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/app/gitea-mcp", "-healthcheck"]

CMD ["/app/gitea-mcp"]
