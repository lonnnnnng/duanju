#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local
cd "$(dirname "$0")/.."
if command -v go >/dev/null 2>&1; then
  go env -w GOPROXY=https://goproxy.cn,direct
  go env -w GOSUMDB=off
fi
case "$(uname -m)" in
  arm64) pattern="./dist/duanju-*-darwin-arm64" ;;
  x86_64) pattern="./dist/duanju-*-darwin-amd64" ;;
  *) echo "不支持的 macOS 架构，请使用 go run ."; exit 1 ;;
esac
binary=""
for candidate in $pattern; do
  if [ -x "$candidate" ]; then binary="$candidate"; fi
done
if [ ! -x "$binary" ]; then
  echo "找不到可执行程序，请先在项目目录运行 ./scripts/build.sh。"
  exit 1
fi
exec "$binary" "$@"
