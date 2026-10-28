# 短剧（Duanju Player 安卓客户端）

一个**完全不依赖自建服务器**的红果短剧安卓播放器：手机直接调官方无签名开放接口、
直连 CDN 下载、本地 CENC 解密、硬件解码 HEVC。出门在 4G/5G 下即可使用。

---

## 1. 为什么是"零服务器"

早期方案是「服务器解密 + 拷贝封装 HLS + 手机播放」，解决了服务器实时转码卡顿的问题，
但视频数据仍要经过家里服务器（流量 + 临时磁盘 + 出门不可用）。

本版本把整条链路搬进手机：

1. **解析**：手机直接调用红果官方**无签名**开放接口（`bookmall/tab` / `search/fetch-debug` /
   `multi_video_detail` / `multi_video_model`），只需公共参数与随机设备身份。
   历史签名实现（X-Gorgon / X-Argus / X-Ladon / SM3）仍保留在 `HongguoSign.kt` 供回归测试。
2. **取流**：手机直连字节 CDN 下载原片（HEVC 1080p）。
3. **解密**：内置 FFmpegKitNext（源码自编译）执行 `-c copy -decryption_key`，
   只解密、不转码，输出本地 MP4。
4. **播放**：ExoPlayer + 硬件 HEVC 解码。

服务器（duanju Go 服务）从此只作为可选的家庭 Emby 源，与本 App 无任何关联。

## 2. 它是怎么来的（关键节点）

- 网页端卡顿 → 排查出根因：服务器低功耗 CPU 实时 HEVC→H.264 转码扛不住。
- 想做手机本地解密 → ffmpeg-kit 于 2025-01 退役、二进制全下架。
- 2026-07 原作者发布 **FFmpegKitNext**（source-only）→ 本地编译出 AAR。
- 签名算法从服务器 Go 实现 1:1 移植到 Kotlin，用**固定向量 + 真实 spade_a 样本**
  双向验证字节级一致；随后官方开放接口可用，安卓端改用无签名接口，签名仅留作回归与兜底参考。
- 最终移除服务器依赖：剧库、搜索、封面、解析、解密全部手机直连。

> 功能清单、接口调用方式与排查手册的完整版本见 [docs/android-app.md](../docs/android-app.md)；
> 本文是应用说明与构建入口。

## 3. 能力（v0.1.23）

- **首页**：官方推荐流分页（下拉刷新、上滑加载更多、断连重试），两级筛选面板（推荐/榜单/新剧/题材）。
- **搜索**：`search/fetch-debug` 关键字搜索，列表化展示（封面由逐条详情补齐），本地搜索历史。
- **详情页**：封面/标签/集数/热度/简介（可展开）、选集分页（30 集一组）、**相关**（同剧各季）。
- **播放器**：HEVC 硬解、倍速 1×–4×、静音、全屏（横屏旋转/竖屏沉浸）、进度拖拽、屏幕常亮。
  - 竖屏下播放器窗口与下方内容 **5:5** 分栏；横屏视频按画面比例收紧，尽量不留黑边。
  - 自动连播、**预加载下一集**（可关）：后台把下一集解密到本地，切集直接命中。
  - 状态行两行：当前集「进度 · 清晰度 · 文件大小」/ 下一集「预取百分比或已预载 · 清晰度 · 大小 · 耗时」。
- **缓存管理**：按剧+集缓存解密后的 MP4；**容量上限可配置**（默认 2 GB，预设 512MB–16GB + 自定义），
  超限按最近访问时间 LRU 淘汰，正在播放/刚写入的集不参与；设置页可查看占用并一键清理。
- **相关（分季）**：详情页与播放页「选集」旁新增 Tab，列出同剧各季（封面/名称/集数），点击进入对应详情。
- **其它**：播放历史续播（集数+秒数、长按删除）、手动检查更新（GitHub Release，直接下 APK）、公共顶栏组件。
- **包体**：native 库压缩存储（`useLegacyPackaging`）+ R8/资源裁剪，APK 约 12.7 MB。

## 4. 直连使用的接口

全部无签名、手机直连，不经自建服务器：首页 `bookmall/tab`、搜索 `search/fetch-debug`、
详情 `multi_video_detail`（均 `version_code=65532`），取流 `multi_video_model`（必须 `74232`），
分季抓 `hongguoduanju.com/player/<seriesId>/<videoId>` 页面，检查更新读 GitHub Release。

参数、解析规则、分季聚合与排查手册统一维护在
**[docs/android-app.md](../docs/android-app.md)**（本文件不再重复，避免两处漂移）。

## 5. 构建

```bash
cd android
./gradlew :app:testDebugUnitTest   # 含签名向量回归（防止算法被无意改坏）
./gradlew :app:assembleRelease     # R8 压缩 + 本地签名
```

- FFmpegKitNext AAR 已随仓库提供（`android/libs/ffmpeg-kit-next-maven/`，arm64-v8a，源码编译）。
- 发版：`.github/workflows/release.yml`，仅安卓改动时可加 `android_only=true` 复用上一版本 Web 程序；见 `docs/releasing.md`。
- 签名 keystore 在 `android/keystore/`（gitignore，丢失将无法覆盖升级，请自行备份）。

## 6. 维护须知（上游改签名时）

所有签名与密钥逻辑集中在两个文件，上游变更只需改这里并发新版 App：

- `app/src/main/java/com/duanju/player/core/HongguoSign.kt`（签名 + spade_a 派生）
- `app/src/main/java/com/duanju/player/core/DirectClient.kt`（接口调用 + 参数）

回归保障：`LegacySignVectorTest` / `HongguoSignVectorTest` / `ContentKeyVectorTest`
对固定向量断言。改算法前先在服务器侧（Go 版仍可用）生成新向量替换。

依赖库：Media3 1.4（exoplayer/ui）、appcompat、recyclerview、swiperefreshlayout、
material、FFmpegKitNext 9.0.2（ffmpeg 的 .so 以 deflate 压缩存储，安装时解压）。