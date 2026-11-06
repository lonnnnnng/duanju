# 安卓客户端：功能、接口与实现

- 接口与配方章节更新于：2026-11-07（工作区实现，尚未发布）；早期功能清单始于 v0.1.23
- 代码位置：`android/`（Kotlin + Media3 + FFmpegKitNext）
- 关联文档：Web 服务端接口见 [android-playback-endpoints.md](android-playback-endpoints.md)；
  上游变更史与规律见 [2026-11-06-api-change-analysis.md](2026-11-06-api-change-analysis.md)；
  预取限流问题见 [2026-11-06-prefetch-rate-limit.md](2026-11-06-prefetch-rate-limit.md)；
  首页真实请求、完整筛选枚举与原始入参/出参见 [2026-11-06-home-api-verification.md](2026-11-06-home-api-verification.md)；
  全页面浅深主题与交互验收见 [2026-11-07-android-ui-polish.md](2026-11-07-android-ui-polish.md)；
  逆向细节见 `hongguo-ref/docs/API.md`、`hongguo-ref/docs/CHAIN.md`

本文只描述安卓端。Web/服务器的取流方式、签名参数与排查手册不在这里重复。

---

## 1. 能力清单

| 模块 | 能力 |
| --- | --- |
| 首页 | 官方推荐流分页、下拉刷新、上滑加载更多、断连重试、两级筛选（推荐/榜单/新剧/题材） |
| 搜索 | 关键字搜索、结果列表、搜索历史（本地）、封面补齐 |
| 详情页 | 封面/标签/集数/热度/简介展开、选集分页（30 集一组）、**相关（同剧各季）** |
| 播放器 | HEVC 硬解、1×–4× 倍速、静音、全屏（横屏旋转/竖屏沉浸）、进度拖拽、屏幕常亮 |
| 连播 | 自动连播开关、**预加载下一集**（可关，后台解密落盘，切集直接命中） |
| 状态行 | 第一行「第N集 进度 · 清晰度 · 文件大小」，第二行「下一集预取百分比 / 已预载 · 清晰度 · 大小 · 耗时」 |
| 缓存 | 按剧+集缓存解密后的 MP4；容量上限可配置（默认 2 GB，预设 512MB–16GB + 自定义 GB），超限 LRU 淘汰；设置页可看占用、一键清理 |
| 相关分季 | 详情页/播放页「选集」旁 Tab，列出同剧各季（封面/名称/集数），点击进入对应详情 |
| 其它 | 播放历史续播与长按删除、手动检查更新（GitHub Release 直下 APK）、公共顶栏组件 `TopBar` |

布局要点：
- 竖屏播放器窗口与下方内容 **5:5** 分栏；横屏视频按画面比例收紧，尽量不留黑边。
- 播放页顶栏为「返回 + 剧名」，全屏时隐藏，改用播放器内悬浮返回栏。
- 控制器不自动弹出，只有触摸视频区域才显示（5 秒后隐藏）。
- 底部控制器为 48dp 单行：左侧为当前时间、进度条与总时长，右侧为倍速、静音、全屏；背景、进度条与按钮同时显隐，不再保留两秒进度条。
- 全屏悬浮标题使用播放器专用白色，不随浅色主题切成深色文字，保证黑色遮罩上的可读性。
- 历史卡片进度为「当前集数 / 全剧总集数」（第 10 集 / 共 100 集显示 10%），与单集播放秒数无关；未知总集数时不显示进度条。续播仍使用原有的集号与单集秒数。
- 首页分页加载提示固定在底部导航上方；分页失败保留已有内容和游标，点击提示重试，滚动不再反复触发失败请求。
- 首页剧集使用封面和信息合为一体的圆角描边卡片；顶部胶囊具有独立底色和描边，浅色模式下也能区分按钮轮廓。
- 首页筛选、详情/播放页分组和当前集共用 `choice_*` 选择配色；详情播放按钮使用同一配色，并显式指定 Material 底色/描边。详情/普通播放返回栏与系统状态栏共用页面底色。
- 全屏系统返回先退出全屏；播放失败停止加载并在视频内提供“重试本集”。选集分组点击始终使用实时当前集，小屏/大字体适当减少列数。
- 搜索失败和关联剧请求失败有独立重试入口；搜索重建后恢复已提交关键词。首页刷新失败保留旧分页游标与现有内容，可点击重新刷新。
- 设置自定义缓存上限的最小值为 0.125GB（128MB）；非法输入保留弹框并在输入框下方提示，不改原值。更新检查防重复，隐藏或销毁设置页后不弹出迟到结果。

## 2. 接口清单

全部为手机直连，**不经自建服务器**。`device_id` / `iid` 由 App 首次运行随机生成
19 位正数并持久化在 `SharedPreferences("duanju")`，避免公开固定 ID 被风控 `110001`。

| 用途 | 方法/URL | 版本号 | 签名 | 代码入口 |
| --- | --- | --- | --- | --- |
| 首页信息流 | `GET api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/bookmall/tab/v/` | 65532 | 无 | `DirectClient.homeTab` |
| 搜索 | `GET …/reading/bookapi/search/fetch-debug/v/` | 65532 | 无 | `DirectClient.searchHits` / `searchHomeItems` |
| 详情/选集 | `GET …/reading/bookapi/video_tab/multi_video_detail/v/` | 65532 | **旧版签名** | `DirectClient.seriesDetail` / `chapters` / `detailBrief` |
| 取流（直链+密钥） | `GET …/reading/bookapi/video_tab/multi_video_model/v/` | **74232** | **旧版签名** | `DirectClient.media` |
| 分季（相关） | `GET hongguoduanju.com/player/<seriesId>/<videoId>` | — | 无 | `DirectClient.relatedSeasons` |
| 预取进度 | `HEAD <CDN 直链>` | — | 无 | `DirectClient.contentLength` |
| 视频下载 | CDN 直链（`https://`） | — | 无 | `DirectClient.decryptTo` → FFmpegKit |
| 检查更新 | `GET api.github.com/repos/lonnnnnng/duanju/releases/latest` | — | 无 | `UpdateChecker.fetchLatest`（优先走 `gh-proxy.com`，失败再直连） |

**版本号不能统一**：当前首页/搜索/详情使用 65532（本轮首页单项筛选返回 6–9 部，并非固定每页 9 部）；取流使用 74232，
否则只会返回 `bytevc2`（H.266）或无数据。

### 2.1 公共参数

`RecipeHttp.commonParams()` 统一拼接约 30 项：

```
iid & device_id & ac=wifi & channel=hongguo_baidu_pz_kaiping0105_android
& aid=8662 & app_name=novelread
& version_code=<65532|74232> & version_name=<6.5.5.32|7.4.2.32>
& manifest_version_code=<> & update_version_code=<> & pv_player=<>
& device_platform=android & os=android & ssmix=a
& device_type=Redmi+Note+8+Pro & device_brand=Redmi & language=zh
& os_api=36 & os_version=16 & resolution=1080*2198 & dpi=440
& gender=2 & need_personal_recommend=1 & player_so_load=1
& is_android_pad_screen=0 & host_abi=arm64-v8a & dragon_device_type=phone
& rom_version=BP4A.251205.006+release-keys & compliance_status=0
& cdid=1f1d14b3-8c25-4733-9f6b-2fdc01640081
```

普通请求头由配方决定；`legacy` 档补齐签名头及模型 UA。
每档发送次数与退避由各端点配方决定，连接超时 15s、读取超时 30s；详情没有额外一层固定重试。

### 2.2 各接口的调用与解析

**首页**：`tab_type=8`（短剧信息流）、`offset`、可选 `session_id`、可选 `selected_items`。
首次/切筛选时 `offset=0`，省略空 session 和筛选值；翻页沿用返回的 session 与 `next_offset`。
三层结构 `tab_item.cell_data[].cell_data[].video_data[]`；本轮首屏 `has_more=false` 但下一页仍有数据，
客户端用 `next_offset > offset` 判断。当前配方没有发送每页条数参数。

筛选取响应的 `cell_selector`，不维护静态分类枚举。2026-11-06 正式取证返回 10 个快捷项与
综合/时代背景/主题情节/角色设定 4 个内组，去重 63 个 ID，逐项请求均被识别。
当前页面完整覆盖这些 ID：顶部快捷行可滚动，右侧筛选入口固定；面板包含快捷分类与全部内组，
不再按组名隐藏“综合”。入口显示已选名称，重开面板定位具体项，可清除回到推荐。
只有内组、没有快捷项时入口仍可用，多份 selector 合并同组并按 ID 去重。
顶部按钮视觉高度约 32dp、面板选项约 36dp，触摸区域保留 48dp，避免点击区域扩大造成视觉膨胀。
上游声明 `global_single=true`，
因此继续全局单选；逗号组合会回显两个选择，但本轮没有证明返回内容是交集。
完整请求和 63 项表格见 [首页实测报告](2026-11-06-home-api-verification.md)。

配方 v6 只在 `home.headers` 设置 App 格式 `User-Agent`、`Accept` 与 `Accept-Language`。
UA 的包名/版本/设备字段与首页公共参数一致，但不冒称逐字复制官方抓包或模拟 Cronet。
搜索、详情、取流的配方与签名 UA 未变；68 次新首页请求与生产 Kotlin 联网回归均通过。

**搜索**：`query`（≤30 字）+ `count=20`。响应可达 5MB，用流式读取只取 `data` 数组，
避免整包解析与 `EOFException`。`data[]` 每项只有 `id`/`type`/`display.title`（HTML）/
`display.summary`：
- `type` 只保留 `短剧`、`漫剧`，过滤 `相关搜索` 等；
- `display.title` 需剥 HTML 标签；
- `集数` 用正则从 `display.summary` 提取；
- **不含封面**：搜索列表的封面是逐条调详情接口补的（8 路并发，见 `searchHomeItems`）。

**详情/选集**：`video_series_id=<seriesId>` + `vs_id_type=1`。
`data.video_detail_data.<seriesId>.video_data`，`video_list[]` 一次给全，集号用 `vid_index`。
`code=0` 但容器内无该剧条目时，按 `missingData` 和该端点的配方预算处理；不会拿其它剧的首项替代。

**取流**：`video_id=<vid>` + `video_platform=1024` + `need_all_video_definition=1`。
`data.video_model_data[<vid>].video_model` 是 JSON 字符串需二次解析：
- 跳过 `codec_type` 含 `bytevc2` 的档位（H.266 安卓无法播放）；
- 只接受 `https://` 的 `main_url`；
- 取最高 `definition`（如 `1080p`）；
- `encrypt_info.spade_a` → `HongguoSign.contentKey()` 派生 AES-128 密钥。

### 2.2.1 签名分层（2026-11-06 变更）

以下是历史故障与本次抽样观察，不能保证所有网络、设备身份与剧集永久呈现相同行为。
2026-11-06 当前配方的联网验收及边界见 [实测记录](2026-11-06-recipe-hardening.md#6-当前接口实测2026-11-06)。
本次同一网络下接口表现分为两档：

| 档位 | 接口 | 未签名表现 |
| --- | --- | --- |
| 开放 | `bookmall/tab`、`search/fetch-debug` | 正常返回 |
| 受保护 | `multi_video_detail/v/`、`multi_video_model/v/` | `HTTP 200` + **0 字节正文** |

当前配方对受保护接口优先带旧版全套头（`HongguoSign.legacyHeaders(query)`）：

```
X-Gorgon / X-Argus / X-Ladon / X-Khronos / X-SS-Req-Ticket / X-SS-STUB
User-Agent: com.phoenix.read/73532 (...)
```

- **签名用的 query 必须与实际发送的那串完全一致**（顺序、`+` 号都不许变）；
  不一致会被当成无效签名，继续返回空正文。
- `X-Khronos` 是秒级时间戳，`X-SS-Req-Ticket` 是毫秒级；`X-SS-STUB` 对 GET 传空串。
- 2026-11-06 之前这两个接口是开放的，之后被划入受保护档；表现为
  「首页/搜索正常，但详情空响应、播放取不到流、搜索封面全空（封面靠详情补）」。
- 排查口径：**0 字节是响应症状，不能单凭它确定风控原因**；`101116` 是上游业务错误码，
  既可能短暂恢复，也有历史持续拒绝案例。当前配方只做有限重试，持续失败仍需重新核验。
- 已知上游抖动：`multi_video_model` 会出现间歇 0 字节（实测有效签名下 3 次中 2 次），
  配方把它配置为 4 次重试、退避 1/2/3 秒。

### 2.2.2 接口配方（L1）+ 签名原语（L2）

当前工作区中，四个官方接口由 `android/app/src/main/assets/recipes/hongguo.json`
描述，`RecipeHttp` 执行；实际发布版本以 Release 为准：

```json
"detail": {
  "host": "open",
  "path": "/reading/bookapi/video_tab/multi_video_detail/v/",
  "versionCode": "65532",
  "sign": ["legacy", "none"],
  "params": [["video_series_id", "$seriesId"], ["vs_id_type", "1"]],
  "retry": { "attempts": 3, "backoffMs": [600, 1200], "on": ["network", "http5xx", "empty"] },
  "extract": "data.video_detail_data"
}
```

- **L1 配方**：路径、版本号、参数、签名档位顺序、重试判据、取值路径都是数据。
  已支持能力范围内的变更可改 JSON，验签与兼容性检查通过且版本更高时生效。行内字段映射、新协议与新签名仍由宿主实现。
- **L2 原语**：`none`（不签名）、`legacy`（X-Gorgon/X-Argus/X-Ladon/X-Khronos/
  X-SS-Req-Ticket/X-SS-STUB）。配方按名字引用，不在 JSON 里写算法。
- **档位降级**：`sign` 是有序数组；返回 `200 + 0 字节` 时先在**同档退避重试**
  （区分瞬时抖动与网关拦截），仍为空再切下一档。例如 `["legacy","none"]`
  表示先签名，被网关拦就退回免签，正好覆盖上游来回切的形态。
- **参数规则**：`$var` 模板替换；`$var` 为空时整项省略（首页首次加载没有 `session_id`）。
- **远程更新**（ECDSA 验签 + 版本单调）：
  - 源顺序：**gh-proxy 加速** → GitHub Release `recipe` → jsDelivr（`@main`）→ raw 直链。
    手机网络通常直连不了 GitHub，所以加速地址放第一位；
  - 只接受内置 P-256 公钥验证通过的 `recipe.json`，且 `version` 必须大于当前版本；
  - 比较全部有效源并采用最高版本，旧镜像不会遮住新源；网络/畸形签名/配置错误保留现有配方；
  - 保存时在同一锁内检查版本并原子落盘，拒绝并发降级、缺端点、未知方法/签名/变量和超限重试；
  - 启动时后台检查一次，设置页「关于 → 接口配方」可手动重试并查看版本/来源。
- **发布流程**（无需发 APK）：
  1. 改 `android/app/src/main/assets/recipes/hongguo.json`，提升 `version`；
  2. `tools/publish-recipe.sh` 生成 `recipes/recipe.json` + `.sig`（私钥在
     `android/keystore/recipe/`，已 gitignore；CI 用 Secret `RECIPE_SIGNING_KEY`）；
  3. 提交推送后，仅在明确要求发布配方时，手动运行 `.github/workflows/recipe.yml`，校验签名并更新 `recipe` 预发布资产；普通推送不触发该流水线；
  4. 已安装的 App 下次检查更新时可拉取。注意 `recipes/recipe.json` 和签名同时提交后，raw / jsDelivr 的 `main` 源也能分发该配方；手动触发规则只控制 Release 资产更新，并不隔离这些源。

改动 Recipe 引擎或原语后必须跑：`./gradlew :app:testDebugUnitTest`（`RecipeTest` 覆盖
解析、URL 拼装、空参省略、取值路径）。

本轮修复后，搜索、详情和取流共用重试状态机；`empty`、`nonJson`、`missingData`、
`network`、`http5xx`、`http429` 和指定 `code:数字` 可作为条件，每档最多 5 次。
`method` 支持 GET/POST，POST 可配置 JSON 对象 `body`，逐个字符串值替换变量；
`headers` 可配置普通请求头。带 body 的签名尚未暴露，配置检查会拒绝该组合。
`extract` 仅定位已有业务模型，不能替换所有行内字段解析。
完整边界、测试与动态类加载评估见 [配方修复与动态扩展评估](2026-11-06-recipe-hardening.md)。

### 2.3 分季（「相关」Tab）

详情接口没有任何季/合集字段，每一季是独立 `series_id`；官方 `plan/v` 需要业务上下文
（实测恒 `101116`）。安卓改用站点剧集页，**一次请求拿全并带封面**：

```
GET https://hongguoduanju.com/player/<seriesId>/<videoId>
User-Agent: <桌面浏览器 UA>
Referer:    https://hongguoduanju.com/
```

页面把数据放在 `data-fn-args='[...]'` 属性，第三项即 `videoList`：

```json
{"series_id":"...","series_name":"…第二季","series_cover":"https://…","episode_cnt":111}
```

- 页面 `videoList` **不含当前这一季**，需从同页 `seriesDetail` 补一条（含 `series_cover`）；
- 属性值是 HTML 转义的，取到后还原 `&quot;` 等实体再 `JSONArray(text)`；
- 聚合规则在 `core/SeriesAggregator.kt`：剥离「第X季」后缀（兼容 `（第一季）`、`第六季！`、
  `第15季`、中文数字）得基名 → 按基名分组 → 区分 product line（短剧 ≠ 漫剧，预约剧壳跟随可播季）
  → 按季号升序；`episode_cnt == 0` 或 `type == 预约剧壳` 视为未上线；
- 站点页不可达时退回「搜索 + 基名聚合」，此时只有当前剧有封面。

### 2.4 剧集发布时间（已调查，暂不实现）

2026-10-31 调查结论：**搜索/详情/相关三个页面想显示「发布时间」，只有一个低成本来源。**

| 页面 | 数据源 | 发布时间 |
| --- | --- | --- |
| 搜索结果列表 | `search/fetch-debug` | 无。每项仅 `id`/`type`/`display.title`/`display.summary`；响应全文里虽有 `last_publish_time` 字样，但只出现在 `debug_info` 调试日志中，不是数据字段 |
| 详情页 | `multi_video_detail` | 无。`video_data` 与单集字段均无时间字段；`record_info.record_number` 是备案号（含年份，非日期） |
| 相关列表 | 站点页 `data-fn-args` 的 `videoList` | 条目内无时间字段 |

**可行方案**：站点剧集页头部有 `<script type="application/ld+json">`（`@type: VideoObject`）块，
其 `uploadDate` 实测即该剧的发布时间——同一部剧任意一集取到的值相同，不同剧不同：

```json
{"@type":"VideoObject","name":"总裁是我前女友 第1集","uploadDate":"2026-09-23T16:42:00.000Z", ...}
```

- 《退役归来，这个保镖不好惹》→ `2026-10-22T16:26:01Z`；《总裁是我前女友》→ `2026-09-23T16:42:00Z`。
- 实现零成本：相关 Tab 的数据本来就来自站点剧集页，同一响应里顺带正则
  `"uploadDate":"([^"]+)"` 即可；详情页如需展示，可只在相关列表标注（当前季自然带上），
  或详情页静默多发一次站点页请求。
- **搜索列表不建议**：要显示只能逐条额外抓站点页（每条一次请求），代价不成比例。

决定：暂不实现，保留方案备查。

### 2.5 本地解密与缓存

取流成功后，本地执行（FFmpegKitNext）：

```
ffmpeg -y -decryption_key <hex> -i <CDN 直链> \
       -map 0:v:0 -map 0:a:0? -c copy -movflags +faststart \
       -f mp4 cache/direct/<seriesId>_<episode>.mp4.<taskId>.tmp
```

成功后原子重命名为 `.mp4`，再交给 ExoPlayer（`ProgressiveMediaSource`）播放。
ffmpeg 需要 `-cafile`：App 启动时把 `/apex/com.android.conscrypt/cacerts` 或
`/system/etc/security/cacerts` 合并导出为 `system-ca.pem`（ffmpeg 的 openssl 不带 Android 系统 CA）。

缓存策略：
- 命中非空 `.mp4` 直接本地播放，不再请求详情或取流，断网时也可从历史重开已缓存分集；
- 预加载下一集在后台解密；切入正在预取的集时等待其结束，不再 60 秒超时后重复启动下载；
- 请求内按配方有限重试；预取临时故障在停留本集时每 20 秒再试，最多追加 3 轮，
  不再叠加整任务立即 3 次重试。空正文无法单凭现象确定是签名失配还是限流；
- 预取复用播放本集时已拿到的分集列表，不再重复请求详情，少一次被限流的机会；
- 每次下载使用独立临时文件；切集、退出或超时会显式取消 native 会话，只有完整成功且未取消的任务才能发布成品；
- 上限 `AppSettings.cacheLimitMb`（默认 2048 MB），超限按 `lastModified` LRU 淘汰，
  正在准备/播放的集与刚写入的集不参与；命中缓存时刷新访问时间；
- 设置页显示占用并可一键清理（只删未被使用的 `.mp4`/`.tmp`，保留 `system-ca.pem`）。

播放状态与页面回调：
- 切集前保存并停止旧媒体，新媒体 READY 后才接受新集的进度；准备中退出不覆盖原续播进度；
- 离开播放页暂停，回前台按用户原有播放/暂停意图恢复，后台不自动起播或连播；
- 搜索关键词改变时取消旧请求，迟到的成功和失败回复都不得改写当前界面；
- 详情与播放页的相关结果可以缓存，但显示哪个 Tab 始终服从当前选择。

## 3. 排查手册（安卓）

### 3.1 一键判断接口档位与编码

下面的脚本**故意不带签名**，用来区分两档：

- 返回 JSON → 该接口仍是开放档，问题在别处（参数/编码/网络）；
- 返回 `200` + 0 字节 → 该接口已转为受保护档，必须带 `legacyHeaders` 签名，见 §2.2.1。

Mac 上直接跑：

```bash
python3 - <<'PY'
import urllib.request, json
H="https://api5-normal-sinfonlinec.fqnovel.com"
C="aid=8662&device_id=1234567890123456&version_code=74232&update_version_code=74232&device_platform=android"
sid="7688352674999176254"
d=json.loads(urllib.request.urlopen(f"{H}/reading/bookapi/video_tab/multi_video_detail/v/?{C}&video_series_id={sid}&vs_id_type=1").read())
vd=list(d["data"]["video_detail_data"].values())[0]["video_data"]
vid=vd["video_list"][0]["vid"]
d=json.loads(urllib.request.urlopen(f"{H}/reading/bookapi/video_tab/multi_video_model/v/?{C}&video_id={vid}&video_platform=1024&need_all_video_definition=1").read())
m=json.loads(d["data"]["video_model_data"][vid]["video_model"])
print(sorted({((r.get("video_meta") or {}).get("definition"),(r.get("video_meta") or {}).get("codec_type")) for r in m["video_list"]}))
PY
```

### 3.2 症状对照

| 现象 | 含义 | 处理 |
| --- | --- | --- |
| `失败：空响应(HTTP 200)` | 网关拒绝：受保护档缺签名、签名 query 不一致，或上游间歇限流 | 先跑 3.1 判断档位；受保护接口必须带 `legacyHeaders`；已配 4 次退避重试，仍失败稍后再试 |
| `失败：该剧仅提供 H.266(bytevc2) 编码` | 没有 bytevc1/h264 | 检查取流 `version_code` 是否为 74232 |
| `失败：取流失败(101116)` | 业务层 SERVICE_ERROR | 换设备 id 重试；持续则接口策略变更 |
| `失败：取流失败(100103)` | PARAM_INVALID | `video_platform` 必须是 1024 |
| `失败：详情无数据(code=0)` | 上游偶发降级 | 已内置 3 次重试；持续出现说明接口形态变化 |
| 有 URL 但 ffmpeg 报解密失败 | `spade_a` 派生问题 | 跑 `ContentKeyVectorTest` 校验派生算法 |
| 有 URL、解密成功但黑屏 | H.266 档位 | 确认选档仍跳过 `bytevc2` |

### 3.3 真机验证命令

```bash
cd android
./gradlew :app:testDebugUnitTest :app:assembleRelease
adb install -r app/build/outputs/apk/release/app-release.apk
adb logcat -d -s 'PlayerActivity:*' 'DirectClient:*' | tail -20
```

日志关键行：`第N集解析Xms 下载Yms`、`第N集命中缓存 ...B`、`预取第N集完成`、
`缓存超限，清理 ...`、`详情重试n/3`。

## 4. 构建与发版

```bash
cd android
./gradlew :app:testDebugUnitTest :app:lintDebug :app:assembleDebug
./gradlew :app:assembleRelease     # R8 + 资源裁剪 + 本地签名
```

- FFmpegKitNext AAR 随仓库提供（`android/libs/ffmpeg-kit-next-maven/`，arm64-v8a，源码编译）。
- native 库以 deflate 压缩存储（`packaging { jniLibs { useLegacyPackaging = true } }`），
  APK 约 12.7 MB；安装时解压。
- 签名 keystore 在 `android/keystore/`（gitignore，丢失将无法覆盖升级）。
- 发版：`.github/workflows/release.yml`；仅安卓改动时可传 `android_only=true` 复用上一版本的
  五个平台程序，只重建 APK。流程见 [releasing.md](releasing.md)。

## 5. 依赖

Media3 1.4（`media3-exoplayer` / `media3-ui`）、appcompat、recyclerview、swiperefreshlayout、
material、FFmpegKitNext 9.0.2、Coil 2.7.0（按控件尺寸解码、取消复用控件的旧请求）。

启用 `desugar_jdk_libs:2.1.5`，为最低支持的 API 24/25 提供播放密钥派生所需的
`java.util.Base64`。JVM 回归覆盖切集进度归属、迟到回调、取消/超时、独立临时文件、
并发成品发布、离线缓存与播放文件保护；Robolectric 覆盖搜索清空/改词、相关 Tab 迟到回复、
footer 结构通知和空封面复用。native 实际停止、视频播放及真机 UI 仍需设备验收。
