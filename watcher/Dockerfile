FROM golang:1.25-alpine AS builder

WORKDIR /src

COPY watcher/go.mod watcher/go.sum ./watcher/
WORKDIR /src/watcher
RUN go mod download

WORKDIR /src
COPY watcher ./watcher

WORKDIR /src/watcher
RUN CGO_ENABLED=0 GOOS=linux go build -o release-rollout-watcher .

FROM alpine:3.20

RUN apk add --no-cache \
    bash \
    curl \
    python3 \
    git \
    ca-certificates \
    kubectl

WORKDIR /app

COPY --from=builder /src/watcher/release-rollout-watcher /app/watcher/release-rollout-watcher
COPY scripts /app/scripts
COPY schemas /app/schemas
COPY configs /app/configs
COPY watcher/config.k8s.yaml /app/watcher/config.yaml

RUN mkdir -p /app/docs/release-reports && \
    chmod +x /app/watcher/release-rollout-watcher && \
    chmod +x /app/scripts/*.sh

ENTRYPOINT ["/app/watcher/release-rollout-watcher"]
CMD ["--config", "/app/watcher/config.yaml"]
