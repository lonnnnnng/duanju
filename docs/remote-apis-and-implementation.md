# 远程接口与实现方式（Web 端 / Android 端）

本文记录两端当前实际使用的远程接口、参数形态、解析方式与关键实现，覆盖首页、搜索、详情、播放、下载五条主链路。所有接口与行为均以当前代码为准，涉及的签名、密钥派生只描述结构，不记录具体常量。

- Android：`android/app/src/main/java/com/duanju/player/`
- Web（Go 服务端）：`internal/app/`
- Web（浏览器前端）：`internal/webui/`

---

## 一、总体架构

### Android

手机 App 直接访问红果开放域名，**不经过 duanju 服务器**：

```
App ──HTTPS──> api5-normal-sinfonlinec.fqnovel.com   （首页/搜索/详情/取流，无签名）
App ──HTTPS──> CDN（mp4 直链）                        （ffmpeg 本地解密）
App ──本地──> FFmpegKitNext                            （-decryption_key，-c copy 落盘）
App ──本地──> Media3 ExoPlayer                         （播放已解密的本地 mp4）
```

- 全部请求为 `GET`，无请求头签名（`fetch-debug` / `bookmall/tab` / `multi_video_detail` / `multi_video_model` 属于开放接口族）。
- 设备身份 `device_id` / `iid` 由 App 本地随机生成并持久化（`DirectClient.identity`，`SharedPreferences` key `duanju`）。
- 内容密钥由响应里的 `spade_a` 本地派生（`HongguoSign.contentKey`），再做 ffmpeg 解密。

### Web

浏览器只与本机/局域网 Go 服务通信，Go 服务再访问上游：

```
浏览器 ──HTTP──> duanju Go 服务 (/api/ui/*)  ──HTTPS──> 红果 App/官网/备用接口
                     │
                     ├── ffmpeg（转封装 / 转码 / 解密 / 合并）
                     ├── HLS 本地代理（重写 m3u8、注入密钥）
                     └── 磁盘缓存（剧库元数据、详情、搜索、榜单）
```

- 浏览器侧不做签名，也不直接接触上游。
- 上游请求分三类：App 接口（需签名）、官网页面/接口（部分需 Referer/Cookie 语义）、第三方备用播放接口。
- 剧库、详情、搜索的元数据会落盘缓存；播放与下载按需重新解析媒体地址（地址有时效）。

---

## 二、Android 端

### 2.1 接口总表

| 功能 | 方法 | 地址 | 关键参数 | 是否签名 |
| --- | --- | --- | --- | --- |
| 首页信息流 | GET | `https://api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/bookmall/tab/v/` | `tab_type=8`、`offset`、`session_id`、`selected_items`，其余为完整真机参数 | 否 |
| 搜索 | GET | `https://api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/search/fetch-debug/v/` | `query`、`count=20` | 否 |
| 详情/选集 | GET | `https://api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/video_tab/multi_video_detail/v/` | `video_series_id`、`vs_id_type=1` | 否 |
| 取流 | GET | `https://api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/video_tab/multi_video_model/v/` | `video_id`、`video_platform=1024`、`need_all_video_definition=1`、`version_code=74232` | 否 |
| 封面/剧照 | GET | 响应里的 CDN 绝对地址 | 直接交给 `BitmapLoader` | 否 |

参数公共部分由 `openCommonFull()` 生成（`DirectClient.kt:475`）：`iid`、`device_id`、`ac=wifi`、`channel`、`aid=8662`、`app_name=novelread`、`version_code`/`version_name`、`device_platform=android`、机型/分辨率/dpi、`host_abi`、`gender` 等约 30 项。

**版本号刻意分开：**

- 首页/搜索/详情：`version_code=65532`（`version_name=6.5.5.32`）
- 取流：`version_code=74232`（`version_name=7.4.2.32`）

只有 74232 返回 HEVC（`bytevc1`）；65532 只返回 H.266（`bytevc2`），设备无解码器无法播放。

### 2.2 首页

实现：`DirectClient.homeTab()`（`DirectClient.kt:498`），页面入口 `LibraryFragment.kt:123`。

1. `GET bookmall/tab/v/`，`tab_type=8`（短剧）。
2. 在 `data.tab_item[]` 中优先选 `tab_type` 匹配、`cell_data` 非空的 tab。
3. 数据是三层嵌套：`cell_data[].cell_data[].video_data[]`，同时也兼容直接挂在 `cell_data[].video_data[]` 的情况；遍历时用 `series_id`（缺省 `vid`）去重。
4. 字段映射：
   - `title` / `video_desc` → 标题
   - `episode_cnt` → 集数
   - `cover` / `horiz_cover` → 封面
   - `score`、`play_cnt`
   - `tag_info.text` / `tag_info.bg_color[0]` → 角标与颜色（如「爆剧」「新剧」）
   - `sub_title_list[data_type=27]` → 热度值；`data_type=3` → 分类
   - `recommend_info.rank`（JSON 字符串）→ 榜单名次
5. 分页：`next_offset > offset` 判定还有更多（`has_more` 在 `offset=0` 时恒为 false，不可信）；`session_id` 透传。
6. 筛选面板：
   - `cell_selector.outer_row.items[]` → 一级分类 chip（`selector_item_id`、`show_name`）
   - `cell_selector.inner_rows[].items[]` → 两级筛选分组（`row_name` + 选项）
   - 选中后把 `selected_items` 传回同一接口，服务端只认单个 `selected_items`。
7. 客户端缓存：无持久化缓存，列表在内存里；封面由 `BitmapLoader` 做内存 LRU + 磁盘缓存。

### 2.3 搜索

实现：`DirectClient.searchHomeItems()` + `searchHits()` + `detailBrief()`。

1. `GET search/fetch-debug/v/?query=<urlencoded>&count=20`。
2. **流式截断解析**（`readSearchData`，`DirectClient.kt:309`）：该响应常达 5–6 MB，其中 99% 是 `debug_info`，`data` 数组出现在前面。客户端逐块读取，定位 `"data":` 后做括号配对，取到完整数组立即断开连接，避免整包读入。
3. 结果过滤：仅保留 `type ∈ {短剧, 漫剧}`，按 `id` 去重；标题去掉 HTML 标签；从 `display.summary` 里正则提取集数（`"集数":"N"`）与 CP 方。
4. 搜索接口本身不返回封面/播放量，所以对每条命中再拉一次详情（`detailBrief`），**8 线程并行**，单条 20s 超时，失败则回退为仅标题的卡片。
5. 失败重试 3 次（间隔 0.6s / 1.2s），空 `data` 视为无结果。

### 2.4 详情

实现：`DirectClient.seriesDetail()`（`DirectClient.kt:377`）。

1. `GET multi_video_detail/v/?video_series_id=<id>&vs_id_type=1`。
2. 响应结构：`data.video_detail_data.<seriesId>.video_data`。
3. 解析字段：`series_title`、`series_cover`（JPEG）、`episode_cnt`（缺省取列表长度）、`series_play_cnt`、`series_status`、`series_intro`、`series_sub_title_list[]`、`category_schema[0].name`。
4. 选集：`video_list[]` 取 `vid` / `vid_index` / `duration` / `disable_play`，按 `vid_index` 排序——**不能按 `vid` 排序**，`vid` 是雪花 ID，与集数无关。
5. 首页搜索补详情复用 `detailBrief`，同一接口只取卡面字段，减少解析量。
6. 界面层 30 集一组分组（`EpisodeGroups`），详情页与播放器共用。

### 2.5 播放

实现：`PlayerActivity.playDirect()`（`PlayerActivity.kt:369`）+ `DirectClient.media()` / `decryptTo()`。

1. `chapters()`：同一详情接口取 `vid` 列表（`disable_play` 过滤），按 `vid_index` 排序。
2. `media(videoId)`：`GET multi_video_model/v/?...&version_code=74232&video_id=<vid>&video_platform=1024&need_all_video_definition=1`。
3. 解析 `data.video_model_data.<vid>.video_model`（可能是字符串或对象）：
   - 遍历 `video_list[]`，跳过 `codec_type` 含 `bytevc2`（H.266）的项；
   - 从 `video_meta.definition`（如 `1080p`）提取清晰度，取最高；
   - 收集所有清晰度为变体；读取 `encrypt_info.spade_a`。
4. 由 `spade_a` 派生 16 字节内容密钥（`HongguoSign.contentKey`），交由 ffmpeg `-decryption_key`。
5. **整集下载后播放**：`decryptTo()` 调 FFmpegKitNext：
   - `-protocol_whitelist file,http,https,tcp,tls,crypto`
   - `-decryption_key <hex>` `-i <cdn-url>` `-c copy -movflags +faststart -f mp4 <tmp>`
   - 输出先写 `.tmp`，成功后原子重命名到 `cache/direct/<seriesId>_<ep>.mp4`
6. TLS：ffmpeg 自带 openssl 不带 Android 系统 CA，客户端把 `cacerts` 合并导出为 PEM，用 `-cafile` 交给 ffmpeg。
7. 播放：`ProgressiveMediaSource` 指向本地 mp4；本地文件已存在则直接复用。
8. 失败处理：**没有实际回退**。`decryptTo` 的注释提到「ffmpeg 失败时回退 Java 下载」，但当前实现只调用 `decryptViaFfmpeg`，失败直接抛错，需重试或换网络。
9. 无用户可见的「下载到相册/文件」功能——`decryptTo` 只是播放缓存。

### 2.6 下载

**Android 未实现用户下载功能。** `DirectClient.decryptTo()` 仅把当前集解密到 App 私有缓存目录供播放使用，不导出到公共目录，也没有下载队列/进度页。

### 2.7 其他实现细节

- 设备身份：19 位随机正数，`device_id` 与 `iid` 分开持久化；缺失时重新生成。
- 并发：搜索补详情固定 8 线程；其余请求串行。
- 重试：开放接口统一 3 次、退避 0.6s×尝试次数。
- 图片：`BitmapLoader` 直接加载 CDN 地址，内存 LRU + 磁盘缓存，未走自家代理。
- 版本检测：点击设置页「版本」调用 GitHub `releases/latest`，与 `BuildConfig.VERSION_NAME` 比较后弹窗，下载走 Release 里 `*-android.apk` 直链。

---

## 三、Web 端

### 3.1 浏览器 ↔ Go 服务接口（按功能）

| 功能 | 接口 | 说明 |
| --- | --- | --- |
| 首页/剧库 | `GET /api/ui/dramas` | 剧库快照；`refresh=1` 重建、`more=1` 追加、`update=1` 增量、`priority=` 优先补齐 |
| 首页推荐 | `GET /api/ui/recommendations` | 推荐流分页（`genre`、`offset`、`session`、`seen`） |
| 榜单 | `GET /api/ui/rankings` | 榜单 id + 页码 |
| 搜索 | `GET /api/ui/search?keyword=` | 搜索结果（含缓存） |
| 详情 | 复用剧库数据 | 章节在播放/下载时解析 |
| 播放 | `POST /api/ui/playback/plan` → `open` / `prepare` / `stream` / `hls/open` / `status` / `control` / `progress` | 客户端能力协商 + 服务端出流 |
| 下载 | `POST /api/ui/download` | 提交剧集 ID 与清晰度，异步解析入队 |
| 下载管理 | `/api/ui/tasks`、`/tasks/pause|resume|cancel|retry|priority|delete|clear|verify` | 队列与文件校验 |
| 封面代理 | `GET /api/ui/image` | 带 Referer 拉取上游图片 |
| 配置/更新 | `/api/ui/config`、`/api/ui/update`、`/api/ui/ffmpeg` | 设置与自更新 |
| Emby | `/api/emby/*` | 媒体库导出（另述） |

### 3.2 Go 服务 → 上游接口

| 功能 | 方法 | 地址 | 签名 |
| --- | --- | --- | --- |
| 剧库/推荐 | POST | `https://api5-normal-sinfonlineb.fqnovel.com/reading/distribution/category/landpage/v/` | 旧版（X-Argus/X-Ladon/X-Gorgon） |
| 剧库回退 | GET | `https://hongguoduanju.com/category/{real-drama|comic-drama|ai-drama|comic}?page=N` | 无（SSR 页面） |
| 搜索 | GET | `https://hongguoduanju.com/api/search/result?query=` | 无（带 Referer） |
| 详情/选集 | POST | `https://api5-normal-sinfonlineb.fqnovel.com/novel/player/video_detail/v1/` | 旧版 |
| 详情回退 | GET | `https://hongguoduanju.com/detail?series_id=` | 无（SSR 页面） |
| 取流（主） | POST | `https://api5-normal-sinfonlineb.fqnovel.com/novel/player/multi_video_model/v1/` | 新版 X-Gorgon |
| 取流（网页） | POST | `https://hongguoduanju.com/api/video/protected/play-info` | 无（Referer/Origin + `X-Xs-From-Web: 1`） |
| 取流（备用） | GET | `https://djapi.999888456.xyz/api/hongguo/play?id=<base64>` | 无（第三方） |
| 榜单 | GET | `https://hongguoduanju.com/rank/{hot-drama|hot-real-drama|hot-comic-drama|hot-ai-drama}?page=N` | 无（SSR 页面） |
| 自更新 | GET | `https://api.github.com/repos/lonnnnnng/duanju/releases/latest` | 无 |

### 3.3 首页 / 剧库

实现：`fetchHongguoDramas()`（`provider_hongguo.go:19`）→ `fetchHongguoAppCatalog()`（`provider_hongguo_catalog.go:38`）。

1. App 目录接口按轮次交替扫描三个 feed：`short_play`（真人剧）、`comic_series`（漫剧）、`ai_series`（AI 剧），每轮各取一页。
2. 每页请求体：`req_scene`、`offset`、`limit=18`、`req_type=only_content`、`need_selector_panel=false`、`client_req_type`（首页 3 / 翻页 2）、`session_id`、`filter_ids`、`select_items{genre:[key], sort:[online_time], ...}`。
3. 分页游标持久化到 `data/`（`offset`、`sessionId`、`exhausted`、更新时间），30 分钟未更新会丢弃 `session_id`。
4. 去重合并（`mergeDramaMetadata`），统一按标题排序后返回。
5. 回退：App 目录失败且无缓存时并发抓官网分类页（4 条路由），解析 `_ROUTER_DATA` / `mergeLoaderData` 里的 `recommendList`。
6. 推荐流：同一 `landpage` 接口，`req_scene` 取 genre 场景、`sort` 为空，`filter_ids` 传已看 ID 去重，校验 `next_offset` 必须前进。
7. 榜单：抓官网榜单 SSR 页 `?page=N`，从 `data-fn-name="mergeLoaderData"` 的脚本参数里解出 JSON。

### 3.4 搜索

实现：`fetchHongguoSearch()`（`provider_hongguo_search.go:78`）。

1. 直连官网客户端搜索接口：`GET https://hongguoduanju.com/api/search/result?query=`，`Referer` 为 `/search/<keyword>`。
2. 解析 `searchList[]`，只保留含 `video_data` 的行，映射为 `Drama`。
3. 关键字校验：1–30 字符、无控制字符。
4. 并发控制：同关键字的并发请求合并（single-flight），结果缓存 5 分钟、上限 64 条。
5. `totalCount` 作为总数；结果为空但有行时报「没有可识别的剧集」。

### 3.5 详情

实现：`fetchHongguoChapters()`（`provider_hongguo.go:131`）。

1. 主路径：`POST /novel/player/video_detail/v1/`，body `{"series_id": "<id>"}`，旧版签名。
2. 校验严格：
   - 返回的 `series_id` 必须等于请求 ID；
   - `vid_index` 必须连续、从 1 开始；
   - `vid` 必须为纯数字且不重复；
   - 分集数与 `episode_cnt` 一致，否则视为不完整。
3. 缓存：详情 5 分钟（最多 128 条），并发合并（single-flight）。
4. 回退：`GET https://hongguoduanju.com/detail?series_id=`，解析 `_ROUTER_DATA` 中 `detail_page.seriesDetail.vid_list`。
5. 章节的内层地址存为伪协议 `hongguo-cenc://<vid>`，真正媒体地址在播放/下载时现取。

### 3.6 播放

实现：`resolveHongguoMedia()`（`provider_hongguo.go:192`）、`planMedia()`（`ui_playback_plan.go:97`）、`streamPlayback()`（`playback_stream.go:163`）。

**取流三级回退：**

1. App 模型接口 `POST /novel/player/multi_video_model/v1/`（新版 X-Gorgon）：
   - body：`mixed_video_id_map{"1004":[vid]}` + `biz_param{video_platform:1024, need_all_video_definition:true, device_level:3, ...}`；
   - 只读响应中请求 vid 对应的条目，禁止回退到第一项（防串集）。
2. 网页路径：先 `GET /player/<seriesId>/<vid>` 校验页面身份（`vid`、`series_id` 必须匹配），若页面声明 `protectedPlayback`，再 `POST /api/video/protected/play-info`：
   - body：`series_id`、`item_id`、`definition`（360p/480p/720p）；
   - 头：`Referer` 指向该分集页、`Origin`、`X-Xs-From-Web: 1`；
   - 解析 `play_info`：要求 `format=dash`、`codec_type=h264`、`expires_at` 未过期、音视频 `kid` 合法且 `player_access_key` 一致；支持音视频分离（`media.AudioURL`）。
3. 备用第三方 `djapi.999888456.xyz/api/hongguo/play?id=<base64(ref)>`：
   - `ref` 为 JSON（`content_type=1004`、`series_id`、`vid`、`video_platform=3`）；
   - 响应可用 `v2.` 前缀压缩格式，解出 `key_urls[]`（`src`、`kid`、`spade_a`、`name`），按名称取清晰度。

**客户端能力协商（`planMedia`）：**

- 若媒体无 CENC、无分离音轨、编码受支持、且直连探测通过 → `delivery=redirect`，浏览器直接播上游地址。
- 需要改容器 → 服务端 `remux`（`-c copy`）输出 fMP4。
- 需要解密（CENC）或音轨/编码不兼容 → 服务端解密 + 必要时转码：
  - 视频：`libx264 baseline`、`yuv420p`、`fps=30`、`crf=23`、`maxrate=12000k`；
  - 音频：`aac 128k 48k stereo`；
  - 输出：`-movflags +frag_keyframe+empty_moov+default_base_moof`，`pipe:1` 流式返回。
- HLS 路径：本地 `hlsProxy` 重写 m3u8（相对地址改本地、`EXT-X-KEY`/`EXT-X-MAP` 一并改写），密钥在服务端注入，浏览器不接触原始密钥。
- 播放会话：`/playback/open` 建立会话（可选断点续播、历史记录），`/playback/status`、`/control`、`/progress` 管理进度；前端用原生 `<video>` 或 hls.js。

### 3.7 下载

实现：`handleDownload` → `enqueueDramasAsync` → `DownloadEpisodeWithProgress` → `downloadMediaWithProgress`（`download_media.go:17`）。

1. 浏览器 `POST /api/ui/download`，body 含剧集 ID 列表与清晰度（360/480/720）。
2. 服务端异步解析章节，按集创建任务（持久化到 `data/`），返回任务视图；由 `/api/ui/tasks/*` 管理暂停/恢复/取消/重试/优先级/删除。
3. 每集执行：
   - 复用已有文件前先 `probe` 校验视频流与时长，防止截断文件被跳过；
   - 磁盘空间守护（`diskspace.Watch`），空间不足时取消；
   - `resolveProviderMedia` → `selectDownloadQuality`（在可用清晰度里选最接近的）；
   - 起 `hlsProxy` 本地代理：重写 playlist、注入解密密钥；
   - `ffmpeg -c copy -f mp4` 输出 `.part.mp4`，读 `-progress` 计算百分比/速度/剩余时间；
   - 成功后原子重命名；失败重试（次数来自配置，默认 3）。
4. 文件名/输出目录来自配置（`-out`），默认 `downloads/`；CENC 密钥校验为 16 字节。
5. 合集合并：`media_merge.go` 调 ffmpeg 拼接多集（可选自动合并，`/api/ui/merge/auto`），并生成校验文件。

### 3.8 签名与加密

- 旧版签名（`provider_hongguo_comment_sign.go`）：`X-Gorgon` + `X-Argus` + `X-Ladon` + `X-Khronos`，基于 MD5 + RC4 风格 KSA、随机 nonce，`_rticket` 为毫秒时间戳；用于目录、详情、评论接口。
- 新版签名（`provider_hongguo_sign.go`）：`X-Gorgon` 基于 query/body 的 MD5 与时间戳、随机数，用于播放模型接口；签名对字节级内容敏感，查询参数与实际发送必须完全一致。
- 设备身份：Go 端同样随机生成 19 位 `device_id`/`iid` 并持久化；注释明确「固定设备 ID 会被判为 110001」。
- 内容密钥：`hongguoContentKey(spade_a)` 解出 16 字节 AES 密钥；`app_v2`/`web_v2` 版本暂不支持。
- Android 端当前**不再使用任何请求签名**，只保留内容密钥派生与历史向量测试。

### 3.9 缓存、并发与容错

- 缓存：剧库分页游标、详情（5 分钟 / 128 条）、搜索（5 分钟 / 64 条）、榜单（5 分钟），均带 single-flight 合并。
- 请求层：全局请求限速器（`network_limiter.go`）、代理支持（`network_proxy.go`）、20s 超时、可配置重试。
- 诊断：`diagnostics.go` 记录上游状态码、耗时、响应大小，不记录媒体地址与密钥。

---

## 四、按功能的接口对照

| 功能 | Android | Web 端 |
| --- | --- | --- |
| 首页 | `bookmall/tab/v/`（开放、无签名、`tab_type=8`） | `landpage/v/`（签名）+ 官网分类页回退；推荐同接口；榜单官网 SSR |
| 搜索 | `search/fetch-debug/v/`（开放，流式截断） | `hongguoduanju.com/api/search/result` |
| 详情 | `multi_video_detail/v/`（开放） | `novel/player/video_detail/v1/`（签名）+ 官网 `/detail` 回退 |
| 播放 | `multi_video_model/v/`（开放，`version_code=74232`）→ 本地 ffmpeg 解密 → 本地 mp4 | 三级取流（App 模型/网页 protected/第三方）→ 服务端 remux/转码 → fMP4 或 HLS |
| 下载 | 无（仅播放缓存） | 完整下载队列 + ffmpeg 落盘 + 合并 + 校验 |

---

## 五、已知限制与风险

1. **上游稳定性**：App 播放模型接口已出现过 200 + 空正文；Web 端因此准备了两级回退，Android 端没有回退，失败即无法播放。
2. **编码兼容**：仅提供 H.266（`bytevc2`）的剧集，Android 直接拒绝；Web 端只能靠服务端转码。
3. **签名时效**：Web 端签名算法依赖固定参数（SDK 版本、设备型号），上游调整后需重新对齐；Android 用开放接口规避了签名，但这些接口本身可能收紧。
4. **Android 播放体验**：整集先下载再播放，首播等待与流量开销大；Web 端为流式。
5. **搜索体积**：`fetch-debug` 响应 5–6 MB，Android 已做流式截断；若上游调整字段顺序（`data` 后移）会退化。
6. **第三方备用接口**：`djapi.999888456.xyz` 为外部服务，可用性与合规性不受控。
