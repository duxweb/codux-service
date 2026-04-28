#!/usr/bin/env bash
set -euo pipefail

version="${1:-dev}"
os_name="$(go env GOOS)"
arch_name="$(go env GOARCH)"
out_dir="dist"
binary="codux-service"

mkdir -p "${out_dir}"
if [[ "${os_name}" == "windows" ]]; then
  binary="${binary}.exe"
fi

artifact="codux-service-${version}-${os_name}-${arch_name}"
output="${out_dir}/${artifact}/${binary}"
mkdir -p "$(dirname "${output}")"
cp deploy/config.toml "${out_dir}/${artifact}/config.toml"

CGO_ENABLED=1 go build \
  -trimpath \
  -ldflags "-s -w -X main.version=${version}" \
  -o "${output}" \
  ./cmd/codux-service

(
  cd "${out_dir}"
  tar -czf "${artifact}.tar.gz" "${artifact}"
  shasum -a 256 "${artifact}.tar.gz" > "${artifact}.tar.gz.sha256"
)
