<h1 align="center">Codux Service</h1>

<p align="center">
  <strong>Relay service for Codux macOS and Codux Mobile remote terminal pairing.</strong>
</p>

<p align="center">
  <a href="https://github.com/duxweb/codux-service/releases">
    <img src="https://img.shields.io/badge/version-0.1.0-22d3ee?style=flat-square" alt="Version">
  </a>
  <a href="LICENSE">
    <img src="https://img.shields.io/badge/license-GPLv3-blue?style=flat-square" alt="License">
  </a>
  <img src="https://img.shields.io/badge/go-1.23-00add8?style=flat-square" alt="Go">
  <img src="https://img.shields.io/badge/docker-GHCR-2496ed?style=flat-square" alt="Docker">
  <img src="https://img.shields.io/badge/database-SQLite-044a64?style=flat-square" alt="SQLite">
</p>

<p align="center">
  English | <a href="README.zh-CN.md">简体中文</a>
</p>

<p align="center">
  <a href="https://github.com/duxweb/codux">Codux for macOS</a> &middot;
  <a href="https://github.com/duxweb/codux-flutter">Codux Mobile</a> &middot;
  <a href="https://github.com/duxweb/codux-service/releases">Releases</a>
</p>

---

Codux Service is the lightweight relay used by Codux macOS and Codux Mobile. It has no account system: Mac hosts register themselves, create one-time pairing QR codes, approve or reject mobile devices, and then exchange host/client WebSocket messages through the relay.

## Features

- **Host registration** — macOS hosts register with a stable `hostId` and token.
- **One-time pairing** — mobile clients claim QR payloads once; hosts confirm or reject the pending request.
- **Device management** — confirmed devices receive tokens, revoked devices are disconnected and hidden from active lists.
- **Relay forwarding** — host/client WebSocket peers exchange terminal, file, project, and stats messages.
- **SQLite storage** — embedded local database with automatic schema migration.
- **Release automation** — GitHub Actions builds Linux/macOS binaries and Docker images on `v*` tags.

## Security Model

Codux Service is a relay. Current Codux macOS/mobile clients encrypt business payloads end-to-end before sending them through the relay.

- Use **HTTPS/WSS** in production. TLS protects traffic between macOS/mobile clients and the relay from network observers.
- Terminal output, terminal input, file payloads, project lists, and AI stats are wrapped as encrypted `secure.message` payloads. The relay only sees routing metadata such as `hostId`, `deviceId`, message type, pairing status, and online state.
- Pairing exchanges public keys and shows a matching code on macOS/mobile. If the macOS host key changes, mobile devices must pair again.
- Endpoints cache the derived symmetric key per host/device connection, so normal traffic only pays AES-256-GCM plus JSON/base64 overhead instead of running X25519/HKDF for every message.
- Pairing secrets and device tokens still pass through the relay, so protect the server, database, logs, and TLS private keys.
- A malicious relay can still drop, delay, or replay traffic, but it cannot decrypt or forge valid encrypted payloads without the endpoint keys.

Recommended deployment: use HTTPS/WSS and keep relay logs minimal. For public/community servers, disclose that content is end-to-end encrypted but relay metadata is visible.

## Run From Source

```bash
go mod tidy
cp config.example.toml config.toml
go run ./cmd/codux-service
```

The service automatically loads `config.toml` from the current directory when it exists. You can also pass a specific file:

```bash
go run ./cmd/codux-service -config ./config.toml
CODEX_SERVICE_CONFIG=./config.toml go run ./cmd/codux-service
```

Configuration priority is: command-line flags → environment variables → TOML file → built-in defaults. Runtime options remain available for quick overrides:

| Flag | Environment | TOML | Default | Description |
|:--|:--|:--|:--|:--|
| `-config` | `CODEX_SERVICE_CONFIG` | — | `config.toml` | TOML config file path. Missing default `config.toml` is ignored; explicit paths must exist. |
| `-addr` | `CODEX_SERVER_ADDR` | `server.addr` | `:8088` | HTTP/WebSocket listen address. Use `127.0.0.1:8088` for local-only binding or `:8088` for all interfaces. |
| `-db` | `CODEX_SERVER_DB` | `database.path` | `codux-service.sqlite3` | SQLite database path. |
| `-stats` | `CODEX_STATS_ENABLED` | `stats.enabled` | `true` | Enable side-channel JSONL relay statistics. This does not participate in protocol state. |
| `-stats-path` | `CODEX_STATS_PATH` | `stats.path` | `codux-service.stats.jsonl` | Relay statistics JSONL path. |
| `-stats-flush-interval` | `CODEX_STATS_FLUSH_INTERVAL` | `stats.flush_interval_seconds` | `10` | Statistics snapshot interval, in seconds. |
| `-pairing-ttl` | `CODEX_PAIRING_TTL` | `pairing.ttl_seconds` | `300` | Pairing QR lifetime in seconds. |
| `-shutdown-timeout` | `CODEX_SHUTDOWN_TIMEOUT` | `shutdown.timeout_seconds` | `3` | Graceful shutdown timeout before force exit, in seconds. |
| `-read-header-timeout` | `CODEX_READ_HEADER_TIMEOUT` | `server.read_header_timeout_seconds` | `10` | HTTP read-header timeout, in seconds. |

Example TOML:

```toml
[server]
addr = ":8088"
read_header_timeout_seconds = 10

[database]
path = "/opt/codux-service/data/codux-service.sqlite3"

[stats]
enabled = true
path = "/opt/codux-service/data/codux-service.stats.jsonl"
flush_interval_seconds = 10

[pairing]
ttl_seconds = 300

[shutdown]
timeout_seconds = 3
```

## Deploy A Binary

Download the archive for your server from [GitHub Releases](https://github.com/duxweb/codux-service/releases), extract it, then run:

```bash
mkdir -p /opt/codux-service/data
tar -xzf codux-service-v0.1.0-linux-amd64.tar.gz
sudo install -m 0755 codux-service-v0.1.0-linux-amd64/codux-service /usr/local/bin/codux-service
sudo install -m 0644 codux-service-v0.1.0-linux-amd64/config.toml /opt/codux-service/config.toml

codux-service -config /opt/codux-service/config.toml
```

Open the chosen port in your firewall and point Codux macOS Settings → Remote to:

```text
https://<server-domain>
```

For local LAN testing without TLS you can use:

```text
http://<server-ip>:8088
```

## Deploy With Docker

```bash
docker run -d \
  --name codux-service \
  --restart unless-stopped \
  -p 8088:8088 \
  -v codux-service-data:/data \
  ghcr.io/duxweb/codux-service:latest
```

Or use Compose:

```bash
docker compose up -d
```

The image loads `/opt/codux-service/config.toml` by default and stores SQLite data at `/data/codux-service.sqlite3`. Compose mounts `deploy/docker.toml` to make the runtime configuration explicit.

## Reverse Proxy

If the service is behind Nginx/Caddy, forward WebSocket upgrade headers for `/ws/host` and `/ws/client`.

Example Nginx location:

```nginx
location / {
    proxy_pass http://127.0.0.1:8088;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

## systemd Example

A ready-to-copy unit is provided at `deploy/codux-service.service`.

```bash
sudo useradd --system --create-home --home-dir /opt/codux-service codux
sudo mkdir -p /opt/codux-service/data
sudo chown -R codux:codux /opt/codux-service
sudo cp deploy/codux-service.service /etc/systemd/system/codux-service.service
sudo systemctl daemon-reload
sudo systemctl enable --now codux-service
sudo systemctl status codux-service
```

## API

| Method | Path | Description |
|:--|:--|:--|
| `GET` | `/healthz` | Health check. |
| `POST` | `/api/hosts/register` | Register or refresh a Mac host. |
| `POST` | `/api/pairings` | Create a pairing QR payload. |
| `POST` | `/api/pairings/claim` | Claim an unused pairing QR from mobile. |
| `POST` | `/api/pairings/status` | Poll pairing status from mobile. |
| `POST` | `/api/pairings/confirm` | Confirm a claimed device from macOS. |
| `POST` | `/api/pairings/reject` | Reject a claimed device from macOS. |
| `GET` | `/api/hosts/{hostID}/devices?token=...` | List active devices for a host. |
| `POST` | `/api/devices/revoke` | Revoke and disconnect a device. |
| `GET` | `/ws/host?hostId=...&token=...` | Host WebSocket. |
| `GET` | `/ws/client?deviceId=...&token=...` | Mobile client WebSocket. |

## Development

```bash
go test ./...
go build ./cmd/codux-service
./scripts/release/build.sh dev
docker build -t codux-service:dev .
```

Because the service uses `github.com/mattn/go-sqlite3`, release builds require CGO. The included GitHub Actions build on native Linux/macOS runners instead of using `CGO_ENABLED=0` cross-compilation.

## Release

This repository includes two workflows:

- `.github/workflows/test-build.yml` — runs tests and produces manual binary/Docker build checks.
- `.github/workflows/release-build.yml` — builds release archives on `v*` tags, publishes a GitHub Release, and pushes Docker images to GHCR.

Publish a version:

```bash
git tag v0.1.0
git push origin main
git push origin v0.1.0
```

The release workflow uploads:

- `codux-service-<version>-linux-amd64.tar.gz`
- `codux-service-<version>-darwin-arm64.tar.gz`
- `*.sha256`
- `SHA256SUMS.txt`
- Docker image `ghcr.io/duxweb/codux-service:<version>` and `latest`

## Repository Layout

| Path | Description |
|:--|:--|
| `cmd/codux-service/` | Service entrypoint. |
| `internal/server/` | HTTP API, WebSocket routing, pairing flow, and peer forwarding. |
| `internal/store/` | SQLite schema and persistence layer. |
| `internal/crypto/` | Token generation helpers. |
| `deploy/` | Deployment examples such as systemd unit files. |
| `.github/workflows/` | Test and release build automation. |
| `scripts/release/` | Local and CI release build helpers. |

## License

Codux Service is licensed under the GNU General Public License v3.0, the same license used by Codux for macOS and Codux Mobile. See `LICENSE` for details.
