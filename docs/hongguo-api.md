# 红果接口说明

本文记录 Web 端当前实际使用的上游接口、签名方式与降级顺序。上游接口变动频繁，
排障时以本文与诊断日志为准，不要直接套用历史文档里的 URL。

## 1. 取流顺序

`internal/app/provider_hongguo.go` 的 `resolveHongguoMedia` 按以下顺序解析播放地址，
成功后立即返回：

| 顺序 | 链路 | 返回编码 | 说明 |
| --- | --- | --- | --- |
| 1 | App `POST /novel/player/multi_video_model/player/v1/` | 完整清晰度档位，筛除 `bytevc2` | 使用 V5 签名，播放与下载共用 |
| 2 | App `POST /novel/player/multi_video_model/v1/` | 同上 | 主入口 HTTP 200 空正文时使用，同一分集、设备和请求体；V5 签名不发送 `X-Helios` |
| 3 | 站点播放页 `GET /player/<series_id>/<video_id>` | 页面内 `_ROUTER_DATA` | App 取流失败后的最后回退 |

两个 App 入口共用媒体解析、编码筛选和清晰度选择逻辑。默认选择最高可用档位，
下载遵守既有清晰度设置，合并仍校验实际编码与分辨率。`play-info` 最高只有 720p，
2026-11-14 起已移除，避免与 App 返回的 1080p 媒体混用。

同一设备的两个入口均返回空正文，或只返回不兼容编码时，最多切换三次辅助设备。
每次解析独立记录已失败的设备，池中未尝试的设备用尽后才注册新设备，避免 A → B → A 循环。
参见 [2026-11-20 播放修复](2026-11-20-playback-recovery.md)。

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

基地址：`https://api5-normal-sinfonlinea.fqnovel.com`。使用独立的 7.4.3.32
播放参数及 V5 签名；不要套用目录接口的 7.3.5.32 签名。
`version_code`、`manifest_version_code`、`update_version_code` 均为 `74332`，
与 `version_name=7.4.3.32` 一致。版本字段在签名前补齐。

```http
POST /novel/player/multi_video_model/player/v1/
{"mixed_video_id_map":{"1004":["<分集 ID>"]},"dr_scene":"player",
 "biz_param":{"detail_page_version":0,"device_level":1,
 "disable_digg_stat":false,"disable_video_relate_book":false,
 "need_all_video_definition":true,"need_mp4_align":false,
 "use_os_player":false,"use_server_dns":false,"video_platform":1024}}
```

主入口空正文时改用 `/novel/player/multi_video_model/v1/`，请求体及设备相同；
批量入口发送 `X-Medusa`、`X-Khronos`、`X-SS-STUB`，不发送 `X-Helios`。
响应按 `data.1004.<分集 ID>` 或 `data.<分集 ID>` 定位，`video_model` 可能是对象或 JSON 字符串（也可能再套一层 base64），
解析后取 `video_list`：每个档位包含 `main_url`、`backup_url*`、`url_list`、
`video_meta.codec_type`/`definition`/`vheight`、`encrypt_info.spade_a`。

要点：

- `bytevc2` 档位会被跳过；同一清晰度优先 H.264，其他兼容编码由既有播放流程处理；
- 地址可能是 base64 编码，需要解码后判断；
- 同一档位的多个备用地址按主机健康度排序后使用；
- `spade_a` 解出 16 字节 AES-128 密钥，供 FFmpeg `-decryption_key` 解密。

### 3.5 评论与弹幕

```http
POST /novel/commentapi/comment/list/<video_id>/v1/
```

使用评论签名（带 `Comment-Source: 601`、`Server-Channel: 1000`），
该路径不需要设备注册。

### 3.6 榜单

```http
GET /reading/bookapi/bookmall/cell/change/v/
```

使用 App 设备签名（与首页、搜索相同）。关键参数：

| 参数 | 说明 |
| --- | --- |
| `selected_items` | 分类：`all`、`series_album`、`comic_series_rank`、`human`、`ai_playlet` |
| `sub_selected_items` | 榜型，如 `ranklist_hot_sc`、`human_hot_play`、`human_new_rank` |
| `panel_selected_items` | 题材/性别筛选 ID；空字符串为总榜 |
| `offset` / `session_id` / `rank_version` / `filter_ids` | 翻页游标 |

响应 `data.cell_view.cell_selector` 给出分类、榜型和筛选项；`cell_data` 的
`video_data` / `ugc_user_data` 给出条目。剧集用 `series_id`；系列剧用
`jump_schema` 里的 `video_series_id`；`recommend_info.rank` 是名次，
`rec_text_item.RecommendText` 是热度指标，`cover` 是 HEIC 封面。
网页端通过 `/api/ui/rankings/catalog` 和 `/api/ui/rankings/app` 封装该接口，
并由 `/api/ui/image` 转换 HEIC 封面。

## 4. 签名

| 路径 | 签名 |
| --- | --- |
| `multi_video_model/player/v1/` | V5：`X-Helios`、`X-Medusa`、`X-Khronos`、`X-SS-STUB` |
| `multi_video_model/v1/` 批量入口 | V5：`X-Medusa`、`X-Khronos`、`X-SS-STUB`；省略 `X-Helios` |
| `video_detail` 等其他 App 接口 | 播放器签名：`X-Gorgon`、`X-Argus`、`X-Ladon`、`X-Khronos`、`X-SS-Req-Ticket`，有请求体时带 `X-SS-STUB` |
| `/novel/commentapi/*` | 评论签名（同上头部，参数与非随机种子不同） |
| 站点接口 | 无需签名，靠 Cookie/Referer/`X-Xs-From-Web` |

签名对 `RawQuery` 与请求体字节级敏感：换设备、改参数、改编码后必须重新签名。
任何 `query` 变更都要在发送前完成，不能先签名再改 URL。所有签名在请求限流队列放行后生成，避免等待导致签名过期。

## 5. 故障判定

| 现象 | 含 义 | 处理 |
| --- | --- | --- |
| `200` + 0 字节 | 接口入口、签名或设备不被接受，也可能是限流；单凭空响应无法区分 | 播放主入口先切批量入口，再尝试本次未失败的辅助设备 |
| `200` + `{"code":101116}` | 上游业务抖动 | 退避重试 |
| `403` | 站点频率限制 | 该域名进入冷却，换域名或等冷却 |
| `444` | 注册/接口风控 | 冷却 10 分钟后重新注册 |
| `404`/`500`（页面） | 站点改版或只公开部分分集 | 依赖 App 链路 |

诊断日志事件：`provider.hongguo.app_response`、`provider.hongguo.web_playback`、
`provider.hongguo.device_pool`、`provider.hongguo.playback_response`、
`provider.hongguo.playback_fallback`。日志只记录结果与结构，不记录设备身份、临时直链和密钥。

### 2026-11-23 再次空响应

线上 v0.2.20 在《谁让这个文盲修仙的第七季》第 1 集播放失败：主入口和批量入口均返回
HTTP 200、0 字节，尝试辅助设备仍失败。独立临时设备池也复现，故障发生在获取媒体地址阶段。

对照 `guoapp-modules/hongguo` 当前 `cdf8a24` 及 [PR #5](https://github.com/guoapp-modules/hongguo/pull/5)
的线索后，对同一身份、分集与请求体做真实请求对照：

| 批量入口请求 | 故障剧与对照剧《0174号奖券》 |
| --- | --- |
| 原参数、完整 V5 头 | HTTP 200，0 字节 |
| 补齐 `manifest_version_code`，完整 V5 头 | HTTP 200，0 字节 |
| 补齐版本字段，同时省略 `X-Helios` | HTTP 200，约 24–25 KiB，1080p、5 个清晰度档位及有效内容密钥 |

参考项目的新 `video_tab` 入口用 Go 与原版 JavaScript 签名均为空，未将其接入。
本次只修正播放请求字段与批量入口头部组合，保留 V5 签名算法、清晰度选择、合并约束和有界设备重试。
证据说明当前请求组合已不被上游接受；空正文没有原因码，无法推断其内部具体规则。

手动联网验证 `TestLiveHongguoPlaybackRecovery` 已加入本次故障剧，使用独立临时设备池；
普通测试仍默认跳过该联网用例。部署前后的真实验证结果应以当次执行为准。

本次修复后验证：

- 故障剧第 1/105/210 集、《公路求生榜一说她要走到公路尽头第三季》第 1/103/207 集、
  《0174号奖券》第 1/44/88 集均取得 1080p，开头和中段的音视频解码通过。
- 三部首集完整下载成功，媒体包含 HEVC 视频和 AAC 音频。
- 故障剧和《0174号奖券》在 Chromium 桌面 1280×900、手机模拟 390×844 下均实际出画、
  播放进度前进且可关闭弹框；测试服务和设备池均独立于线上目录。
- `go test ./... -count=1 -timeout=10m`、`go vet ./...` 通过。
