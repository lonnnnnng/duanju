#!/usr/bin/env bash
set -euo pipefail
project_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
if ! command -v docker >/dev/null 2>&1; then
  echo "请先安装 Docker。" >&2
  exit 1
fi
exec docker build --pull -t "${DUANJU_IMAGE:-duanju:local}" --build-arg "GO_IMAGE=${DUANJU_GO_IMAGE:-golang:1.26-bookworm@sha256:a688600ca24f8a4d3ca77f95b0dd40704a9fc787c826660eb7ba0b641b8b175d}" --build-arg "RUNTIME_IMAGE=${DUANJU_RUNTIME_IMAGE:-debian:bookworm-slim@sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251}" "$@" "$project_dir"
