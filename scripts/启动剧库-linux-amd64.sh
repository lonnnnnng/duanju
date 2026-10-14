#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local
cd "$(dirname "$0")/.."
if command -v go >/dev/null 2>&1; then
  go env -w GOPROXY=https://goproxy.cn,direct
  go env -w GOSUMDB=off
fi
binary=""
for candidate in ./dist/duanju-*-linux-amd64; do
  if [ -x "$candidate" ]; then binary="$candidate"; fi
done
if [ ! -x "$binary" ]; then
  echo "找不到 Linux x64 程序，请先运行 ./scripts/build.sh。"
  exit 1
fi
exec "$binary" "$@"
