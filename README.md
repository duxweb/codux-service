# Codux Service Release Repository

This repository is kept as the public release and Docker publishing shell for Codux Service.

The server source code now lives in the Codux monorepo:

- Source: `https://github.com/duxweb/codux/tree/main/apps/server`
- Public service releases: `https://github.com/duxweb/codux-service/releases`
- Docker image: `ghcr.io/duxweb/codux-service`

## Release Model

The workflows in this repository do not build checked-in source from this repository. They checkout the matching tag from `duxweb/codux` and build `apps/server` from the monorepo.

For a service release:

1. Commit the server source changes in `duxweb/codux`.
2. Tag and push the monorepo:

```bash
cd /Volumes/Web/codux-gpui
git tag v1.8.0
git push origin main
git push origin v1.8.0
```

3. Push the same tag in this release repository:

```bash
cd /Volumes/Web/codex-server
git tag v1.8.0
git push origin v1.8.0
```

The tag in this repository is only the release trigger. The workflows build `duxweb/codux@v1.8.0`.

## Workflows

- `.github/workflows/release-build.yml`: builds Rust `codux-server` archives from `duxweb/codux/apps/server`, publishes this repository's GitHub Release, and pushes `ghcr.io/duxweb/codux-service`.
- `.github/workflows/test-build.yml`: manual test builds from `duxweb/codux/apps/server`. Use `source_ref` to choose the monorepo branch, tag, or commit SHA.

## Changelog

`CHANGELOG.md` is kept here for the public service release page. Source and deployment docs live under `duxweb/codux/apps/server`.

## License

Codux Service is licensed under the GNU General Public License v3.0. See `LICENSE` for details.
