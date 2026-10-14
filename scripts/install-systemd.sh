#!/usr/bin/env bash
set -euo pipefail

# long: 安装脚本只生成可审计的服务文件并提示管理员执行 sudo，避免无提示修改系统服务。
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SERVICE_NAME="${DUANJU_SERVICE_NAME:-duanju}"
INSTALL_DIR="${DUANJU_INSTALL_DIR:-$HOME/duanju}"
SERVICE_FILE="${ROOT_DIR}/deploy/systemd/${SERVICE_NAME}.service"
TARGET_FILE="/tmp/${SERVICE_NAME}.service"

if [[ ! -f "$SERVICE_FILE" ]]; then
  echo "找不到服务模板：$SERVICE_FILE" >&2
  exit 1
fi
if [[ -e "$TARGET_FILE" ]]; then
  echo "目标文件已存在，避免覆盖：$TARGET_FILE" >&2
  exit 1
fi

sed -e "s#^User=.*#User=${DUANJU_USER:-$(id -un)}#" \
    -e "s#^WorkingDirectory=.*#WorkingDirectory=${INSTALL_DIR}#" \
    -e "s#^EnvironmentFile=.*#EnvironmentFile=-${INSTALL_DIR}/.env#" \
    -e "s#^ExecStart=.*#ExecStart=${INSTALL_DIR}/duanju -open=false -listen ${DUANJU_LISTEN:-0.0.0.0:3333} -data-dir ${DUANJU_DATA_DIR:-${INSTALL_DIR}/data} -out ${DUANJU_OUTPUT_DIR:-${INSTALL_DIR}/短剧下载}#" \
    -e "s#^ReadWritePaths=.*#ReadWritePaths=${INSTALL_DIR}#" \
    "$SERVICE_FILE" > "$TARGET_FILE"

echo "已生成：$TARGET_FILE"
echo "请确认路径和用户后执行："
echo "  sudo install -m 0644 $TARGET_FILE /etc/systemd/system/${SERVICE_NAME}.service"
echo "  sudo systemctl daemon-reload"
echo "  sudo systemctl enable --now ${SERVICE_NAME}"
echo "  sudo systemctl status ${SERVICE_NAME}"
