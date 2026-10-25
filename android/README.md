# Duanju Player（红果短剧安卓客户端）

一个自用的局域网红果短剧播放器，配合自建 `duanju` 服务使用。手机端硬件解码 HEVC 原片，
不再让服务器的低功耗 CPU 做实时 H.264 转码。

---

## 1. 为什么会有这个 App

`duanju` 的网页端在手机浏览器上播放后续分集时会卡顿。排查结论是：源片只有 **HEVC 1080p** 一种编码，
而安卓浏览器的 MSE 普遍不支持 HEVC 解码，于是服务器只能把 HEVC 实时转成 H.264，而服务器是低功耗
Xeon（E3-1265L v3），转码余量不足，导致卡顿缓冲。

官方 App 之所以流畅，是因为它直接用手机 GPU **硬解 HEVC 原片**，从字节 CDN 拉流，还能做自适应码率。

于是做这个安卓 App，让手机代替网页端做「拉直链 → 解密 → 硬解」，服务器不再承担转码。这样既绕开了
服务器的算力瓶颈，又保留了「用自建服务统一管理和续播」的体验。

## 2. 它是如何产生的（关键取舍）

- 最初想做「App 本地 ffmpeg 解密 + 硬解」，但发现标准的安卓内置 ffmpeg 库 **ffmpeg-kit 已于 2025 年
  退役**，Maven/GitHub 上的二进制全部下架，自带 NDK 交叉编译 ffmpeg + OpenSSL 又太重。
- 因此改为**等价且仍然不转码**的方案：服务器做「AES-CTR 解密 + `-c copy` 转封装成 HLS」——这只是复制
  封装，不是重编码，CPU 可忽略；App 端用 **Media3 ExoPlayer + 硬件 HEVC 解码**直接播放。
- 服务器为此新增了两个轻量接口（详见第 5 节），App 不需要内置 ffmpeg，APK 体积很小。

## 3. 实现方式

技术栈：**Kotlin + 原生 View（XML）+ ExoPlayer/Media3 1.4**，`minSdk 24 / targetSdk 34 / compileSdk 35`。

### 3.1 页面结构

- `MainActivity`：宿主，底部三个 Tab（`BottomNavigationView`）：
  - **播放**（`LibraryFragment`）：剧库列表，搜索 + 下拉刷新 + 上滑加载更多。
  - **历史**（`HistoryFragment`）：播放历史，点击续播、长按删除。
  - **设置**（`SettingsFragment`）：服务器地址、自动连播、默认倍速。
- `PlayerActivity`：上方播放器窗口，下方选集网格。

### 3.2 播放链路

1. 进入播放页 → 从本地 `HistoryStore` 取续播记录（集数 + 秒数），无记录则从第 1 集开始。
2. 请求服务器 `POST /api/ui/playback/native/hls`，服务器解密并 `-c copy` 生成一份 VOD HLS。
3. ExoPlayer 用 `HlsMediaSource` 播放，走后端硬件 HEVC 解码。
4. 播放中每秒把进度写回本地 `SharedPreferences`（历史续播）。

### 3.3 关键实现点

- `Network.kt`：`HttpURLConnection` + `CookieManager`，封装 viewer 身份引导（`/api/ui/viewer?confirm=1`）。
- `HistoryStore.kt`：历史记录以 JSON 数组存在 `SharedPreferences`（上限 100 条）。
- `AppSettings.kt`：全局设置（服务器地址、自动连播、默认倍速）持久化。
- `PlayerActivity`：
  - 自定义 Media3 控制器 `duanju_controller.xml`（保留进度条/播放暂停/时间，额外加 倍速/静音/全屏 三个按钮）。
  - 全屏：根据视频横竖自动决定是否旋转，横屏片旋转、竖屏片填充屏幕并留安全边（`RESIZE_MODE_FIT`，不裁剪）；
    顶部导航栏显示「返回图标 + 剧名 - 第几集」，跟随控制器一起显示/隐藏。
  - 屏幕常亮（`FLAG_KEEP_SCREEN_ON`），自动连播下一集。

## 4. 目前的能力

- 剧库列表（封面缩略图、标题、集数），下拉刷新、上滑加载更多。
- 服务端搜索（搜索按钮触发，结果复用服务端官方搜索接口）。
- 播放历史：续播、长按删除。
- 全局设置：服务器地址、自动连播、默认播放倍速（1.0~4×）。
- 播放器：HEVC 硬解、进度拖拽、倍速/静音/全屏按钮、自动连播、选集中带历史续播。
- 全屏：横竖屏自适应、沉浸式、顶部导航栏自动隐藏。

## 5. 依赖的服务端接口

所有请求都指向设置里的服务器地址（默认 `http://192.168.1.2:3333`，明文 HTTP，仅限局域网）。

| 用途 | 接口 | 说明 |
| --- | --- | --- |
| 身份引导 | `GET /api/ui/viewer` → `GET /api/ui/viewer?confirm=1` | 取 viewer Cookie 与 `X-Duanju-Viewer` 头 |
| 剧库列表 | `GET /api/ui/dramas`（`?more=1`、`?revision=N`） | 加载 / 加载更多 / 轮询 |
| 搜索 | `GET /api/ui/search?q=<关键词>` | 服务端内部走官方 `/api/search/result` |
| 封面代理 | `GET /api/ui/image?url=<原图>` | 服务端抓取剧集封面 |
| 取流 | `POST /api/ui/playback/native/hls` | 入参 `{dramaId, episode}`，返回 `{url, duration, quality, title}` |
| HLS 资源 | `GET /api/ui/playback/native/hls/<token>/...` | 播放器拉取 `index.m3u8` / `init.mp4` / `*.m4s` |

> 播放类接口需要 viewer 身份；剧库 / 搜索 / 封面是公开接口。

## 6. 构建与打包

环境要求：JDK 17+、Android SDK（`compileSdk 35`）。已提交 Gradle Wrapper。

```bash
cd android
./gradlew :app:assembleDebug    # 调试包
./gradlew :app:assembleRelease  # 带 R8 压缩的发行包
```

- `release` 构建已开启 **R8 压缩 + 资源瘦身**。
- 签名使用本地 `keystore.properties`（已 gitignore，不提交仓库），缺失时回退 debug 签名以便本地构建。
- 产物：`app/build/outputs/apk/release/app-release.apk`。

安装：

```bash
adb install -r app/build/outputs/apk/release/app-release.apk
```

## 7. 目录与依赖库

```text
app/src/main/java/com/duanju/player/
  MainActivity.kt      底部导航宿主
  LibraryFragment.kt   剧库 + 搜索 + 下拉刷新/加载更多
  HistoryFragment.kt   播放历史
  SettingsFragment.kt  设置
  PlayerActivity.kt    播放器
  Network.kt / Drama.kt / DramaAdapter.kt / EpisodeAdapter.kt
  HistoryStore.kt / AppSettings.kt / BitmapLoader.kt
```

依赖库（`app/build.gradle.kts`）：

- `androidx.media3:media3-exoplayer` / `media3-ui` / `media3-exoplayer-hls`
- `androidx.appcompat` / `recyclerview` / `swiperefreshlayout`
- `com.google.android.material`