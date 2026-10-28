# 安卓「短剧」App：实现方案、踩坑与解决记录


> 现状以 [android-app.md](android-app.md) 为准（功能、接口清单与排查手册）；本文是当时的踩坑记录。

- 日期：2026-10-26
- 版本基线：android-v0.2.2（纯直连版）
- 相关提交：4e44338 → 5a58a39 → 23f309b → 1651a6d → 1b6fb0f → 39b97f7 → e36baa5

---

## 1. 背景与演进路径

### 1.1 问题的起点

网页端在手机浏览器播放后续分集时持续卡顿。排查结论：

- 源片只有 HEVC(bytevc1) 1080p 一种编码，没有 H.264 版本；
- 源片是 CENC(AES-CTR) 加密，浏览器拿不到密钥，服务器必须解密；
- 安卓浏览器 MSE 普遍不支持 HEVC 解码 → 服务器只能实时 HEVC→H.264 转码；
- 家用服务器是低功耗 Xeon（E3-1265L v3），实测 `-threads 2` 转码仅约 2.3× 实时，余量不足 → 卡顿。

官方 App 流畅的原因：手机 GPU 硬解 HEVC 原片 + 直连 CDN + 自适应码率。

### 1.2 方案演进（三阶段）

| 阶段 | 方案 | 结果 |
| --- | --- | --- |
| v0.1.x 网页/服务器 | 服务器解密 + 实时转码 H.264 | 手机能看但卡顿；服务器 CPU 吃紧 |
| v0.1.19~v0.1.20 透传 | 服务器解密 + `-c copy` 渐进式 HLS，手机 ExoPlayer 硬解 | 播放流畅、切集 <1s，但视频数据仍全部经过服务器（流量 + /tmp 临时盘 + 出门不可用） |
| v0.2.x 纯直连（当前） | 手机内置官方签名直连解析 + CDN 直链 + FFmpegKitNext 本地解密 + 硬解 | 服务器零关联，4G/5G 出门可用 |

中途的关键决策：用户明确要求**出门可用、完全移除服务器依赖**，因此 v0.2.1 起不再保留双模式，全部接口直连。

## 2. 当前实现方案（v0.2.2）

### 2.1 架构

```text
官方分类接口(landpage) ──┐
官网搜索接口 ────────────┤
官方详情接口(video_detail) ──→ CDN 直链 + spade_a
官方取流接口(multi_video_model) ┘        │
                                          ▼
                          Java: 解析选择最优清晰度(跳过bytevc2)
                                          │
                          ffmpeg(本地): -c copy -decryption_key → 缓存 MP4
                                          │
                          ExoPlayer: 硬解 HEVC 播放本地文件
```

- 剧库：`landpage` 分页（旧版签名），下拉刷新 + 上滑加载更多 + 断连重试
- 搜索：官网 `/api/search/result`（无签名）
- 封面：CDN 图片直链（注意部分是 HEIC，见 §4 坑 8）
- 播放：`multi_video_model/v1`（新版 X-Gorgon 签名）→ 直链 + spade_a → 本地解密
- 播放历史/设置：SharedPreferences 本地存储

### 2.2 关键代码位置

| 职责 | 文件 |
| --- | --- |
| 签名算法 + spade_a→AES 密钥 | `core/HongguoSign.kt` |
| 全部接口调用 + ffmpeg 解密 | `core/DirectClient.kt` |
| 播放页（播放器+选集+全屏） | `PlayerActivity.kt` |
| 剧库页（列表/搜索/刷新） | `LibraryFragment.kt` |
| FFmpegKitNext AAR | `android/libs/ffmpeg-kit-next-maven/`（源码自编译） |
| 签名回归向量测试 | `app/src/test/java/com/duanju/player/core/` |

### 2.3 上游改签名时的维护方式

改动集中在 `HongguoSign.kt` / `DirectClient.kt`，改完用服务器侧（Go 版仍可用）生成新固定向量，替换 `LegacySignVectorTest` / `HongguoSignVectorTest` / `ContentKeyVectorTest` 中的期望值，`./gradlew :app:testDebugUnitTest` 通过后发新版 App。

## 3. 重大坑与最终解决方案

### 坑 1：ffmpeg-kit 退役，无现成可用包

- **现象**：Maven Central / CocoaPods / npm 上 ffmpeg-kit 全部二进制 404（2025-01 退役，2025-04 清库）。
- **解决**：原作者 2026-07 启动了官方续作 **FFmpegKitNext**（`arthenica/ffmpeg-kit-next`），只发源码不发二进制。本地 `./android.sh --arch=arm64-v8a` 编译出 AAR（约 9.1MB），以本地 maven 仓库随 App 仓库提交，克隆即可构建。
- **教训**：退役 ≠ 无解，先找官方续作/活跃 fork，再考虑自行编译或自研。

### 坑 2：签名算法 Kotlin 移植——手写位运算极易错

- **现象**：凭"看起来等价"手写移植，连续错 4 处。
- **具体错误清单**（都有代表性）：
  1. **byte 语义丢失**：Go 的 `box[box[i]+box[j]]` 中 byte+byte 自然回绕；Kotlin `u8+u8` 是 Int 加法不回绕 → 数组越界/结果错。必须 `(box[i]+box[j]).toByte()`。
  2. **PRGA 的 j 是否每轮 mask**：两种写法索引等价，但中途 KSA 单向赋值 `box[i]=box[j]`（非交换！）使 j 的中间值参与读操作，必须与 Go 逐位对齐。
  3. **第二轮变换多写了 `.inv()`**：Go 只有 `reverse8(a^b) ^ 0xeb`，Kotlin 手滑加了取反。
  4. **`shl/ushr` 与 rotate 等价性想当然**：`rol(mixed,-1)` 是 ror1 不是 rol63。
- **解决（方法论）**：
  1. 服务器 Go 版生成**固定输入→输出的向量文件**（固定 query/ts/nonce/random）；
  2. Kotlin 写对应用例断言十六进制全等；
  3. 失败时**分段对拍中间态**（input → ksa → rc4 → final 各打印一次，逐段定位第一处分叉）；
  4. 修到全绿。此法抓出了全部 4 处错。
- **教训**：跨语言移植加密/签名代码，向量测试不是可选项；"数学等价"的直觉在 byte 回绕、并行赋值、rotate 方向上全部不可靠。

### 坑 3：`Protocol not found`——openssl 没编进 ffmpeg

- **现象**：手机上 ffmpeg 打开 https 直链报 Protocol not found；同一 URL Mac 系统 ffmpeg 正常。
- **定位**：`strings libavformat.so | grep "recompile FFmpeg with openssl"` 直接看到编译期提示——首次编译没加 `--enable-lib-openssl`，https 协议被禁用。
- **解决**：重编 `./android.sh --arch=arm64-v8a --enable-lib-openssl`。

### 坑 4：`certificate verify failed`——openssl 找不到系统 CA（最深的一个坑）

- **现象**：openssl 编进去后变报 `SSL routines::certificate verify failed`；关 VPN、重启手机均无效；Mac 同 URL 正常、Java 层下载也正常，只有手机 ffmpeg 失败。
- **错误弯路**：
  1. 先怀疑 VPN/代理 → 用户关掉后依旧失败，**被用户纠正"有问题就去修，不要老想着回退/回避"**；
  2. 先瞎试 `-cafile` 参数位置——没看源码、没看日志，被用户点破"你就是靠猜啊"。
- **正确定位过程**：
  1. `loglevel verbose` + 打印 `getAllLogsAsString` 抓到精确错误 `error:0A000086:SSL routines::certificate verify failed`；
  2. 读 ffmpeg-kit-next 源码 `scripts/android/openssl.sh`：`./Configure` **没传 `--openssldir`**，openssl 编译期默认 CA 路径落在了构建机的 `prebuilt/android-arm64-24/openssl/ssl`——手机上根本不存在；
  3. 第一次修复踩子坑：`--openssldir=/system/etc/security/cacerts` 后 `make install_ssldirs` 试图在 Mac 上真建 `/system/etc` 目录而失败 → 把 `install_ssldirs` 从 make 目标中去掉（运行时默认路径由 Configure 决定，不需要真实安装）；
  4. 重编后 `strings` 确认默认路径已编入；再配合 App 启动时把 Android 系统 CA（`/apex/com.android.conscrypt/cacerts` 或 `/system/etc/security/cacerts`）合并导出 PEM 传 `-cafile` 双保险。
- **最终结果**：日志显示 `路径=ffmpeg直连`，0.5~1.2s/集，连续切集全部直连成功。
- **教训**：
  - 报错要抓**完整日志**再下结论，`loglevel verbose` + 全量 log 导出是基本操作；
  - 猜参数不如读源码：构建脚本的 Configure 选项就是根因所在；
  - 用户说得对——回退掩盖问题，root cause 修复后回退逻辑也应删掉（已删）。

### 坑 5：官方接口字段名不统一，导致列表全是"短剧"无封面

- **现象**：直连版剧库 36 条全是标题"短剧"、无封面、集数倒是正常。
- **根因**：三个接口的剧名字段完全不同——
  - 分类 `landpage`：`title` / `cover` / `episode_cnt`；
  - 详情/搜索：`series_title` / `series_cover`。
  我按 detail 的字段名解析分类接口，全部取空。
- **解决**：不猜字段，写 JVM 测试直接 dump 接口原始 JSON 的 keys（`rowKeys=...`）后按真实字段解析。
- **教训**：**同类接口字段名不通用**；上手机前先在电脑 JVM 层 dump 真实响应结构。

### 坑 6：真机调试低效，先电脑后手机（用户提出，效果显著）

- **旧方式**：装机 → 点 UI → 抓 logcat，一轮 5 分钟且手机被占用（还撞上用户正在用手机/其他 App 抢前台）。
- **改为**：JVM 集成测试 `DirectPipelineTest` 在电脑上一次跑通"首页→详情→取流→系统 ffmpeg 同参数下载解密→probe 校验"，把签名/解析/直链/密钥全部验证为正确，问题被隔离到"手机 ffmpeg 本身"，范围瞬间缩小。
- **教训**：凡是能在 JVM/宿主机闭环验证的链路，绝不占用真机试错。

### 坑 7：固定设备 ID 被风控（110001）

- **现象**：测试二进制用随机设备 ID 请求 model 接口返回 `110001 未知异常`。
- **根因**：公开项目里广为流传的固定 `device_id`/`iid` 已被上游拉黑。
- **解决**：App 首次运行随机生成 19 位正数设备身份并持久化；临时测试复用服务器已持久化的合法 ID。
- **子坑**：Kotlin 生成 19 位正数时 `Long` 移位溢出成**负数 iid**（日志里 `iid=-1789...`），上游直接空响应——`(v and Long.MAX_VALUE) % 8e18 + 1e18` 修正。

### 坑 8：封面是 HEIC 格式

- 分类接口的 `cover` 部分返回 `.heic` 直链（`tplv-...-superreso-aifit`），Android `BitmapFactory` 不解码 HEIC → 封面空白。
- 现状：部分封面（`fqnovelpic.com` 的 jpg/png）正常，HEIC 的仍空白，列为待办（引入 coil 或 HEIF 解码）。

### 坑 9：CDN 直链有效期约 1 小时

- 直链 `expire_time` 约 1h；缓存的 MP4 是解密后的成品不受影响，但**不能缓存直链本身跨天复用**，每次播放都重新走取流接口（约 0.5s，可接受）。

### 坑 10：R8 与 release 的干扰项

- 排查 ffmpeg 问题时一度怀疑 R8 裁剪（`-keep com.arthenica.ffmpegkit.**` 也确实该加），用户建议"先不用 R8 打包验证"——**minify 关掉依旧失败**，快速排除了 R8 因素。
- 教训：多个变量时，逐个关闭可疑变量做差分，比盯着代码猜快得多。

### 坑 11：uiautomator/layout 工具的真机局限

- 这台 Redmi（MIUI）上 `run-as` 被 SELinux 拒绝、uiautomator dump 常拿不到节点、`android layout` 间歇性失败，且悬浮窗（KClash）会抢前台。
- 应对：日志为主（路径/耗时打点），UI dump 只做辅证；关键结论一律用 logcat 固定 PID 的新日志，避免读到旧进程日志误判（实际发生过一次：拿旧日志"确认"了错误结论）。

## 4. 服务器侧遗留（与 App 无关但同期完成）

- 渐进式 HLS（8e47e8f）：透传接口首段可播即返回，切集从整集下载（1.6~2.4s）降到 0.5~0.7s。
- 此能力仍保留在 Go 服务中供网页端使用；App 已不再依赖。

## 5. 待办

1. **热门榜单**：官网首页 hot-drama 数据（服务器侧已确认可抓到 192KB 首页 HTML；Mac 直连官网超时，接入时走服务器代理或手机直连官网）。
2. **HEIC 封面解码**（coil-glide 或系统 HEIF 解码器）。
3. 上游签名再变更时的向量更新与发版流程（见 §2.3）。

## 6. 一句话总结

这轮从"服务器转码卡顿"走到"手机纯直连"，技术上最重要的三件事：**向量测试保证跨语言移植正确**、**读构建脚本源码定位编译期根因（openssl CA 路径）**、**先 JVM 闭环再上真机**；做事方式上最重要的一条来自用户的两次纠偏——不要用回退回避问题，不要靠猜，抓完整日志、看源码、修根因。