# 安卓客户端：功能、接口与实现

- 适用版本：`v0.1.23`（`versionCode 9`）
- 代码位置：`android/`（Kotlin + Media3 + FFmpegKitNext）
- 关联文档：Web 服务端接口见 [android-playback-endpoints.md](android-playback-endpoints.md)；
  上游接口逆向细节见 `hongguo-ref/docs/API.md`、`hongguo-ref/docs/CHAIN.md`

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

## 2. 接口清单

全部为手机直连，**不经自建服务器**。`device_id` / `iid` 由 App 首次运行随机生成
19 位正数并持久化在 `SharedPreferences("duanju")`，避免公开固定 ID 被风控 `110001`。

| 用途 | 方法/URL | 版本号 | 签名 | 代码入口 |
| --- | --- | --- | --- | --- |
| 首页信息流 | `GET api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/bookmall/tab/v/` | 65532 | 无 | `DirectClient.homeTab` |
| 搜索 | `GET …/reading/bookapi/search/fetch-debug/v/` | 65532 | 无 | `DirectClient.searchHits` / `searchHomeItems` |
| 详情/选集 | `GET …/reading/bookapi/video_tab/multi_video_detail/v/` | 65532 | 无 | `DirectClient.seriesDetail` / `chapters` / `detailBrief` |
| 取流（直链+密钥） | `GET …/reading/bookapi/video_tab/multi_video_model/v/` | **74232** | 无 | `DirectClient.media` |
| 分季（相关） | `GET hongguoduanju.com/player/<seriesId>/<videoId>` | — | 无 | `DirectClient.relatedSeasons` |
| 预取进度 | `HEAD <CDN 直链>` | — | 无 | `DirectClient.contentLength` |
| 视频下载 | CDN 直链（`https://`） | — | 无 | `DirectClient.decryptTo` → FFmpegKit |
| 检查更新 | `GET api.github.com/repos/lonnnnnng/duanju/releases/latest` | — | 无 | `UpdateChecker.fetchLatest` |

**版本号不能统一**：首页/搜索/详情 65532（筛选器更全、每页 9 部）；取流必须 74232，
否则只会返回 `bytevc2`（H.266）或无数据。

### 2.1 公共参数

`DirectClient.openCommonFull()` 统一拼接约 30 项：

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

公共请求头：**不设** UA / Accept / 签名。重试 3 次（600ms、1200ms 退避），
连接 15s / 读取 30s。

### 2.2 各接口的调用与解析

**首页**：`tab_type`（0=推荐）、`offset`、`session_id`、`selected_items`。
三层结构 `tab_item.cell_data[].cell_data[].video_data[]`；`offset=0` 时 `has_more` 恒为 false，
翻页用 `next_offset > offset` 判断。

**搜索**：`query`（≤30 字）+ `count=20`。响应可达 5MB，用流式读取只取 `data` 数组，
避免整包解析与 `EOFException`。`data[]` 每项只有 `id`/`type`/`display.title`（HTML）/
`display.summary`：
- `type` 只保留 `短剧`、`漫剧`，过滤 `相关搜索` 等；
- `display.title` 需剥 HTML 标签；
- `集数` 用正则从 `display.summary` 提取；
- **不含封面**：搜索列表的封面是逐条调详情接口补的（8 路并发，见 `searchHomeItems`）。

**详情/选集**：`video_series_id=<seriesId>` + `vs_id_type=1`。
`data.video_detail_data.<seriesId>.video_data`，`video_list[]` 一次给全，集号用 `vid_index`。
偶发 `code=0` 但容器内无该剧条目，`detailEntry()` 会重试 3 次并把 `code` 带进错误信息。

**取流**：`video_id=<vid>` + `video_platform=1024` + `need_all_video_definition=1`。
`data.video_model_data[<vid>].video_model` 是 JSON 字符串需二次解析：
- 跳过 `codec_type` 含 `bytevc2` 的档位（H.266 安卓无法播放）；
- 只接受 `https://` 的 `main_url`；
- 取最高 `definition`（如 `1080p`）；
- `encrypt_info.spade_a` → `HongguoSign.contentKey()` 派生 AES-128 密钥。

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

### 2.4 本地解密与缓存

取流成功后，本地执行（FFmpegKitNext）：

```
ffmpeg -y -decryption_key <hex> -i <CDN 直链> \
       -map 0:v:0 -map 0:a:0? -c copy -movflags +faststart \
       -f mp4 cache/direct/<seriesId>_<episode>.mp4.tmp
```

成功后原子重命名为 `.mp4`，再交给 ExoPlayer（`ProgressiveMediaSource`）播放。
ffmpeg 需要 `-cafile`：App 启动时把 `/apex/com.android.conscrypt/cacerts` 或
`/system/etc/security/cacerts` 合并导出为 `system-ca.pem`（ffmpeg 的 openssl 不带 Android 系统 CA）。

缓存策略：
- 命中 `.mp4` 直接跳过下载；预加载下一集在后台解密，切集时等其落盘后命中；
- 上限 `AppSettings.cacheLimitMb`（默认 2048 MB），超限按 `lastModified` LRU 淘汰，
  正在播放的集与刚写入的集不参与；命中缓存时刷新访问时间；
- 设置页显示占用并可一键清理（只删 `.mp4`/`.tmp`，保留 `system-ca.pem`）。

## 3. 排查手册（安卓）

### 3.1 一键判断「接口挂了」还是「编码不支持」

Mac 上直接跑，不需要 App、不需要签名：

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
| `失败：空响应(HTTP 200)` | 接口拒绝（风控/下线） | 先跑 3.1；开放接口也空说明域名族整体变动 |
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
./gradlew :app:testDebugUnitTest   # 含签名向量与聚合规则回归
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
material、FFmpegKitNext 9.0.2。
