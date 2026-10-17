# 短剧

短剧是一个单体 Go 应用，提供剧库浏览、在线播放、观看记录、追剧、下载、合并和 Emby 导出。网页资源已嵌入可执行文件，发布后的程序不需要 Node.js 或 Python；需要解密、封装、转码或下载时使用 FFmpeg。

## 快速启动

### 使用发布程序

`dist/` 中的程序按版本、系统和架构命名：

| 系统 | 程序 |
| --- | --- |
| macOS Apple Silicon | `duanju-vX.Y.Z-darwin-arm64` |
| macOS Intel | `duanju-vX.Y.Z-darwin-amd64` |
| Linux x64 | `duanju-vX.Y.Z-linux-amd64` |
| Linux ARM64 | `duanju-vX.Y.Z-linux-arm64` |
| Windows x64 | `duanju-vX.Y.Z-windows-amd64.exe` |

macOS / Linux 首次运行可能需要 `chmod +x 程序名`。默认监听 `0.0.0.0:3333`，本机访问 `http://127.0.0.1:3333`，局域网设备访问运行主机的 IP 和端口。服务器或后台运行建议关闭自动打开浏览器：

```bash
./duanju-vX.Y.Z-linux-amd64 -open=false
```

只允许本机访问时使用 `-listen 127.0.0.1:3333`。程序、`data/` 和下载目录必须对运行用户可写。

### 从源码运行

需要 Go 1.21+：

```bash
go run .
```

常用参数：

| 参数 | 作用 |
| --- | --- |
| `-listen 地址` | 修改监听地址，默认 `0.0.0.0:3333` |
| `-open=false` | 不自动打开浏览器 |
| `-data-dir 目录` | 配置、缓存、账号和任务目录，默认 `./data` |
| `-out 目录` | 下载目录，默认 `./downloads` |
| `-ffmpeg 路径` | 指定 FFmpeg，默认从 PATH 查找 |
| `-admin-user` / `-admin-password` | 设置管理员凭据 |
| `-ui=false -mode list` | 使用命令行模式查看剧库 |

## 账号与权限

首次启动创建 `admin` 管理员。未指定密码时，随机密码只在启动输出一次，并要求首次登录改密；也可以通过启动参数或环境变量指定：

```bash
DUANJU_ADMIN_USER=admin DUANJU_ADMIN_PASSWORD='替换为强密码' ./duanju-vX.Y.Z-linux-amd64 -open=false
```

`DUANJU_ADMIN_PASSWORD` 或 `-admin-password` 会在每次启动时重新应用。网页修改密码后，应移除旧环境变量或同步更新服务环境文件。

管理员在“更多 → 用户管理”中控制：

- 是否必须登录、是否允许注册；
- 普通账号可用的站源；
- 是否仅允许在线观看；仅在线观看会同时隐藏并拒绝下载、合并和 Emby 导出接口。

观看记录、收藏、追剧和续播按账号保存；匿名记录按浏览器 Cookie 隔离。账号、会话、密钥和记录都在 `data/` 中，备份时不要遗漏。

## Linux 后台运行、日志与更新

### 一键安装（Linux systemd）

在 Linux 服务器上执行以下命令，会自动识别 CPU 架构，下载最新 Release，校验 SHA-256，安装 systemd 服务和管理脚本，并等待健康检查通过：

```bash
curl -fsSL https://raw.githubusercontent.com/lonnnnnng/duanju/main/scripts/install.sh | bash
```

安装器默认使用当前用户、`~/duanju` 和 `~/duanju/downloads`。也可以先下载后执行，便于审阅脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/lonnnnnng/duanju/main/scripts/install.sh -o install-duanju.sh
bash install-duanju.sh
```

可选环境变量：`DUANJU_VERSION=v0.1.10` 固定版本，`DUANJU_INSTALL_DIR` 修改安装目录，`DUANJU_OUTPUT_DIR` 修改下载目录，`DUANJU_LISTEN` 修改监听地址。安装完成后首次登录密码可从 `sudo journalctl -u duanju -n 30 --no-pager` 查看；登录后建议立即修改密码。

### systemd

把可执行文件命名为 `duanju`，与 `data/`、下载目录放在安装目录，然后生成服务文件：

```bash
bash scripts/install-systemd.sh
sudo install -m 0644 /tmp/duanju.service /etc/systemd/system/duanju.service
sudo systemctl daemon-reload
sudo systemctl enable --now duanju
sudo systemctl status duanju
install -m 0755 scripts/duanju-service.sh "$HOME/duanju/duanju-service"
```

脚本不会自动执行 `sudo`。可用环境变量生成其他路径：`DUANJU_USER`、`DUANJU_INSTALL_DIR`、`DUANJU_DATA_DIR`、`DUANJU_OUTPUT_DIR`、`DUANJU_LISTEN`。服务模板使用 `Restart=on-failure`、优雅停止和 `DUANJU_AUTO_RESTART=1`。

安装管理脚本后，可直接在服务器运行：

```bash
~/duanju/duanju-service status        # 状态
~/duanju/duanju-service logs -f       # 跟踪 systemd 日志，Ctrl+C 退出
~/duanju/duanju-service app-logs -f   # 跟踪应用诊断日志
~/duanju/duanju-service start         # 启动
~/duanju/duanju-service stop          # 停止
~/duanju/duanju-service restart       # 重启
~/duanju/duanju-service check-update  # 检查新版本
~/duanju/duanju-service update        # 在线更新并验证重启结果
```

启停和重启会调用 `sudo systemctl`；检查和更新需 Python 3，会交互输入管理员密码。默认访问 `http://127.0.0.1:3333`，自定义端口设置 `DUANJU_SERVICE_URL`；更新时服务须处于运行状态。脚本也支持 `DUANJU_SERVICE_NAME`、`DUANJU_INSTALL_DIR`、`DUANJU_DATA_DIR` 和 `DUANJU_ADMIN_USER`。

### 日志与健康检查

- 应用诊断日志：`data/logs/app.log`；单文件 5 MiB，保留 2 个轮转文件，权限为 `0600`。
- systemd 日志：`journalctl -u duanju -f`。
- 健康检查：`GET /healthz`，返回 `status`、`version` 和 `commit`。

```bash
tail -n 100 data/logs/app.log
sudo systemctl status duanju
journalctl -u duanju --since '10 minutes ago'
```

日志记录启动、配置、网络检查、播放、下载、FFmpeg、合并和更新事件，并会隐藏密码、令牌及签名 URL。

### 在线更新

管理员在“更多 → 代理 / 设置 → 应用版本”检查 GitHub 正式 Release。更新器：

1. 按当前 `GOOS/GOARCH` 查找 `duanju-版本号-系统-架构` 资产；Windows 资产带 `.exe`。
2. 下载程序和 `SHA256SUMS`，校验 SHA-256。
3. 备份当前程序后原子替换，并写入启动标记。
4. systemd 自动重启；新程序成功监听后删除标记和备份，启动失败时下一次启动自动恢复旧程序。

正式发版通过 [手动发版流水线](docs/releasing.md) 完成五个平台构建、校验和 Release 发布。仅在明确要求发版后手动触发，普通推送和标签推送均不触发。

公开仓库在线更新不需要令牌；私有仓库才设置只读 `DUANJU_GITHUB_TOKEN`，建议写入服务用户可读的 `.env` 并设置 `chmod 600`。手动 `nohup`、源码运行和 Docker 不会自动替你重启服务；Windows 运行中的程序不能原位替换。

## 剧库、播放与下载

### 剧库与播放

剧库支持本地筛选、联网搜索、榜单、分类推荐、收藏、想看 / 在看 / 已看和历史记录。详情页会异步补齐剧名、简介、集数、封面、日期、热度等资料，失败不阻塞播放。

在线播放支持：

- 分集、画质、倍速、全屏、自动下一集和续播；
- 桌面键盘操作，手机上下滑动切集、横竖屏播放和方向感应；
- 原始 MP4 / HLS 播放（默认关闭），兼容内容可跳过 FFmpeg；
- 解密、换封装、音频转码和视频转码的独立并发限制；
- Emby、本地已下载文件和在线缓冲的统一播放入口。

“优先原始 MP4 / HLS 播放”及客户端直连都在管理员设置中控制。直连只是条件允许时的尝试，失败会回退到服务端中转；开启直连不会绕过账号权限。

### 下载与合并

下载画质支持“最高可用”或指定档位，任务会保存选择并在重启后继续。下载完成后检查视频流、尺寸和时长；默认保留源码流，不会凭空生成源站没有的分辨率。

下载页支持暂停、继续、取消、重试、优先级、补新集和文件校验。合并任务在后台执行，刷新或关闭网页不会中断；兼容分集优先复制或无损换封装，必要时只处理不兼容分集。合并成功后可播放本地全集，也可以选择在校验成功后清理分集文件。

后台下载、合并和入库复制会检查目标磁盘，默认预留 **2 GiB** 安全余量；已知下载大小、合并分集总大小和复制文件大小另计预留。FFmpeg 与复制期间继续检查。空间不足显示“等待磁盘空间”，下载不占名额、不消耗尝试次数，每 30 秒重查；合并每 5 秒重试，入库复制下一分钟补查。空间恢复后自动继续，原分集和旧入库视频保留；等待中的合并在重启后自动恢复，并保留合并后清理分集的选择。未完成的下载会重新解析源地址并重下当前分集。

默认下载目录为 `downloads/`，可在设置页面、`-out` 或 `DUANJU_OUTPUT_DIR` 修改。已存在任务继续使用保存时的目录。

## 配置与数据

配置优先级：内置默认值 < `data/duanju.json` < `DUANJU_*` 环境变量 < 启动参数。常用环境变量：

| 变量 | 作用 |
| --- | --- |
| `DUANJU_ADMIN_USER` / `DUANJU_ADMIN_PASSWORD` | 管理员凭据 |
| `DUANJU_OUTPUT_DIR` | 下载目录 |
| `DUANJU_FFMPEG` | FFmpeg 路径 |
| `DUANJU_PROXY_URL` | HTTP、HTTPS 或 SOCKS5 代理 |
| `DUANJU_HONGGUO_URL` | 受支持站源的地址覆盖 |
| `DUANJU_GITHUB_REPO` | 在线更新仓库，格式为 `owner/repository` |
| `DUANJU_GITHUB_TOKEN` | 私有更新仓库的只读令牌 |

主要数据文件：

```text
data/
├── duanju.json                 # 代理、并发和下载目录
├── accounts.json             # 账号、权限和会话
├── library.json              # 剧库缓存
├── ui-state.json             # 下载与合并状态
├── viewers/                  # 观看记录和追剧清单
├── viewer-key                # 匿名身份密钥
├── emby-key                  # Emby 链接签名密钥
└── logs/app.log              # 诊断日志
downloads/                    # 视频文件
```

备份时同时保存 `data/` 和视频目录，尤其是 `accounts.json`、`viewer-key`、`viewers/` 与 `emby-key`。不要把真实数据、密码、令牌或视频提交到 Git。

## Docker

需要 Docker Engine 和 Compose v2：

```bash
./scripts/docker-up.sh
docker compose logs --tail=100 app
```

`compose.yaml` 默认使用 `duanju:local`，端口为 `3333`，数据卷为 `data` 和 `downloads`。可在 `.env` 设置：

```dotenv
DUANJU_PORT=3333
DUANJU_BIND_ADDRESS=0.0.0.0
DUANJU_ADMIN_USER=admin
DUANJU_ADMIN_PASSWORD=替换为强密码
DUANJU_PROXY_URL=
```

升级执行同一条 `./scripts/docker-up.sh`，Compose 会重新构建并保留卷。宿主目录映射时，容器内固定使用 `/data` 和 `/downloads`，运行用户 UID/GID 1000 需要有读写权限。1Panel 或其他面板从源码构建时，构建上下文必须是完整仓库根目录。

单独构建可用：

```bash
./scripts/docker-build.sh
```

## 反向代理与 Emby

反向代理建议使用独立域名和 HTTPS，转发到 `http://127.0.0.1:3333`，透传 `Host`、Cookie、Set-Cookie 和 `X-Forwarded-Proto`，并关闭 API / 媒体缓存；媒体接口建议设置较长的读写超时。

Emby 自动同步在“更多 → Emby 同步”中配置。请把输出目录加入 Emby 的“电视节目”媒体库，并让两项服务通过宿主目录或共享卷访问同一份入库文件。

**已合并全集直接入库：** 勾选“只同步已合并的全集”，程序会将合并成功的本地视频发布为 `Season 01/S01E001.mp4`，生成对应 NFO，并将海报保存为 `poster.jpg` 或 `poster.png`。Emby 直接读取这些文件，停止 duanju 后仍能播放和扫描；无需填写剧库播放地址。本模式不导出未完成的合并结果。

- 后台合并成功并保存记录后立即触发自动同步，无需点击“立即同步”；选择合并后清理分集也会保留这条链路。配置 Emby 地址和 API Key 后，文件发布完成会自动通知扫描；通知失败、连续合并取消旧轮次或发布后重启时，会继续补扫。同步间隔用于定期补查和重试，不影响合并完成时的即时触发。
- 同步面板分别显示“文件已发布”和“Emby 已识别”。全集模式配置 Emby 地址和 API Key 后，后台查询分集条目，只有路径匹配且媒体源协议为 `File` 的 MP4 才确认；HTTP STRM、重复条目和不完整查询会提示异常。扫描延迟自动等待，超过 10 分钟仍未发现文件会提示检查目录和权限，“立即同步”可补扫。已识别条目每 5 分钟复查；确认表示 Emby 已识别文件，实际播放权限和编码兼容性由 Emby 决定。
- 同一文件系统且源文件与入库目录属组一致、允许共享组读取时，优先硬链接，避免复制整部视频；跨磁盘、不支持硬链接或源文件权限不适合共享时，完整复制到隐藏临时目录后再发布。复制可能需要额外磁盘空间，原下载文件的权限保持不变。
- 从旧版本升级后，下一轮同步会在 MP4 完整发布后迁移本程序生成的 HTTP STRM，保持剧集目录、集号和 NFO 中的剧集身份。文件名或内容被用户修改的入口、同名用户视频不会被覆盖；旧条目的观看进度是否保留需在实际 Emby 中核对。
- 重新合并会更新稳定的入库路径；删除、移动原全集或切回分集模式后，下轮同步撤下校验匹配的入库副本与元数据，原下载文件和自定义海报会保留。单独从 Emby 删除入库副本不会删除原下载文件，仍符合范围的内容可能再次同步入库。
- 海报下载失败不会撤掉已发布的 MP4，错误和重试状态会保存并在重启后继续处理。已有自定义海报保留；程序发布的海报可随资料更新。本地全集的观看权限由 Emby 的账号和媒体库权限管理。

**在线分集与手动 ZIP 导出：** 按追剧或下载记录同步分集、以及手动导出 ZIP，仍使用 HTTP STRM / NFO，播放需要 duanju 在线。填写 Emby 能访问的剧库地址，不要使用容器内的 `localhost`；已下载分集优先使用本地文件，其余分集可在线播放。

两种自动同步模式都可以选填 Emby 地址及 API Key，以通知扫描并重试失败请求。全集的本地海报无需连接 Emby；在线分集的海报由 Emby 获取和缓存。未连接时可使用 Emby 的实时监控或手动扫描。

宿主机部署中，如果 duanju 和 Emby 使用不同 Linux 用户，请执行 [`scripts/configure-emby-permissions.sh`](scripts/configure-emby-permissions.sh) 配置共享组、父目录 ACL 和 setgid 权限；完整说明见 [`docs/emby-permissions.md`](docs/emby-permissions.md)。不要直接对整个下载目录执行 `chmod -R 777`。

手动导出 ZIP 仍可在剧集详情中使用。关闭自动同步只停止后续更新，不删除已有导出文件。

## 常见问题

| 现象 | 排查 |
| --- | --- |
| 局域网无法访问 | 确认监听不是 `127.0.0.1`，放行 TCP 3333，并确认客户端未启用无线隔离 |
| 登录后反复跳回 | 保持访问地址不变，反向代理透传 Cookie / Set-Cookie，并使用 HTTPS |
| 播放卡顿或 CPU 高 | 先查看日志中的 `playback.plan`、`playback.mode`，限制播放处理并发；免转码中转仍消耗服务器带宽 |
| 下载目录不可写 | 检查服务用户、容器 UID/GID 以及父目录权限；不要用无条件 `chmod -R 777` |
| FFmpeg 不可用 | 在设置中重新检查，或设置 `-ffmpeg` / `DUANJU_FFMPEG` 指向支持 H.264/AAC 的版本 |
| Emby 没有剧集或海报 | 检查输出目录共享、媒体库扫描、剧库地址和 API Key；查看同步面板与应用日志 |
| 在线更新失败 | 检查 GitHub Release 是否包含精确资产名和 `SHA256SUMS`，再查看 `app.update_*` 日志事件 |

日志事件是 JSON Lines，可按事件筛选：

```bash
rg '"event":"(app\\.|playback\\.|download\\.|ffmpeg\\.|media\\.)' data/logs/app.log
```

## 开发检查

```bash
go test ./...
go vet ./...
git diff --check
```

前端资源通过 `go:embed` 打包，修改 `internal/webui/` 后需要重新编译程序。发布前确认版本号、`dist/SHA256SUMS`、运行平台和数据目录均符合部署环境。
