#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local

cd "$(dirname "${BASH_SOURCE[0]}")/.."
mkdir -p dist

VERSION="${VERSION:-dev}"
COMMIT="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || printf 'unknown')}"
BUILD_TIME="${BUILD_TIME:-$(date -u '+%Y-%m-%dT%H:%M:%SZ')}"
LDFLAGS="-s -w -X duanju/internal/app.Version=${VERSION} -X duanju/internal/app.Commit=${COMMIT} -X duanju/internal/app.BuildTime=${BUILD_TIME}"

echo "Building macOS arm64..."
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/duanju-${VERSION}-darwin-arm64" .

echo "Building macOS amd64..."
CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/duanju-${VERSION}-darwin-amd64" .

echo "Building Linux amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/duanju-${VERSION}-linux-amd64" .

echo "Building Linux arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/duanju-${VERSION}-linux-arm64" .

echo "Building Windows amd64..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="${LDFLAGS}" -o "dist/duanju-${VERSION}-windows-amd64.exe" .

# long: 校验清单只覆盖本次构建的五个程序，避免 dist 中的旧文件或旧摘要混入发布包。
(
  cd dist
  shasum -a 256 "duanju-${VERSION}-darwin-arm64" "duanju-${VERSION}-darwin-amd64" "duanju-${VERSION}-linux-amd64" "duanju-${VERSION}-linux-arm64" "duanju-${VERSION}-windows-amd64.exe" > SHA256SUMS
  shasum -a 256 -c SHA256SUMS
)

echo "Done. Files and SHA256SUMS are in dist/."
