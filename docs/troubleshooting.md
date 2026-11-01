# 常见问题

| 现象 | 排查方向 |
| --- | --- |
| 局域网无法访问 | 确认监听地址不是 `127.0.0.1`，放行 TCP 3333，并检查无线客户端隔离。 |
| 播放卡顿或 CPU 占用高 | 查看 `data/logs/app.log` 中的 `playback.plan`、`playback.mode`，检查转码负载和并发设置。 |
| 下载目录不可写 | 检查服务用户、容器 UID/GID 和父目录权限；Emby 共享权限见[目录权限说明](emby-permissions.md)。 |
| FFmpeg 不可用 | 在“系统设置”检查 FFmpeg，或使用 `-ffmpeg` / `DUANJU_FFMPEG` 指定可执行文件。 |
| Emby 没有剧集或海报 | 检查输出目录共享、媒体库扫描、Emby设置中的同步模式和日志。 |
| Emby 无法播放在线分集 | 确认 Emby 能访问配置的短剧服务地址，反向代理没有缓存播放接口。 |
| 任务等待磁盘空间 | 清理目标磁盘空间；空间恢复后任务会自动重试。 |

诊断日志为 JSON Lines，可按事件过滤：

```bash
rg '"event":"(app\\.|playback\\.|download\\.|ffmpeg\\.|media\\.)' data/logs/app.log
```

systemd 部署可同时查看应用日志与服务日志：

```bash
tail -n 100 data/logs/app.log
journalctl -u duanju --since '10 minutes ago'
```
