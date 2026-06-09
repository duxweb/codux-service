<h1 align="center">Codux Service</h1>

<p align="center">
  <strong>Codux macOS 与 Codux Mobile 远程终端配对使用的中继服务。</strong>
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
  <a href="README.md">English</a> | 简体中文
</p>

<p align="center">
  <a href="https://github.com/duxweb/codux">Codux macOS</a> &middot;
  <a href="https://github.com/duxweb/codux-flutter">Codux Mobile</a> &middot;
  <a href="https://github.com/duxweb/codux-service/releases">下载发布版</a>
</p>

---

Codux Service 是 Codux macOS 与 Codux Mobile 使用的轻量中继服务。它没有账号系统：Mac 主机注册自身，创建一次性配对二维码，确认或拒绝移动端设备，然后通过中继转发主机和移动端之间的 WebSocket 消息。

## 功能

- **主机注册**：macOS 主机使用稳定的 `hostId` 和 token 注册。
- **一次性配对**：移动端扫码后只能 claim 一次，Mac 端确认或拒绝该请求。
- **设备管理**：确认后的设备获得 token；移除后的设备会断开连接，并从活跃设备列表隐藏。
- **消息中继**：转发终端、文件、项目、统计等 host/client WebSocket 消息。
- **SQLite 存储**：本地嵌入式数据库，启动时自动迁移结构。
- **自动发布**：GitHub Actions 在 `v*` tag 上自动构建 Linux/macOS 二进制和 Docker 镜像。

## 安全模型

Codux Service 是中继服务。当前 Codux macOS/移动端会先对业务 payload 做端到端加密，再通过中继转发。

- 生产环境请使用 **HTTPS/WSS**。TLS 可以保护 macOS/移动端到中继服务之间的网络传输，避免被网络旁路监听。
- 终端输出、终端输入、文件内容、项目列表和 AI 统计会封装为加密的 `secure.message`。中继只能看到 `hostId`、`deviceId`、消息类型、配对状态和在线状态等路由元数据。
- 配对时会交换公钥，并在 macOS/移动端显示匹配码。如果 macOS 主机密钥变化，移动端必须重新配对。
- 端侧会按 host/device 连接缓存派生后的对称密钥，正常业务消息只需要 AES-256-GCM 加解密和 JSON/base64 编解码，不会每条消息都跑 X25519/HKDF。
- 配对 secret 和设备 token 仍会经过中继，因此仍要保护服务器、数据库、日志和 TLS 私钥。
- 恶意中继仍然可以丢弃、延迟或重放流量，但没有端侧密钥就不能解密或伪造有效业务 payload。

推荐方案：使用 HTTPS/WSS，并尽量减少中继日志。若做公开/公益服务器，需要说明内容已端到端加密，但路由元数据对中继可见。

## 从源码运行

```bash
go mod tidy
cp config.example.toml config.toml
go run ./cmd/codux-service
```

服务启动时会自动读取当前目录下的 `config.toml`。也可以显式指定配置文件：

```bash
go run ./cmd/codux-service -config ./config.toml
CODEX_SERVICE_CONFIG=./config.toml go run ./cmd/codux-service
```

配置优先级为：命令行参数 → 环境变量 → TOML 配置 → 内置默认值。运行参数仍可用于临时覆盖：

| 参数 | 环境变量 | TOML | 默认值 | 说明 |
|:--|:--|:--|:--|:--|
| `-config` | `CODEX_SERVICE_CONFIG` | — | `config.toml` | TOML 配置文件路径。默认 `config.toml` 不存在时会忽略；显式指定的路径必须存在。 |
| `-addr` | `CODEX_SERVER_ADDR` | `server.addr` | `:8088` | HTTP/WebSocket 监听地址。`127.0.0.1:8088` 仅本机监听，`:8088` 监听所有网卡。 |
| `-db` | `CODEX_SERVER_DB` | `database.path` | `codux-service.sqlite3` | SQLite 数据库路径。 |
| `-stats` | `CODEX_STATS_ENABLED` | `stats.enabled` | `true` | 启用旁路 JSONL 中继统计，不参与协议状态。 |
| `-stats-path` | `CODEX_STATS_PATH` | `stats.path` | `codux-service.stats.jsonl` | 中继统计 JSONL 文件路径。 |
| `-stats-flush-interval` | `CODEX_STATS_FLUSH_INTERVAL` | `stats.flush_interval_seconds` | `10` | 统计快照写入间隔，单位秒。 |
| `-pairing-ttl` | `CODEX_PAIRING_TTL` | `pairing.ttl_seconds` | `300` | 配对二维码有效期，单位秒。 |
| `-shutdown-timeout` | `CODEX_SHUTDOWN_TIMEOUT` | `shutdown.timeout_seconds` | `3` | 优雅关闭超时时间，超过后强制退出，单位秒。 |
| `-read-header-timeout` | `CODEX_READ_HEADER_TIMEOUT` | `server.read_header_timeout_seconds` | `10` | HTTP 请求头读取超时，单位秒。 |

TOML 示例：

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

## 二进制部署

从 [GitHub Releases](https://github.com/duxweb/codux-service/releases) 下载服务器对应系统的压缩包，解压后运行：

```bash
mkdir -p /opt/codux-service/data
tar -xzf codux-service-v0.1.0-linux-amd64.tar.gz
sudo install -m 0755 codux-service-v0.1.0-linux-amd64/codux-service /usr/local/bin/codux-service
sudo install -m 0644 codux-service-v0.1.0-linux-amd64/config.toml /opt/codux-service/config.toml

codux-service -config /opt/codux-service/config.toml
```

防火墙放行对应端口后，在 Codux macOS 设置 → 远程 中填写：

```text
https://<你的域名>
```

局域网测试且没有 TLS 时可以填写：

```text
http://<服务器 IP>:8088
```

## Docker 部署

```bash
docker run -d \
  --name codux-service \
  --restart unless-stopped \
  -p 8088:8088 \
  -v codux-service-data:/data \
  ghcr.io/duxweb/codux-service:latest
```

也可以使用 Compose：

```bash
docker compose up -d
```

镜像默认读取 `/opt/codux-service/config.toml`，并把 SQLite 数据存储在 `/data/codux-service.sqlite3`。Compose 会挂载 `deploy/docker.toml`，让运行配置显式可见。

## 反向代理

如果服务放在 Nginx/Caddy 后面，需要确保 `/ws/host` 和 `/ws/client` 支持 WebSocket Upgrade。

Nginx 示例：

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

## systemd 示例

仓库已提供可直接复制的 unit 文件：`deploy/codux-service.service`。

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

| 方法 | 路径 | 说明 |
|:--|:--|:--|
| `GET` | `/healthz` | 健康检查。 |
| `POST` | `/api/hosts/register` | 注册或刷新 Mac 主机。 |
| `POST` | `/api/pairings` | 创建配对二维码内容。 |
| `POST` | `/api/pairings/claim` | 移动端 claim 一个未使用的配对二维码。 |
| `POST` | `/api/pairings/status` | 移动端轮询配对状态。 |
| `POST` | `/api/pairings/confirm` | Mac 端确认配对设备。 |
| `POST` | `/api/pairings/reject` | Mac 端拒绝配对设备。 |
| `GET` | `/api/hosts/{hostID}/devices?token=...` | 获取主机的活跃设备列表。 |
| `POST` | `/api/devices/revoke` | 移除并断开设备。 |
| `GET` | `/ws/host?hostId=...&token=...` | 主机 WebSocket。 |
| `GET` | `/ws/client?deviceId=...&token=...` | 移动端 WebSocket。 |

## 开发

```bash
go test ./...
go build ./cmd/codux-service
./scripts/release/build.sh dev
docker build -t codux-service:dev .
```

由于服务使用 `github.com/mattn/go-sqlite3`，发布构建需要 CGO。当前 GitHub Actions 使用原生 Linux/macOS runner 构建，不使用 `CGO_ENABLED=0` 交叉编译。

## 发布

仓库包含两个 workflow：

- `.github/workflows/test-build.yml`：运行测试，并验证二进制/Docker 构建。
- `.github/workflows/release-build.yml`：在 `v*` tag 上构建发布包，创建 GitHub Release，并推送 Docker 镜像到 GHCR。

发布版本：

```bash
git tag v0.1.0
git push origin main
git push origin v0.1.0
```

发布产物包括：

- `codux-service-<version>-linux-amd64.tar.gz`
- `codux-service-<version>-darwin-arm64.tar.gz`
- `*.sha256`
- `SHA256SUMS.txt`
- Docker 镜像 `ghcr.io/duxweb/codux-service:<version>` 和 `latest`

## 目录结构

| 路径 | 说明 |
|:--|:--|
| `cmd/codux-service/` | 服务入口。 |
| `internal/server/` | HTTP API、WebSocket 路由、配对流程和消息转发。 |
| `internal/store/` | SQLite 数据结构和持久化。 |
| `internal/crypto/` | token 生成工具。 |
| `deploy/` | systemd 等部署示例。 |
| `.github/workflows/` | 测试和发布自动化。 |
| `scripts/release/` | 本地和 CI 发布构建脚本。 |

## 开源协议

Codux Service 使用 GNU General Public License v3.0，与 Codux macOS 和 Codux Mobile 保持一致。详见 `LICENSE`。
