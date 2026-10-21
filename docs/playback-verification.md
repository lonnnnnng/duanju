# 播放修复验收

验收日期：2026-10-21。本次在 macOS 本地运行，并在服务器 `192.168.1.2` 上部署验证。

## 故障与修复

- 上游把 App 播放接口的签名升级为带随机数的新版 `X-Gorgon`；旧签名请求返回 HTTP 200 空正文。
- 播放接口改用 `/novel/player/multi_video_model/v1/`，请求使用 `mixed_video_id_map: {"1004": [分集ID]}`、`video_platform: 1024`。
- 播放接口必须使用随机 `device_id`/`iid`；公开代码中的固定设备 ID 会被判为 `110001 未知异常`。
- 目录和详情接口改用带 `X-Argus`/`X-Ladon` 的旧版签名，否则同样返回空正文。
- 批量响应必须按 `data[所请求分集ID].video_model` 精确读取，兼容对象与 JSON 字符串，拒绝拿其他分集代替。
- 旧网页内嵌地址已改为 `protectedPlayback`，网页取流保留为回退路径；官网只公开前几集，后续分集必须走 App 接口。
- 网页返回的 `play_info.vid` 是媒体 ID，不等于请求中的分集 `item_id`；不以它替代分集身份。
- 区分网页与 App 的密钥封装版本，支持官网返回的分离音视频；两路分别解密、同步跳转后合并。
- 在线播放、预缓存、HLS 封装与下载均传递独立音轨；下载完成后校验音轨存在。
- 日志记录接口状态和媒体结构，不记录临时播放地址、媒体密钥或响应正文。

## 已验证

- App model：《180万利润谁说了算》第 1、4、40、78 集均返回 1080p；各在起点和中段解码 3 秒，FFmpeg 使用 `-xerror`，音视频均存在。第 4 集完整下载后含音视频。
- App model：《谁说纨绔不能当状元第五季》第 4、5、6、40、155 集均返回 1080p；第 5 集完整下载后含音视频。
- Chrome：原失败的分集恢复播放；通过选集跳转后连续播放。
- 真实源站：第一个样本在 0 秒与 62 秒处各解码 3 秒，FFmpeg 使用 `-xerror`，音视频均存在。
- 本地合成双轨 CENC：流式合并、生成 HLS、App 空响应回退网页后的完整下载，音视频压缩包哈希保持一致。
- `go test ./... -count=1`、`go vet ./...`、`go build` 通过。

真实源站测试须手动启用，避免普通回归依赖外部站点：

```sh
DUANJU_LIVE_PLAYBACK=1 go test ./internal/app -run '^TestLiveHongguoProtectedPlayback$' -count=1 -v
DUANJU_LIVE_PLAYBACK=1 go test ./internal/app -run '^TestLiveHongguoModelPlayback$' -count=1 -v
```

## 核验边界与来源

上游接口和签名会继续调整，本次修复不保证长期稳定。此次抽查了上述分集，没有逐集验收全部剧集。

- 接口与签名线索：[drpys 红果果](https://github.com/woshishiq1/drpys/blob/main/spider/js/%E7%BA%A2%E6%9E%9C%E6%9E%9C%5B%E7%9F%AD%5D.js)、[hongguoTV](https://github.com/N3urda/hongguoTV)。仅阅读源码，未执行第三方脚本。
- 播放接口返回的 `video_list` 当前为 HEVC（bytevc1），应用在客户端不支持 HEVC 时会转码为 H.264 播放。
