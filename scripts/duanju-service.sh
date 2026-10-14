#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="${DUANJU_SERVICE_NAME:-duanju}"
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
INSTALL_DIR="${DUANJU_INSTALL_DIR:-$SCRIPT_DIR}"
DATA_DIR="${DUANJU_DATA_DIR:-${INSTALL_DIR}/data}"
ACTION="${1:-help}"
if [[ $# -gt 0 ]]; then shift; fi

case "$ACTION" in
  status) exec systemctl status "${SERVICE_NAME}.service" --no-pager "$@" ;;
  logs) exec journalctl -u "${SERVICE_NAME}.service" -n 100 --no-pager "$@" ;;
  app-logs) exec tail -n 100 "$@" "${DATA_DIR}/logs/app.log" ;;
  start|stop|restart)
    if [[ $# -gt 0 ]]; then echo "启停命令不接受额外参数" >&2; exit 2; fi
    if [[ $EUID -eq 0 ]]; then
      exec systemctl "$ACTION" "${SERVICE_NAME}.service"
    fi
    exec sudo systemctl "$ACTION" "${SERVICE_NAME}.service"
    ;;
  check-update|update)
    if [[ $# -gt 0 ]]; then echo "更新命令不接受额外参数" >&2; exit 2; fi
    command -v python3 >/dev/null || { echo "更新命令需要 Python 3" >&2; exit 1; }
    ;;
  help|-h|--help)
    echo "用法：$0 {status|logs [-f]|app-logs [-f]|start|stop|restart|check-update|update}"
    exit 0
    ;;
  *) echo "未知命令：$ACTION；使用 help 查看用法" >&2; exit 2 ;;
esac

# long: 调用正在运行的服务能共用网页更新锁、校验与回滚，避免另起进程替换程序时互相覆盖备份。
exec python3 - "$ACTION" <<'PY'
import getpass
import http.cookiejar
import json
import os
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


class APIError(Exception):
    pass


class NoRedirect(urllib.request.HTTPRedirectHandler):
    # long: 管理员密码只发送到显式选择的本机服务，拒绝接口重定向带来的凭据转发。
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def main():
    base = os.environ.get("DUANJU_SERVICE_URL", "http://127.0.0.1:3333").rstrip("/")
    address = urllib.parse.urlsplit(base)
    if (address.scheme not in ("http", "https")
            or address.hostname not in ("127.0.0.1", "localhost", "::1")
            or address.username is not None or address.password is not None
            or address.path or address.query or address.fragment):
        raise APIError("DUANJU_SERVICE_URL 必须是本机 HTTP/HTTPS 地址，例如 http://127.0.0.1:3333")
    cookies = http.cookiejar.CookieJar()
    # long: 本机管理接口绕过系统代理，Cookie 只保存在内存中，退出时注销临时会话。
    client = urllib.request.build_opener(
        urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(cookies), NoRedirect())
    viewer_id = ""

    def request(path, payload=None, timeout=30):
        data = None if payload is None else json.dumps(payload).encode("utf-8")
        headers = {"Content-Type": "application/json"}
        if viewer_id:
            headers["X-Duanju-Viewer"] = viewer_id
        req = urllib.request.Request(base + path, data=data, headers=headers)
        try:
            with client.open(req, timeout=timeout) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            try:
                detail = json.load(error).get("error", "请求失败")
            except (ValueError, AttributeError):
                detail = "请求失败"
            raise APIError("HTTP %s：%s" % (error.code, detail)) from None
        except (urllib.error.URLError, TimeoutError, OSError, ValueError):
            raise APIError("无法访问 duanju 服务，请检查服务状态和 DUANJU_SERVICE_URL") from None

    def initialize_viewer():
        nonlocal viewer_id
        # long: 账号接口需要已确认的 Cookie 与身份头；登录后身份会切换，必须重新读取才能继续管理服务。
        viewer = request("/api/ui/viewer")
        if not viewer.get("ready"):
            viewer = request("/api/ui/viewer?confirm=1")
        identifier = viewer.get("id", "")
        if not viewer.get("ready") or not isinstance(identifier, str) or not re.fullmatch(r"[a-f0-9]{64}", identifier):
            raise APIError("无法初始化管理会话，请检查服务日志")
        viewer_id = identifier

    request("/healthz")
    initialize_viewer()
    username = os.environ.get("DUANJU_ADMIN_USER") or "admin"
    password = getpass.getpass("管理员 %s 密码：" % username)
    logged_in = False
    try:
        login = request("/api/ui/account/login", {"username": username, "password": password})
        password = None
        logged_in = True
        initialize_viewer()
        if not login.get("account", {}).get("admin"):
            raise APIError("该账号没有管理员权限")
        info = request("/api/ui/admin/update")
        print("当前版本：%s；最新版本：%s" % (info["currentVersion"], info.get("latestVersion", "未知")), flush=True)
        if not info.get("updateAvailable"):
            print("当前程序已是最新版本，无需更新。")
            return
        if not info.get("updateReady"):
            raise APIError("最新 Release 缺少当前系统的程序包")
        print("可更新程序包：%s" % info["assetName"], flush=True)
        if sys.argv[1] == "check-update":
            return
        print("正在下载、校验并替换程序……", flush=True)
        result = request("/api/ui/admin/update", {}, timeout=150)
        if not result.get("updateApplied"):
            raise APIError(result.get("message", "服务未确认更新完成"))
        print(result.get("message", "程序已更新"), flush=True)
        if not result.get("autoRestart"):
            print("请运行 duanju-service restart 完成重启。")
            return
        # long: 磁盘替换成功不代表新程序已启动，必须等健康接口报告目标版本再宣布更新完成。
        deadline = time.monotonic() + 60
        while time.monotonic() < deadline:
            try:
                health = request("/healthz", timeout=3)
                if health.get("status") == "ok" and health.get("version") == result["latestVersion"]:
                    print("更新完成，服务已运行 %s。" % health["version"])
                    return
            except APIError:
                pass
            time.sleep(1)
        raise APIError("等待新版本启动超时；请用 status、logs 查看启动或回滚结果")
    finally:
        password = None
        if logged_in:
            try:
                request("/api/ui/account/logout", {}, timeout=3)
            except APIError:
                pass


try:
    main()
except (APIError, EOFError) as error:
    print("操作失败：%s" % (error or "未输入管理员密码"), file=sys.stderr)
    sys.exit(1)
except KeyboardInterrupt:
    print("\n操作已中断；若已提交更新，请查看服务状态。", file=sys.stderr)
    sys.exit(130)
PY
