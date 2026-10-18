#!/usr/bin/env bash
set -Eeuo pipefail

# long: 安装器只面向 Linux systemd，Release 程序下载后先做 SHA-256 校验。
REPOSITORY="${DUANJU_REPOSITORY:-lonnnnnng/duanju}"
VERSION="${DUANJU_VERSION:-}"
SERVICE_NAME="${DUANJU_SERVICE_NAME:-duanju}"
SERVICE_USER="${DUANJU_USER:-${SUDO_USER:-$(id -un)}}"
LISTEN_ADDRESS="${DUANJU_LISTEN:-0.0.0.0:3333}"

if [[ "$(uname -s)" != "Linux" ]]; then
  echo "安装失败：此安装器只支持 Linux。" >&2
  exit 1
fi

if [[ -n "${DUANJU_INSTALL_DIR:-}" ]]; then
  INSTALL_DIR="$DUANJU_INSTALL_DIR"
else
  TARGET_HOME="$(getent passwd "$SERVICE_USER" | cut -d: -f6 || true)"
  TARGET_HOME="${TARGET_HOME:-$HOME}"
  INSTALL_DIR="${TARGET_HOME}/duanju"
fi
DATA_DIR="${DUANJU_DATA_DIR:-${INSTALL_DIR}/data}"
OUTPUT_DIR="${DUANJU_OUTPUT_DIR:-${INSTALL_DIR}/downloads}"
TMP_DIR=""
SERVICE_WAS_ACTIVE=0
OLD_BINARY_BACKUP=""

die() { echo "安装失败：$*" >&2; exit 1; }

cleanup() {
  local status=$?
  if [[ "$status" -ne 0 && -n "$OLD_BINARY_BACKUP" && -f "$OLD_BINARY_BACKUP" ]]; then
    sudo install -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0755 "$OLD_BINARY_BACKUP" "$INSTALL_DIR/duanju" >/dev/null 2>&1 || true
  fi
  if [[ "$status" -ne 0 && "$SERVICE_WAS_ACTIVE" -eq 1 ]]; then
    sudo systemctl start "${SERVICE_NAME}.service" >/dev/null 2>&1 || true
  fi
  [[ -z "$TMP_DIR" ]] || rm -rf "$TMP_DIR"
  exit "$status"
}
trap cleanup EXIT

require_command() { command -v "$1" >/dev/null 2>&1 || die "缺少命令 $1，请先安装它后重试。"; }

ensure_directory() {
  local directory="$1"
  if sudo test -e "$directory"; then
    sudo test -d "$directory" || die "路径已存在但不是目录：$directory"
    return
  fi
  # long: 升级时保留媒体目录的共享组、ACL 与 setgid，避免覆盖 Emby 等服务已有的访问权限。
  sudo install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0750 "$directory"
}

[[ "$(ps -p 1 -o comm= 2>/dev/null | tr -d ' ')" == "systemd" ]] || die "当前系统不是 systemd，不能安装后台服务。"
id "$SERVICE_USER" >/dev/null 2>&1 || die "服务用户不存在：$SERVICE_USER"
SERVICE_GROUP="$(id -gn "$SERVICE_USER")"
[[ "$INSTALL_DIR" = /* && "$DATA_DIR" = /* && "$OUTPUT_DIR" = /* ]] || die "目录路径必须是绝对路径。"
[[ "$INSTALL_DIR" != *[[:space:]]* && "$DATA_DIR" != *[[:space:]]* && "$OUTPUT_DIR" != *[[:space:]]* ]] || die "目录路径不能包含空格。"
[[ "$REPOSITORY" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || die "DUANJU_REPOSITORY 格式必须为 owner/repository。"
[[ "$SERVICE_NAME" =~ ^[A-Za-z0-9_.@-]+$ ]] || die "DUANJU_SERVICE_NAME 含有无效字符。"
[[ "$LISTEN_ADDRESS" != *[[:space:]]* ]] || die "DUANJU_LISTEN 不能包含空格。"
LISTEN_PORT="${LISTEN_ADDRESS##*:}"
[[ "$LISTEN_PORT" =~ ^[0-9]+$ ]] && ((LISTEN_PORT > 0 && LISTEN_PORT < 65536)) || die "DUANJU_LISTEN 必须以有效端口结尾。"

require_command curl
require_command python3
require_command systemctl
require_command sudo
if command -v sha256sum >/dev/null 2>&1; then
  CHECKSUM_COMMAND="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  CHECKSUM_COMMAND="shasum -a 256"
else
  die "缺少 sha256sum 或 shasum，无法校验下载程序。"
fi
[[ "$(id -u)" -ne 0 || -n "${DUANJU_USER:-}" || -n "${SUDO_USER:-}" ]] || die "请使用普通服务用户运行，或显式设置 DUANJU_USER。"

case "$(uname -m)" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) die "暂不支持当前 CPU 架构：$(uname -m)" ;;
esac

if [[ -n "$VERSION" ]]; then
  [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "DUANJU_VERSION 必须类似 v0.1.10。"
  RELEASE_ENDPOINT="https://api.github.com/repos/${REPOSITORY}/releases/tags/${VERSION}"
else
  RELEASE_ENDPOINT="https://api.github.com/repos/${REPOSITORY}/releases/latest"
fi

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/duanju-install.XXXXXX")"
RELEASE_JSON="$TMP_DIR/release.json"
CURL=(curl --fail --silent --show-error --location --retry 3 --connect-timeout 10 --max-time 180)
echo "正在读取 ${REPOSITORY} 的 Release 信息……"
"${CURL[@]}" "$RELEASE_ENDPOINT" -o "$RELEASE_JSON" || die "无法读取 GitHub Release，请检查网络。"

RELEASE_FIELDS="$TMP_DIR/release-fields"
python3 - "$RELEASE_JSON" "$ARCH" > "$RELEASE_FIELDS" <<'PY' || die "Release 信息不完整或没有匹配当前架构的安装包。"
import json
import re
import sys
with open(sys.argv[1], encoding="utf-8") as source:
    release = json.load(source)
arch = sys.argv[2]
tag = release.get("tag_name", "")
if not re.fullmatch(r"v\d+\.\d+\.\d+", tag):
    raise SystemExit("Release 版本号无效")
asset_name = f"duanju-{tag}-linux-{arch}"
assets = {item.get("name"): item.get("browser_download_url") for item in release.get("assets", [])}
asset_url = assets.get(asset_name)
checksum_url = assets.get("SHA256SUMS")
if not asset_url or not checksum_url:
    raise SystemExit(f"Release {tag} 缺少 {asset_name} 或 SHA256SUMS")
print(tag, asset_name, asset_url, checksum_url)
PY
IFS=' ' read -r RELEASE_TAG ASSET_NAME ASSET_URL CHECKSUM_URL < "$RELEASE_FIELDS"
[[ -n "$RELEASE_TAG" && -n "$ASSET_NAME" && -n "$ASSET_URL" && -n "$CHECKSUM_URL" ]] || die "Release 信息解析失败。"

echo "将安装 ${RELEASE_TAG}（Linux ${ARCH}）到 ${INSTALL_DIR}"
echo "正在下载并校验程序……"
"${CURL[@]}" "$ASSET_URL" -o "$TMP_DIR/$ASSET_NAME"
"${CURL[@]}" "$CHECKSUM_URL" -o "$TMP_DIR/SHA256SUMS"
EXPECTED_CHECKSUM="$(awk -v name="$ASSET_NAME" '$2 == name { print; found=1 } END { if (!found) exit 1 }' "$TMP_DIR/SHA256SUMS")" || die "SHA256SUMS 中没有找到 $ASSET_NAME。"
printf '%s\n' "$EXPECTED_CHECKSUM" | (cd "$TMP_DIR" && $CHECKSUM_COMMAND -c -) || die "程序校验失败，已停止安装。"

HELPER_URL="https://raw.githubusercontent.com/${REPOSITORY}/main/scripts/duanju-service.sh"
"${CURL[@]}" "$HELPER_URL" -o "$TMP_DIR/duanju-service" || die "无法下载 duanju-service 管理脚本。"
bash -n "$TMP_DIR/duanju-service" || die "管理脚本语法检查失败。"

sudo -v || die "无法取得 sudo 权限。"
if sudo systemctl is-active --quiet "${SERVICE_NAME}.service"; then
  SERVICE_WAS_ACTIVE=1
  echo "正在停止已有 ${SERVICE_NAME} 服务……"
  sudo systemctl stop "${SERVICE_NAME}.service"
fi

# long: 先写入临时程序再原子替换；已有 data 和 downloads 的内容及权限均保持原样。
ensure_directory "$INSTALL_DIR"
ensure_directory "$DATA_DIR"
ensure_directory "$OUTPUT_DIR"
if sudo test -f "$INSTALL_DIR/duanju"; then
  OLD_BINARY_BACKUP="$TMP_DIR/duanju.before-install"
  sudo cp -p "$INSTALL_DIR/duanju" "$OLD_BINARY_BACKUP"
fi
sudo install -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0755 "$TMP_DIR/$ASSET_NAME" "$INSTALL_DIR/duanju.new"
sudo mv -f "$INSTALL_DIR/duanju.new" "$INSTALL_DIR/duanju"
sudo install -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0755 "$TMP_DIR/duanju-service" "$INSTALL_DIR/duanju-service"

SERVICE_FILE="$TMP_DIR/${SERVICE_NAME}.service"
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=短剧
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
WorkingDirectory=${INSTALL_DIR}
EnvironmentFile=-${INSTALL_DIR}/.env
Environment=DUANJU_AUTO_RESTART=1
ExecStart=${INSTALL_DIR}/duanju -open=false -listen ${LISTEN_ADDRESS} -data-dir ${DATA_DIR} -out ${OUTPUT_DIR}
Restart=on-failure
RestartSec=5s
KillSignal=SIGTERM
TimeoutStopSec=45s
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=full
ProtectHome=read-only
ReadWritePaths=${INSTALL_DIR} ${DATA_DIR} ${OUTPUT_DIR}

[Install]
WantedBy=multi-user.target
EOF
sudo install -o root -g root -m 0644 "$SERVICE_FILE" "/etc/systemd/system/${SERVICE_NAME}.service"
sudo systemctl daemon-reload
sudo systemctl enable --now "${SERVICE_NAME}.service"

echo "正在等待服务健康检查……"
HEALTH_OK=0
for _ in $(seq 1 30); do
  if curl --fail --silent --max-time 3 "http://127.0.0.1:${LISTEN_PORT}/healthz" > "$TMP_DIR/health.json"; then
    if python3 - "$TMP_DIR/health.json" "$RELEASE_TAG" <<'PY'
import json
import sys
with open(sys.argv[1], encoding="utf-8") as source:
    health = json.load(source)
if health.get("status") != "ok" or health.get("version") != sys.argv[2]:
    raise SystemExit(1)
PY
    then
      HEALTH_OK=1
      break
    fi
  fi
  sleep 1
done
[[ "$HEALTH_OK" -eq 1 ]] || die "服务未能通过健康检查，请执行 ${INSTALL_DIR}/duanju-service logs 查看日志。"

SERVICE_WAS_ACTIVE=0
OLD_BINARY_BACKUP=""
echo
echo "安装完成：${RELEASE_TAG}"
echo "访问地址：http://127.0.0.1:3333"
echo "安装目录：${INSTALL_DIR}"
echo "下载目录：${OUTPUT_DIR}"
echo "管理命令：${INSTALL_DIR}/duanju-service status|logs|start|stop|restart|update"
