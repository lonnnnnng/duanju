# 红果短剧播放取流失败修复报告

- 日期：2026-10-21
- 版本：v0.1.16
- 影响范围：红果短剧在线播放、下载、预缓存
- 状态：已修复，已在服务器 `192.168.1.2` 验证并部署

## 1. 故障现象

用户在局域网访问 duanju 并点击播放时失败，提示：

```text
播放地址解析失败： App 取流失败： App 接口返回空正文（HTTP 200）；
网页取流失败：hongguoduanju.com HTTP 404；
备用取流失败：备用播放接口未返回该集可用的媒体和密钥，请稍后重试或确认该集是否仍可访问；
可点击“重试播放”。
```

服务器日志同时出现：

```text
/novel/player/multi_video_model/preload/v1/: App 接口返回空正文（HTTP 200）
/novel/player/video_detail/v1/: App 接口返回空正文（HTTP 200）
```

同一部剧的前几集可以播放，后续分集失败。网页路径对后续分集返回 404，说明官网只公开了前几集，后续分集必须走 App 接口。

## 2. 根因

### 2.1 播放接口签名升级

上游把 App 播放接口的签名从旧版 `X-Gorgon` 升级为带随机数的新版 `X-Gorgon`。继续使用旧签名时，网关直接返回 HTTP 200 空正文，因此旧代码把问题误判为“接口返回空”。

### 2.2 详情和目录接口需要旧版完整签名

详情接口 `/novel/player/video_detail/v1/` 和目录接口 `/reading/distribution/category/landpage/v/` 需要 `X-Argus`、`X-Ladon` 与 `X-Gorgon` 一起校验。只带旧 `X-Gorgon` 同样会得到空正文。

### 2.3 固定设备 ID 被风控

公开代码中常见的固定 `device_id`/`iid` 已被判定为异常，播放接口会返回：

```json
{"Code":110001,"Message":"未知异常"}
```

必须使用本机随机生成的设备 ID。

### 2.4 预加载接口不再稳定

此前修复使用的 `/novel/player/multi_video_model/preload/v1/` 在当前签名下仍可能返回空正文。当前可用路径是 `/novel/player/multi_video_model/v1/`，响应中的 `video_model` 直接包含可用清晰度列表。

## 3. 修复内容

### 3.1 新增播放接口签名

在 `internal/app/provider_hongguo_sign.go` 中实现新版 `X-Gorgon`：

- 输入包含实际发送的 `RawQuery`、请求体 MD5、SDK 版本 `67503104` 和时间戳。
- 使用 16 位随机数参与密钥和签名头。
- 输出前缀为 `8404 <随机数低字节> <随机数高字节> 4001`。
- 同时设置 `X-Khronos`、`X-SS-Req-Ticket`、`X-SS-STUB`、`X-Argus`、`X-Ladon`、`X-Tt-Request-Tag`、`Sdk-Version`、`Passport-Sdk-Version`、`X-Vc-Bdturing-Sdk-Version`。

### 3.2 按接口选择签名

在 `hongguoAppRequest` 中区分两类请求：

- 播放模型接口：使用新版 `X-Gorgon`。
- 详情、目录、评论接口：使用带 `X-Argus`/`X-Ladon` 的旧版签名；评论接口额外携带 `Comment-Source`/`Server-Channel`。

### 3.3 播放接口路径与请求体

`resolveHongguoAppMedia` 改为：

- 路径：`/novel/player/multi_video_model/v1/`
- 请求体：`mixed_video_id_map: {"1004": [分集ID]}` 与 `video_platform: 1024`
- 去掉不再需要的 `dr_scene: "preload"`

响应仍按 `data[所请求分集ID].video_model` 精确读取，兼容对象和 JSON 字符串，避免串集。

### 3.4 设备 ID

沿用启动时随机生成的 `device_id`/`iid`，不再使用公开固定值。

## 4. 测试与验证

### 4.1 自动化测试

新增或更新：

- `TestHongguoModelGorgonVector`：固定输入的新版 `X-Gorgon` 向量校验。
- `TestHongguoModelRequestSetsSignatureHeaders`：播放请求签名头完整性。
- `TestHongguoModelRequestsExactEpisode`：只读取请求分集的媒体，拒绝串集。
- `TestHongguoDanmakuCacheAndConcurrentRequests`：评论专用头不会泄漏到普通请求。
- `TestLiveHongguoModelPlayback`：真实源站播放与下载验收。

已通过：

```sh
go test ./... -count=1
go vet ./...
go build ./...
```

### 4.2 真实源站

- 《180万利润谁说了算》第 1、4、40、78 集：均返回 1080p，起点和中段解码 3 秒，音视频均存在；第 4 集完整下载后含音视频。
- 《谁说纨绔不能当状元第五季》第 4、5、6、40、155 集：均返回 1080p；第 5 集完整下载后含音视频。
- 服务器实测第 4、5、6 集播放流均返回 `200 video/mp4`，服务端将 HEVC 源转码为 H.264（MSE transcode）。

### 4.3 并发播放上限

验收过程中连续打开了多个播放会话且未关闭，默认上限为 4，曾导致用户看到“同时播放数量已达上限”。重启服务清空会话后恢复正常。正常使用中关闭标签页或等待 10 分钟会话过期即可释放名额。

## 5. 部署

- 发布版本：`v0.1.16`
- 发布仓库：`lonnnnnng/duanju`
- 服务器：`192.168.1.2:3333`
- 部署方式：使用 Release 中的 `duanju-v0.1.16-linux-amd64` 替换服务器程序，保留旧程序备份后重启。
- 健康检查：`/healthz` 返回 `status=ok` 与目标版本。

## 6. 已知边界

- 上游接口和签名可能继续调整，本次修复不保证长期稳定。
- 当前播放模型返回的清晰度列表为 HEVC（bytevc1）；浏览器不支持 HEVC 时由服务端转码为 H.264，下载保留原画。
- 当前实现直接使用 `video_model.video_list` 的内联地址；若上游后续只返回 `fallback_api`，需要补充 `fallback_api` 与 `key_seed` 解密链路。
- 官网仍只公开前几集，后续分集依赖 App 接口，不能仅凭网页 404 判定整剧不可用。

## 7. 变更文件

```text
docs/playback-verification.md
internal/app/provider_hongguo_app.go
internal/app/provider_hongguo_native_media.go
internal/app/provider_hongguo_native_media_test.go
internal/app/provider_hongguo_sign.go
internal/app/provider_hongguo_sign_test.go
internal/app/playback_remux_test.go
internal/app/ui_playback_danmaku_test.go
```

## 8. 参考

- drpys 红果果：<https://github.com/woshishiq1/drpys/blob/main/spider/js/%E7%BA%A2%E6%9E%9C%E6%9E%9C%5B%E7%9F%AD%5D.js>
- hongguoTV：<https://github.com/N3urda/hongguoTV>
- 仅阅读第三方源码用于接口与签名线索，未执行第三方脚本。
