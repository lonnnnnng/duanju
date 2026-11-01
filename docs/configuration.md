# 配置与数据

配置可通过 Web 的“系统设置”、`data/duanju.json`、环境变量和启动参数管理。配置值由内置默认值初始化，再载入保存配置及环境变量；启动参数可覆盖对应运行参数。

## 常用启动参数

| 参数 | 作用 |
| --- | --- |
| `-listen 地址` | Web 监听地址，默认 `0.0.0.0:3333` |
| `-open=false` | 启动后不自动打开浏览器 |
| `-data-dir 目录` | 配置、缓存和任务数据目录，默认 `./data` |
| `-out 目录` | 下载目录，默认 `./downloads` |
| `-ffmpeg 路径` | 指定 FFmpeg 程序路径 |
| `-ui=false -mode list` | 以命令行模式查看剧库 |

## 常用环境变量

| 变量 | 作用 |
| --- | --- |
| `DUANJU_OUTPUT_DIR` | 下载目录 |
| `DUANJU_FFMPEG` | FFmpeg 路径 |
| `DUANJU_PROXY_URL` | HTTP、HTTPS 或 SOCKS5 代理地址 |
| `DUANJU_HONGGUO_URL` | 受支持站源的地址覆盖 |
| `DUANJU_GITHUB_REPO` | 在线更新仓库，格式为 `owner/repository` |
| `DUANJU_GITHUB_TOKEN` | 私有更新仓库所需的只读令牌 |

代理、并发、外观和播放行为也可在“系统设置”中修改。下载目录由部署参数或环境变量管理，页面显示当前生效路径。

## 数据目录

```text
data/
├── duanju.json       # 运行配置
├── library.json      # 剧库缓存
├── ui-state.json     # 下载与合并任务状态
├── viewers/          # 浏览器身份对应的观看记录和追剧清单
├── viewer-key        # 匿名浏览器身份密钥
├── emby-key          # Emby 播放链接签名密钥
└── logs/app.log      # 诊断日志
downloads/            # 视频文件及 Emby 导出目录
```

观看记录、追剧清单和续播状态按浏览器身份保存。更换浏览器或清除其身份 Cookie 后不会自动接续。备份时同时保存数据目录和视频目录，尤其保留 `viewer-key`、`viewers/` 与 `emby-key`。不要将真实数据、凭据或视频提交到 Git。

升级后不再使用旧账号、会话和权限策略；已有旧数据文件不会因此自动删除。Emby 新导出链接使用服务密钥签名，旧签名链接继续有效。
