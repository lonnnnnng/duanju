# 部署与运行

## 本地运行

从 [GitHub Releases](https://github.com/lonnnnnng/duanju/releases) 下载对应平台程序。默认监听 `0.0.0.0:3333`，本机访问 `http://127.0.0.1:3333`，局域网访问运行主机的 IP 和端口。

```bash
chmod +x ./duanju-vX.Y.Z-linux-amd64
./duanju-vX.Y.Z-linux-amd64 -open=false
```

只允许本机访问可使用 `-listen 127.0.0.1:3333`。程序运行用户需要读写数据目录和下载目录。

## Linux systemd

安装器面向 Linux systemd，会下载最新 Release、校验 SHA-256、安装服务并等待健康检查：

```bash
curl -fsSL https://raw.githubusercontent.com/lonnnnnng/duanju/main/scripts/install.sh | bash
```

也可以先下载脚本检查后再运行：

```bash
curl -fsSL https://raw.githubusercontent.com/lonnnnnng/duanju/main/scripts/install.sh -o install-duanju.sh
bash install-duanju.sh
```

可通过 `DUANJU_VERSION` 固定安装版本，或通过 `DUANJU_INSTALL_DIR`、`DUANJU_DATA_DIR`、`DUANJU_OUTPUT_DIR` 和 `DUANJU_LISTEN` 修改安装、数据、下载目录及监听地址。安装器使用 sudo 安装程序和 systemd 服务；服务以指定的普通用户运行。

安装后可使用管理脚本：

```bash
~/duanju/duanju-service status
~/duanju/duanju-service logs -f
~/duanju/duanju-service app-logs -f
~/duanju/duanju-service start
~/duanju/duanju-service stop
~/duanju/duanju-service restart
~/duanju/duanju-service check-update
~/duanju/duanju-service update
```

启停操作调用 systemd；检查和更新需要 Python 3。管理脚本默认访问 `http://127.0.0.1:3333`，自定义端口时设置 `DUANJU_SERVICE_URL`。

## Docker Compose

需要 Docker Engine 和 Compose v2：

```bash
./scripts/docker-up.sh
docker compose logs --tail=100 app
```

默认使用端口 `3333`，并持久化 `data` 和 `downloads` 卷。`.env` 可设置 `DUANJU_PORT`、`DUANJU_BIND_ADDRESS` 和 `DUANJU_PROXY_URL`。宿主目录映射时，容器用户 UID/GID 1000 需要有读写权限。源码构建上下文应使用仓库根目录。

## 反向代理

建议使用独立域名和 HTTPS，代理到 `http://127.0.0.1:3333`。透传 `Host`、`Cookie`、`Set-Cookie` 和 `X-Forwarded-Proto`，关闭 API 与媒体缓存，并为媒体请求设置足够长的读写超时。Emby 使用在线分集同步时，也必须能访问配置的短剧服务地址。

## 日志与健康检查

- 应用日志：`data/logs/app.log`，单文件上限 5 MiB，并保留轮转日志。
- systemd 日志：`journalctl -u duanju -f`。
- 健康接口：`GET /healthz`，返回状态、版本和提交信息。

```bash
tail -n 100 data/logs/app.log
journalctl -u duanju --since '10 minutes ago'
```

诊断日志记录启动、网络、播放、下载、FFmpeg、合并和更新事件，并隐藏密码、令牌及签名 URL。
