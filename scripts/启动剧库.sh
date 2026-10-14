#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local
cd "$(dirname "$0")/.."
if command -v go >/dev/null 2>&1; then
  go env -w GOPROXY=https://goproxy.cn,direct
  go env -w GOSUMDB=off
fi
case "$(uname -m)" in
  x86_64) pattern="./dist/duanju-*-linux-amd64"; label="Linux x64" ;;
  aarch64|arm64) pattern="./dist/duanju-*-linux-arm64"; label="Linux ARM64" ;;
  *) echo "不支持的 Linux 架构，请使用 go run ."; exit 1 ;;
esac
binary=""
for candidate in $pattern; do
  if [ -x "$candidate" ]; then
    binary="$candidate"
  fi
done
if [ ! -x "$binary" ]; then
  echo "找不到 $label 程序，请先运行 ./scripts/build.sh。"
  exit 1
fi
exec "$binary" "$@"
