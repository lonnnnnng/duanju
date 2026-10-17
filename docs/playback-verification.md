# 播放修复验收

验收日期：2026-10-16。本次在 macOS 本地运行，未部署服务器。

## 故障与修复

- 旧 App 单集接口返回 HTTP 200、空正文；改用 `/novel/player/multi_video_model/preload/v1/`。请求使用 `mixed_video_id_map: {"1004": [分集ID]}`、`dr_scene: "preload"`、`video_platform: 1024`。
- 批量响应必须按 `data[所请求分集ID].video_model` 精确读取，兼容对象与 JSON 字符串，拒绝拿其他分集代替。
- 旧网页内嵌地址已改为 `protectedPlayback`，网页取流保留为回退路径。
- 接入官网 `/api/video/protected/play-info`，沿用当前页面域名，并先校验页面的剧与分集身份。
- 网页返回的 `play_info.vid` 是媒体 ID，不等于请求中的分集 `item_id`；不以它替代分集身份。
- 区分网页与 App 的密钥封装版本，支持官网返回的分离音视频；两路分别解密、同步跳转后合并。
- 在线播放、预缓存、HLS 封装与下载均传递独立音轨；下载完成后校验音轨存在。
- 日志记录接口状态和媒体结构，不记录临时播放地址、媒体密钥或响应正文。

## 已验证

- Chrome：《180万利润谁说了算》第 1–3 集连续播放、自动下一集；第 1 集快进到约 1:12 后继续播放。
- Chrome：《1991，从柴房到天明》第 1 集播放，实际画面 720×1280。
- App preload：《180万利润谁说了算》第 1、4、40、78 集均返回 1080p；各在起点和中段解码 3 秒，FFmpeg 使用 `-xerror`，音视频均存在。第 4 集完整下载后含音视频。
- Chrome：原失败的第 4 集恢复播放；通过选集跳转到第 78 集并播放到约 29 秒，两集实际画面均为 1920×1080。
- 真实源站：第一个样本在 0 秒与 62 秒处各解码 3 秒，FFmpeg 使用 `-xerror`，音视频均存在。
- 本地合成双轨 CENC：流式合并、生成 HLS、App 空响应回退网页后的完整下载，音视频压缩包哈希保持一致。
- `go test ./... -count=1`、`go vet ./...`、`go build` 通过。

真实源站测试须手动启用，避免普通回归依赖外部站点：

```sh
DUANJU_LIVE_PLAYBACK=1 go test ./internal/app -run '^TestLiveHongguoProtectedPlayback$' -count=1 -v
DUANJU_LIVE_PLAYBACK=1 go test ./internal/app -run '^TestLiveHongguoPreloadPlayback$' -count=1 -v
```

## 核验边界与来源

第 4 集的官网页面 HTTP 404、网页播放接口 HTTP 403 仅说明网页路径不可用，不能据此认定 App 路径不可用；本次 preload 实测已纠正这一判断。此次抽查第 1、4、40、78 集，没有逐集验收全部 78 集，也不保证上游接口长期稳定。

- 接口线索：[hongguoTV Issue #17](https://github.com/N3urda/hongguoTV/issues/17)。该讨论后续也有失效反馈，以本次实测为准。
- 参数与返回结构参考：[verify-best-def.js](https://github.com/jackotom/hongguo-drama/blob/84b6e88e65374942c123a53c7d6ad9e78bcd101b/scripts/verify-best-def.js)。仅阅读源码，未执行第三方脚本。
