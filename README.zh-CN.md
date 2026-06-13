# Codux Service 发布仓库

当前仓库保留为 Codux Service 的公开发布和 Docker 镜像发布壳。

服务端源码现在维护在 Codux monorepo：

- 源码：`https://github.com/duxweb/codux/tree/main/apps/server`
- 服务端公开发布：`https://github.com/duxweb/codux-service/releases`
- Docker 镜像：`ghcr.io/duxweb/codux-service`

## 发布模型

当前仓库的 workflows 不再构建本仓库内的旧源码。它们会 checkout `duxweb/codux` 的同名 tag，并从 monorepo 的 `apps/server` 构建服务端。

发布服务端版本：

1. 在 `duxweb/codux` 提交服务端源码变更。
2. 给 monorepo 打 tag 并推送：

```bash
cd /Volumes/Web/codux-gpui
git tag v1.8.0
git push origin main
git push origin v1.8.0
```

3. 在当前发布仓推送同名 tag：

```bash
cd /Volumes/Web/codex-server
git tag v1.8.0
git push origin v1.8.0
```

当前仓库的 tag 只作为发布触发器。workflows 会构建 `duxweb/codux@v1.8.0`。

## Workflows

- `.github/workflows/release-build.yml`：从 `duxweb/codux/apps/server` 构建 Rust `codux-server` 发布包，发布到当前仓库 GitHub Release，并推送 `ghcr.io/duxweb/codux-service`。
- `.github/workflows/test-build.yml`：从 `duxweb/codux/apps/server` 执行手动测试构建。通过 `source_ref` 指定 monorepo 的 branch、tag 或 commit SHA。

## 更新日志

`CHANGELOG.md` 保留在当前仓库，用于公开服务端发布页。源码和部署说明维护在 `duxweb/codux/apps/server`。

## 开源协议

Codux Service 使用 GNU General Public License v3.0。详情见 `LICENSE`。
