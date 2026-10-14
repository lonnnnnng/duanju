#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="${DUANJU_SERVICE_NAME:-duanju}"
echo "请确认要移除 systemd 服务 ${SERVICE_NAME}。执行以下命令："
echo "  sudo systemctl disable --now ${SERVICE_NAME}"
echo "  sudo rm /etc/systemd/system/${SERVICE_NAME}.service"
echo "  sudo systemctl daemon-reload"
