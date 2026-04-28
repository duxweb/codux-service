<h1 align="center">Codux Service</h1>

<p align="center">
  <strong>Codux macOS 与 Codux Mobile 远程终端配对使用的中继服务。</strong>
</p>

<p align="center">
  <a href="https://github.com/duxweb/codux-service/releases">
    <img src="https://img.shields.io/badge/version-1.0.0-22d3ee?style=flat-square" alt="Version">
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

Codux Service 是中继服务，不是端到端加密传输层。

- 生产环境请使用 **HTTPS/WSS**。TLS 可以保护 macOS/移动端到中继服务之间的网络传输，避免被网络旁路监听。
- 当前服务端会终止 TLS，并在服务进程内以明文 JSON/WebSocket 消息转发。服务端运营者理论上可以看到终端输出、终端输入、文件内容、项目元数据、设备名称和配对状态。
- 配对 secret 和设备 token 属于当前 host/relay 流程，服务端会参与生成/存储，因此公益服务器必须被视为可信基础设施。
- 如果你提供公开/公益中继服务，需要明确告知用户这个信任边界，并保护服务器、数据库、日志和 TLS 私钥。
- 真正的零信任公益服务器需要额外端到端加密：密钥只由 Codux macOS 和 Codux Mobile 生成和保存，服务端只转发密文。当前版本还没有实现这层能力。

推荐方案：自建中继，或者使用你信任的服务提供方。若做公益服务器，建议尽量减少日志，并公开说明以上安全边界。

## 从源码运行

```bash
go mod tidy
go run ./cmd/codux-service
```

监听端口可以自定义，下面两种方式等价：

```bash
go run ./cmd/codux-service -addr :8088
CODEX_SERVER_ADDR=:8088 go run ./cmd/codux-service
```

运行参数：

| 参数 | 环境变量 | 默认值 | 说明 |
|:--|:--|:--|:--|
| `-addr` | `CODEX_SERVER_ADDR` | `:8088` | HTTP/WebSocket 监听地址。`127.0.0.1:8088` 仅本机监听，`:8088` 监听所有网卡。 |
| `-db` | `CODEX_SERVER_DB` | `codux-service.sqlite3` | SQLite 数据库路径。 |
| `-pairing-ttl` | `CODEX_PAIRING_TTL` | `300` | 配对二维码有效期，单位秒。 |

## 二进制部署

从 [GitHub Releases](https://github.com/duxweb/codux-service/releases) 下载服务器对应系统的压缩包，解压后运行：

```bash
mkdir -p /opt/codux-service/data
tar -xzf codux-service-v1.0.0-linux-amd64.tar.gz
sudo install -m 0755 codux-service-v1.0.0-linux-amd64/codux-service /usr/local/bin/codux-service

codux-service \
  -addr :8088 \
  -db /opt/codux-service/data/codux-service.sqlite3 \
  -pairing-ttl 300
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

镜像默认把 SQLite 数据存储在 `/data/codux-service.sqlite3`。

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
git tag v1.0.0
git push origin main
git push origin v1.0.0
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
