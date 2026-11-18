#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local

cd "$(dirname "${BASH_SOURCE[0]}")/.."
mkdir -p dist

VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || printf 'unknown')}"
BUILD_TIME="${BUILD_TIME:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
LDFLAGS="-s -w -X duanju/internal/app.Version=${VERSION} -X duanju/internal/app.Commit=${COMMIT} -X duanju/internal/app.BuildTime=${BUILD_TIME}"

echo "Building Linux amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/duanju-${VERSION}-linux-amd64" .

# long: 正式发版只交付 linux-amd64；校验清单也只覆盖本次构建，避免 dist 中的旧文件或旧摘要混入发布包。
(
  cd dist
  shasum -a 256 "duanju-${VERSION}-linux-amd64" > SHA256SUMS
  shasum -a 256 -c SHA256SUMS
)

echo "Done. Files and SHA256SUMS are in dist/."
