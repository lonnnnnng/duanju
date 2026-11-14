# 红果取流签名与设备注册修复（2026-11-14）

- 影响范围：Web 端在线播放、下载取流、详情/分集列表
- 状态：已修复并部署到 `192.168.1.2`，服务器实测通过

## 1. 现象

Web 端播放与下载全部不可用：进入剧集取不到分集，或取流失败。

排查时三条链路表现为：

| 链路 | 现象 |
| --- | --- |
| App 签名 POST `/novel/player/multi_video_model/v1/` | `200` + 0 字节 |
| App 详情 `/novel/player/video_detail/v1/` | 返回不可解析数据（前端表现为「剧集 ID 无效」） |
| 网页 `/detail?series_id=` | `HTTP 500` |
| 网页取流页 `/player/<sid>/<vid>` | `HTTP 404` |

## 2. 根因

上游把红果 App 的取流/详情接口升级为**播放器专用签名 + 设备注册**：

1. `/novel/player/*` 不再接受旧的评论/目录签名，必须使用播放器签名
   （`X-Gorgon` 头固定为 `84 04 40 1c`，`X-Argus`/`X-Ladon` 使用播放器密钥与非随机常量）。
2. 请求必须携带**已注册设备号**（`https://log.snssdk.com/service/2/device_register/`
   换取 `device_id` + `install_id`），随机生成的设备号会被判为无效并返回空正文。
3. 取流接口从批量 `multi_video_model/v1` 改为单集 `video_model/v1`，
   入参为 `{video_id, content_type: 1, biz_param{need_all_video_definition, video_platform: 3}}`。
4. 响应里的播放地址可能带 base64 编码与多个备用地址，只读 `main_url` 会取不到地址。

## 3. 修复

新增与调整：

- `internal/app/provider_hongguo_player_sign.go`：播放器签名（Gorgon/Argus/Ladon + SM3 + AES-CBC）。
- `internal/app/provider_hongguo_device.go`：设备注册、注册失败冷却（10 分钟）、注册态持久化。
- `internal/app/provider_hongguo_app.go`：
  - `hongguoAppRequest` 对非评论接口先确保设备已注册，统一使用播放器签名；
  - 空正文时轮换已注册设备重试（最多 3 次），失败原因写入诊断日志；
  - 公共查询参数抽成 `hongguoAppQuery`，设备注册复用同一组参数（去掉 device_id/iid）。
- `internal/app/provider_hongguo_native_media.go`：
  - 改用 `/novel/player/video_model/v1/` 与新的入参；
  - 解析 `data.video_model`（兼容 JSON 字符串与 base64 编码）；
  - 新增 `hongguoMediaAddresses`，汇总 `main_url`/`backup_url*`/`url_list` 并自动解码 base64。

取流顺序为：站点 `play-info`（h264，优先给网页播放）→ App `video_model`（HEVC，兜底）
→ 网页页面。开放签名 GET 与第三方备用接口已在此后清理中移除（长期不可用）。

## 4. 验证

服务器实测：

```text
# 分集列表
180万利润谁说了算 | 78 chapters

# 取流（站点 play-info）
q=720 key=16 url=https://v3-hgweb.qznovelvod.com/...

# 取流（App video_model）
q=1080 key=16 url=https://v26-reading-video.qznovelvod.com/...

# 下载：整部 78 集在约 3 分钟内全部 success
# 播放：/api/ui/playback/open 返回 mimeType avc1；stream 接口返回 16.8MB MP4（ftyp iso5）
```

`go test ./...`、`go vet ./...` 全部通过。

## 5. 注意

- App 取流（`video_model`）目前只返回 HEVC（`bytevc1` / `h265_hvc1`），
  浏览器需硬件 HEVC 支持；网页播放优先走站点 `play-info` 的 h264。
- 站点 `play-info` 有频率限制，短时间内大量取流会 403，程序会暂停该域名一段时间。
- 设备注册被上游拒绝（HTTP 444 等）时会冷却 10 分钟，期间只能走站点链路。
