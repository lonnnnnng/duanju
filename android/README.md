# 短剧（Duanju Player 安卓客户端）

一个**完全不依赖自建服务器**的红果短剧安卓播放器：手机直接调官方接口（内置签名）、
直连 CDN 下载、本地 CENC 解密、硬件解码 HEVC。出门在 4G/5G 下即可使用。

---

## 1. 为什么是"零服务器"

早期方案是「服务器解密 + 拷贝封装 HLS + 手机播放」，解决了服务器实时转码卡顿的问题，
但视频数据仍要经过家里服务器（流量 + 临时磁盘 + 出门不可用）。

本版本把整条链路搬进手机：

1. **解析**：手机直接调用红果官方 App 接口（`video_detail` / `multi_video_model`），
   签名算法（新版 X-Gorgon、旧版 X-Gorgon/X-Argus/X-Ladon、SM3）全部内置。
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
  双向验证字节级一致（移植过程中靠向量测试抓出 3 处位运算/字节语义 bug）。
- 最终移除服务器依赖：剧库、搜索、封面、解析、解密全部手机直连。

## 3. 能力

- 剧库：官方分类接口分页加载（下拉刷新、上滑加载更多、偶发断连自动重试）。
- 搜索：官网搜索接口（搜索按钮/软键盘搜索键触发）。
- 播放历史：本地续播（集数+秒数）、长按删除。
- 播放器：HEVC 硬解、倍速(1×–4×)/静音/全屏按钮、自动连播、进度拖拽、屏幕常亮。
- 全屏：横/竖屏视频自适应（横屏旋转、竖屏填充+安全边）、沉浸式、
  顶部「返回图标 + 剧名 - 第几集」导航栏、与控制器同步显示/隐藏。
- 本地解密缓存：按剧+集缓存 MP4，二次进入秒开；临时文件原子写入防损坏。

## 4. 直连使用的接口

| 用途 | 接口 | 签名 |
| --- | --- | --- |
| 剧库列表 | `POST api5-normal-sinfonlineb.fqnovel.com/reading/distribution/category/landpage/v/` | 旧版全套 |
| 分集列表 | `POST …/novel/player/video_detail/v1/` | 旧版全套 |
| 取流(直链+spade_a) | `POST …/novel/player/multi_video_model/v1/` | 新版 X-Gorgon |
| 搜索 | `GET hongguoduanju.com/api/search/result?query=` | 无 |
| 封面 | CDN 图片直链 | 无 |
| 视频下载 | CDN 直链（本地 ffmpeg 解密） | 无 |

设备身份由 App 首次运行随机生成并持久化（避免公开固定 ID 被风控 110001）。

## 5. 构建

```bash
cd android
./gradlew :app:testDebugUnitTest   # 含签名向量回归（防止算法被无意改坏）
./gradlew :app:assembleRelease     # R8 压缩 + 本地签名
```

- FFmpegKitNext AAR 已随仓库提供（`android/libs/ffmpeg-kit-next-maven/`，arm64-v8a，源码编译）。
- 签名 keystore 在 `android/keystore/`（gitignore，丢失将无法覆盖升级，请自行备份）。

## 6. 维护须知（上游改签名时）

所有签名与密钥逻辑集中在两个文件，上游变更只需改这里并发新版 App：

- `app/src/main/java/com/duanju/player/core/HongguoSign.kt`（签名 + spade_a 派生）
- `app/src/main/java/com/duanju/player/core/DirectClient.kt`（接口调用 + 参数）

回归保障：`LegacySignVectorTest` / `HongguoSignVectorTest` / `ContentKeyVectorTest`
对固定向量断言。改算法前先在服务器侧（Go 版仍可用）生成新向量替换。

依赖库：Media3 1.4（exoplayer/ui/hls）、appcompat、recyclerview、
swiperefreshlayout、material、FFmpegKitNext 9.0.2。