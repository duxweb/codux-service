# syntax=docker/dockerfile:1

FROM golang:1.23-bookworm AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 go build \
    -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/codux-service \
    ./cmd/codux-service

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --create-home --home-dir /opt/codux-service codux \
    && mkdir -p /data \
    && chown -R codux:codux /data /opt/codux-service

COPY --from=builder /out/codux-service /usr/local/bin/codux-service
COPY deploy/docker.toml /opt/codux-service/config.toml
USER codux
WORKDIR /opt/codux-service
VOLUME ["/data"]
EXPOSE 8088
ENTRYPOINT ["/usr/local/bin/codux-service"]
CMD ["-config", "/opt/codux-service/config.toml"]
