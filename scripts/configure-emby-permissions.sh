#!/usr/bin/env bash
set -Eeuo pipefail

# long: 让 duanju 与 Emby 通过共享组访问同一份导出媒体，避免把下载根目录放开为 777。
DUANJU_USER="${DUANJU_USER:-${SUDO_USER:-$(id -un)}}"
EMBY_USER="${EMBY_USER:-emby}"
MEDIA_GROUP="${DUANJU_MEDIA_GROUP:-media}"
DUANJU_INSTALL_DIR="${DUANJU_INSTALL_DIR:-$(getent passwd "$DUANJU_USER" | cut -d: -f6)/duanju}"
DUANJU_OUTPUT_DIR="${DUANJU_OUTPUT_DIR:-${DUANJU_INSTALL_DIR}/downloads}"
EMBY_DIR="${DUANJU_EMBY_DIR:-${DUANJU_OUTPUT_DIR}/Emby}"
DUANJU_SERVICE_NAME="${DUANJU_SERVICE_NAME:-duanju}"
EMBY_SERVICE_NAME="${EMBY_SERVICE_NAME:-emby-server}"
RESTART_SERVICES=1

die() {
  echo "配置失败：$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
用法：configure-emby-permissions.sh [--no-restart]

通过 media 组和父目录 ACL，让 duanju 与 Emby 共享指定的 Emby 导出目录。
可用环境变量：
  DUANJU_USER             duanju systemd 运行用户，默认当前用户
  EMBY_USER               Emby 运行用户，默认 emby
  DUANJU_MEDIA_GROUP      共享组，默认 media
  DUANJU_INSTALL_DIR      duanju 安装目录，默认目标用户 home 下的 duanju
  DUANJU_OUTPUT_DIR       下载根目录，默认 <安装目录>/downloads
  DUANJU_EMBY_DIR         Emby 目录，默认 <下载根目录>/Emby
  DUANJU_SERVICE_NAME     duanju systemd 服务名，默认 duanju
  EMBY_SERVICE_NAME       Emby systemd 服务名，默认 emby-server
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --no-restart) RESTART_SERVICES=0 ;;
    -h|--help) usage; exit 0 ;;
    *) die "未知参数：$1" ;;
  esac
  shift
done

[[ "$(uname -s)" == "Linux" ]] || die "此脚本只支持 Linux。"
command -v sudo >/dev/null 2>&1 || die "缺少 sudo。"
command -v setfacl >/dev/null 2>&1 || die "缺少 setfacl，请先安装 acl 工具包。"
command -v getfacl >/dev/null 2>&1 || die "缺少 getfacl，请先安装 acl 工具包。"
command -v systemctl >/dev/null 2>&1 || die "缺少 systemctl。"
command -v realpath >/dev/null 2>&1 || die "缺少 realpath。"

id "$DUANJU_USER" >/dev/null 2>&1 || die "duanju 用户不存在：$DUANJU_USER"
id "$EMBY_USER" >/dev/null 2>&1 || die "Emby 用户不存在：$EMBY_USER"
[[ "$DUANJU_OUTPUT_DIR" = /* && "$EMBY_DIR" = /* ]] || die "目录路径必须使用绝对路径。"
[[ "$DUANJU_OUTPUT_DIR" != *[[:space:]]* && "$EMBY_DIR" != *[[:space:]]* ]] || die "目录路径不能包含空格。"
[[ "$DUANJU_OUTPUT_DIR" == "$(realpath -m -- "$DUANJU_OUTPUT_DIR")" ]] || die "下载目录必须是规范绝对路径。"
[[ "$EMBY_DIR" == "$DUANJU_OUTPUT_DIR/Emby" ]] || die "Emby 目录必须是下载目录下的 Emby。"
[[ ! -L "$DUANJU_OUTPUT_DIR" && ! -L "$EMBY_DIR" ]] || die "下载目录和 Emby 目录不能是符号链接。"

if ! getent group "$MEDIA_GROUP" >/dev/null 2>&1; then
  echo "创建共享组：$MEDIA_GROUP"
  sudo groupadd -- "$MEDIA_GROUP"
fi

add_to_group() {
  local user="$1"
  if id -nG "$user" | tr ' ' '\n' | grep -Fxq "$MEDIA_GROUP"; then
    return
  fi
  echo "将 $user 加入共享组：$MEDIA_GROUP"
  sudo usermod -aG "$MEDIA_GROUP" "$user"
}

add_to_group "$DUANJU_USER"
add_to_group "$EMBY_USER"

ensure_directory() {
  local directory="$1"
  local mode="$2"
  if sudo test -e "$directory"; then
    sudo test -d "$directory" || die "路径已存在但不是目录：$directory"
    return
  fi
  sudo install -d -o "$DUANJU_USER" -g "$MEDIA_GROUP" -m "$mode" "$directory"
}

ensure_directory "$DUANJU_OUTPUT_DIR" 2770
ensure_directory "$EMBY_DIR" 2770
[[ ! -L "$DUANJU_OUTPUT_DIR" && ! -L "$EMBY_DIR" ]] || die "目录被替换为符号链接，已停止操作。"

# long: 只给 Emby 穿过父目录的能力；不授予它读取 data、配置或密钥的权限。
grant_parent_traverse() {
  local ancestor
  ancestor="$(dirname "$1")"
  while [[ "$ancestor" != "/" ]]; do
    if ! sudo -u "$EMBY_USER" test -x "$ancestor" 2>/dev/null; then
      sudo setfacl -m "u:${EMBY_USER}:--x" "$ancestor"
    fi
    ancestor="$(dirname "$ancestor")"
  done
}
grant_parent_traverse "$DUANJU_OUTPUT_DIR"

# long: Emby 目录只归 duanju 管理，Emby 通过 media 组读写；setgid 让新文件继续继承共享组。
sudo chown -R "$DUANJU_USER:$MEDIA_GROUP" "$EMBY_DIR"
sudo find "$EMBY_DIR" -type d -exec chmod 2770 {} +
sudo find "$EMBY_DIR" -type f -exec chmod 0660 {} +

if [[ "$RESTART_SERVICES" -eq 1 ]]; then
  if sudo systemctl is-active --quiet "${EMBY_SERVICE_NAME}.service"; then
    echo "重启 Emby 服务以加载新的补充组……"
    sudo systemctl restart "${EMBY_SERVICE_NAME}.service"
  fi
  if sudo systemctl is-active --quiet "${DUANJU_SERVICE_NAME}.service"; then
    echo "重启 duanju 服务以加载新的补充组……"
    sudo systemctl restart "${DUANJU_SERVICE_NAME}.service"
  fi
else
  echo "已跳过服务重启；重新登录用户会话并重启两个服务后，补充组才会对进程生效。"
fi

sudo -u "$EMBY_USER" test -r "$EMBY_DIR" || die "Emby 无法读取目录：$EMBY_DIR"
sudo -u "$EMBY_USER" test -w "$EMBY_DIR" || die "Emby 无法写入目录：$EMBY_DIR"
sudo -u "$DUANJU_USER" test -w "$EMBY_DIR" || die "duanju 无法写入目录：$EMBY_DIR"
sudo -u "$EMBY_USER" sh -c 'set -eu; target="$1/.duanju-permissions-check.$$"; trap '\''rm -f "$target"'\'' EXIT; umask 007; : > "$target"' sh "$EMBY_DIR"

echo
echo "Emby 目录权限配置完成：$EMBY_DIR"
stat -c '  %A %a %U:%G %n' "$DUANJU_OUTPUT_DIR" "$EMBY_DIR"
echo "  Emby 用户已通过实际临时文件创建验证读写权限。"
echo "  若进程刚刚重启，检查：systemctl status ${EMBY_SERVICE_NAME} ${DUANJU_SERVICE_NAME}"
echo "  撤销父目录 ACL：sudo setfacl -x u:${EMBY_USER} <父目录>"
