# 红果接口说明

本文记录 Web 端当前实际使用的上游接口、签名方式与降级顺序。上游接口变动频繁，
排障时以本文与诊断日志为准，不要直接套用历史文档里的 URL。

## 1. 取流顺序

`internal/app/provider_hongguo.go` 的 `resolveHongguoMedia` 按以下顺序解析播放地址，
成功后立即返回：

| 顺序 | 链路 | 返回编码 | 说明 |
| --- | --- | --- | --- |
| 1 | App `POST /novel/player/video_model/v1/` | 1080p HEVC（`hvc1`）+ HE-AACv2 | 与上游客户端一致，返回全部档位并取最高，播放与下载共用 |
| 2 | 站点播放页 `GET /player/<series_id>/<video_id>` | 页面内 `_ROUTER_DATA` | 仅在站点仍保留 SSR 数据时可用 |

**只保留一条主链路的原因**：站点 `play-info` 只接受 `360p/480p/720p`（传 `1080p` 返回
HTTP 400），返回 H.264 + AAC-LC；App 接口返回 `1080/720/540/480/360` 全部档位，
产物是 HEVC + HE-AACv2。两条链路交替命中会让同一部剧出现混合编码，合并时被迫转码。
2026-11-14 起移除 `play-info` 兜底，播放与下载都固定走 App 主链路，
产物统一为 1080p HEVC + HE-AACv2，合并无需转码。

播放端若浏览器不支持 HEVC，会在计划阶段标记 `video_codec_unsupported` 并由服务端转码为 H.264。

候选域名由主机健康统计排序（见 [设备池与主机健康](2026-11-14-device-pool-and-host-health.md)），
失败域名在 2 分钟冷却期内排到最后。

2026-11-06 至 2026-11-14 期间曾保留「开放签名 GET」和第三方备用接口
（`djapi.999888456.xyz`）两条兜底；两者均已稳定不可用，现已移除。

## 2. 站点接口

基地址：`https://hongguoduanju.com`（可通过 `DUANJU_HONGGUO_URL` 覆盖）。

### 2.1 播放信息（已停用）

> 2026-11-14 起不再调用。该接口最高 720p，且会与 App 链路混出不同编码，保留本节仅作历史参考。

```http
POST /api/video/protected/play-info
Content-Type: application/json
Referer: https://hongguoduanju.com/player/<series_id>/<video_id>
Origin: https://hongguoduanju.com
X-Xs-From-Web: 1

{"series_id": "<19 位剧 ID>", "item_id": "<19 位分集 ID>", "definition": "720p"}
```

`definition` 由请求画质决定，默认 `720p`，可选 `360p`/`480p`/`720p`。

响应要点：

- 返回视频（必要时还有音频）的直链与 `player_access_key`；
- 直链可能带 `spade_a` 密钥，属于 CENC 加密媒体，由 FFmpeg 解密；
- 站点对该接口有频率限制，短时间批量请求会返回 `403`，需要冷却后重试。

### 2.2 详情页与播放页

`GET /detail?series_id=<剧 ID>` 与 `GET /player/<剧 ID>/<分集 ID>` 属于 SSR 页面，
解析 `window._ROUTER_DATA`。站点改版后这两条路径可能返回 `404`/`500`，
只作为最后的兜底。

## 3. App 接口

基地址：`https://api5-normal-sinfonlineb.fqnovel.com`。

### 3.1 公共查询参数

```text
aid=8662&app_name=novelread&version_code=73532&version_name=7.3.5.32
&manifest_version_code=73532&update_version_code=73532&channel=update_64
&device_platform=android&os=android&ssmix=a&device_type=25053RT47C
&device_brand=Redmi&language=zh&os_api=36&os_version=16
&resolution=1280*2772&dpi=520&ac=wifi
&device_id=<已注册设备号>&iid=<已注册安装号>&_rticket=<毫秒时间戳>
```

`_rticket` 每次请求重新生成，并且必须与签名时使用的 `RawQuery` 完全一致。

请求头：`User-Agent` 为 `com.phoenix.read/73532 ...`，
`X-XS-From-Web: 0`，`Sdk-Version: 2`，有请求体时 `Content-Type: application/json; charset=utf-8`。

### 3.2 设备注册

```http
POST https://log.snssdk.com/service/2/device_register/?<公共参数，去掉 device_id 与 iid>&req_id=<uuid>&_rticket=<毫秒时间戳>

{"magic_tag":"ss_app_log","header":{...设备信息...},"_gen_time":<毫秒时间戳>}
```

响应里的 `device_id`/`install_id` 才是有效设备号。未注册的随机设备号会被上游判为无效，
表现为 `200` + 0 字节。注册设备保存在 `data/hongguo-devices.json`（最多 3 个）。

### 3.3 分集列表

```http
POST /novel/player/video_detail/v1/
{"series_id": "<剧 ID>"}
```

响应 `data.video_data.video_list` 提供各集的 `vid`、`vid_index`、`vertical`；
`episode_cnt` 用于校验分集是否完整、连续。

### 3.4 单集取流

```http
POST /novel/player/video_model/v1/
{"video_id": "<分集 ID>", "content_type": 1,
 "biz_param": {"need_all_video_definition": true, "video_platform": 3}}
```

响应 `data.video_model` 可能是对象或 JSON 字符串（也可能再套一层 base64），
解析后取 `video_list`：每个档位包含 `main_url`、`backup_url*`、`url_list`、
`video_meta.codec_type`/`definition`/`vheight`、`encrypt_info.spade_a`。

要点：

- `video_platform=3` 当前只返回 HEVC，`bytevc2`（H.266）档位会被跳过；
- 地址可能是 base64 编码，需要解码后判断；
- 同一档位的多个备用地址按主机健康度排序后使用；
- `spade_a` 解出 16 字节 AES-128 密钥，供 FFmpeg `-decryption_key` 解密。

### 3.5 评论与弹幕

```http
POST /novel/commentapi/comment/list/<video_id>/v1/
```

使用评论签名（带 `Comment-Source: 601`、`Server-Channel: 1000`），
该路径不需要设备注册。

## 4. 签名

| 路径 | 签名 |
| --- | --- |
| `/novel/player/*` | 播放器签名：`X-Gorgon`（`84 04 40 1c` 前缀）、`X-Argus`、`X-Ladon`、`X-Khronos`、`X-SS-Req-Ticket`，有请求体时带 `X-SS-STUB` |
| `/novel/commentapi/*` | 评论签名（同上头部，参数与非随机种子不同） |
| 站点接口 | 无需签名，靠 Cookie/Referer/`X-Xs-From-Web` |

签名对 `RawQuery` 与请求体字节级敏感：换设备、改参数、改编码后必须重新签名。
任何 `query` 变更都要在发送前完成，不能先签名再改 URL。

## 5. 故障判定

| 现象 | 含 义 | 处理 |
| --- | --- | --- |
| `200` + 0 字节 | 签名/设备号无效，或高频限流 | 轮换设备池中的其它设备；仍失败则等待冷却 |
| `200` + `{"code":101116}` | 上游业务抖动 | 退避重试 |
| `403` | 站点频率限制 | 该域名进入冷却，换域名或等冷却 |
| `444` | 注册/接口风控 | 冷却 10 分钟后重新注册 |
| `404`/`500`（页面） | 站点改版或只公开部分分集 | 依赖 App 链路 |

诊断日志事件：`provider.hongguo.app_response`、`provider.hongguo.web_playback`、
`provider.hongguo.device_pool`。日志只记录结果与结构，不记录临时直链和密钥。