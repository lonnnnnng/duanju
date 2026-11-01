# 短剧

短剧是一个 Go 应用，提供剧库浏览、在线播放、追剧、观看记录、下载与 Emby 同步。Web 资源嵌入程序中；运行已发布版本不需要 Node.js 或 Python。需要解密、转码或下载时使用 FFmpeg。

## 快速开始

从 [GitHub Releases](https://github.com/lonnnnnng/duanju/releases) 下载适合当前系统和 CPU 架构的程序。macOS / Linux 首次运行可能需要执行 `chmod +x 程序名`。

默认监听 `0.0.0.0:3333`，浏览器打开 `http://127.0.0.1:3333` 即可使用。只允许本机访问时加上 `-listen 127.0.0.1:3333`；后台运行时可加 `-open=false`。

从源码运行需要 Go 1.21 或更新版本：

```bash
go run .
```

## 文档

- [部署与运行](docs/deployment.md)：Linux systemd、Docker、反向代理和日志。
- [配置与数据](docs/configuration.md)：启动参数、环境变量、数据目录和备份。
- [播放、下载与合并](docs/playback-downloads.md)：播放选项、任务行为、自动合并和磁盘空间检查。
- [Emby 同步](docs/emby.md)：分集与全集同步模式、扫描通知和权限。
- [常见问题](docs/troubleshooting.md)：网络、播放、下载和同步排查。
- [Android 应用](docs/android-app.md)：Android 客户端构建与使用说明。
- [发布流程](docs/releasing.md)：手动触发 Release 工作流。

## 开发检查

```bash
go test ./...
go vet ./...
git diff --check
```
