# 安卓播放方式对比：整集下载 / fMP4 over TCP / 本地 HLS

同一集视频（《婆婆装瘫七年，我高调离婚！》第 1 集，1080p HEVC，约 12 MB，146 秒）在三种实现下的差异。实测设备：Redmi Note 8 Pro（Android 16），局域网代理环境。

三种方式对应代码：

| 方式 | 入口 | 关键文件 |
| --- | --- | --- |
| 线上 v0.1.21：整集下载解密 | `PlayerActivity.playDirect()` | `DirectClient.decryptTo/decryptViaFfmpeg` |
| 方案 1：fMP4 over TCP | `PlayerActivity.playStream()`（保留未启用） | `core/Fmp4Streamer.kt`、`core/StreamDataSource.kt` |
| 方案 2：本地 HLS | `PlayerActivity.playHls()`（当前分支默认） | `core/HlsStreamer.kt`、`core/LocalHlsServer.kt` |

---

## 一、数据路径

| | 线上 | 方案 1 | 方案 2 |
| --- | --- | --- | --- |
| ffmpeg 输出 | 完整 mp4 文件（`+faststart`） | fMP4 写入本地 TCP | HLS 分片（`.ts`）写入本地目录 |
| 播放器输入 | `Uri.fromFile` 本地文件 | 自定义 `DataSource` 读 socket | `http://127.0.0.1:port/index.m3u8` |
| MediaSource | `ProgressiveMediaSource` | `ProgressiveMediaSource` + `FragmentedMp4Extractor` | `HlsMediaSource` |
| 中间层 | 文件系统 | 单向 TCP socket | 本地 HTTP + 文件系统 |
| 起播前提 | 整集写完 | 第一个分片到达 | 播放列表写完（`ENDLIST`） |

关键 ffmpeg 参数：

```text
# 线上
-decryption_key <hex> -i <cdn> -c copy -movflags +faststart -f mp4 out.mp4

# 方案 1
-decryption_key <hex> -i <cdn> -c copy \
-movflags +frag_keyframe+empty_moov+default_base_moof \
-flush_packets 1 -f mp4 tcp://127.0.0.1:<port>

# 方案 2
-decryption_key <hex> -i <cdn> -c copy -f hls -hls_time 2 \
-hls_list_size 0 -hls_playlist_type event -hls_flags independent_segments \
-hls_segment_filename seg%05d.ts index.m3u8
```

---

## 二、实测时间线

| 指标 | 线上 | 方案 1 | 方案 2 |
| --- | --- | --- | --- |
| 接口解析（章节 + 取流） | ~1.0 s | ~0.5 s | ~0.6 s |
| ffmpeg 转封装 | 2.28 s | 0.6 s | 0.79 s |
| 播放器准备 | ~0.3 s | ~0.1 s | ~0.1 s |
| **点击到首帧** | **≈3.5–4 s** | **≈0.6 s** | **≈0.9 s** |

方案 2 当前选择等 `ENDLIST` 后再起播，所以首帧时间包含整集转封装；若改为首个分片即起播，可降到约 0.3 s，但总时长会随分片增长（见下节）。

---

## 三、时长与索引

| | 线上 | 方案 1 | 方案 2 |
| --- | --- | --- | --- |
| 总时长 | 准确（mp4 `moov`） | **0 s（无 moov/索引）** | 准确（`#EXTINF` 累加，±1 s 取整） |
| 进度条 | 完整可用 | 不可用 | 可用 |
| 索引 | `stbl` / `sidx` 完整 | 无 | 分片索引（`ENDLIST`） |

原因：

- 方案 1 的 `-movflags +frag_keyframe+empty_moov` 只写空的 `moov` 与后续 `moof/mdat`，既没有总时长也没有索引，ExoPlayer 只能把「读到 EOF」当作结束。
- 方案 2 的 `event` 播放列表在 ffmpeg 写入过程中持续追加分片，ExoPlayer 的 `player.duration` 等于「已列出分片的 `#EXTINF` 累加」，因此写完前会从几十秒一路涨到完整值；等 `#EXT-X-ENDLIST` 后固定。

实测：方案 2 未等 `ENDLIST` 时出现 10 s → 30 s → 60 s 的爬升；改为等 `ENDLIST` 后，起播即 `02:26`，随后 `145 s` 属于分片取整误差。

---

## 四、seek 能力

| | 线上 | 方案 1 | 方案 2 |
| --- | --- | --- | --- |
| 进度条拖动 | 任意位置 | **不可用** | 可用（按分片） |
| +15 s / -15 s | 可用 | 缓冲内偶发成功，缓冲外失败 | 可用 |
| 进入时续播 | 播放器内 seek | 只能 ffmpeg `-ss` 重开 | 只能 ffmpeg `-ss` 重开 |
| 根本限制 | 本地文件随机读 | TCP 单向，已消费字节无法回退 | HTTP 支持 Range，可重开连接 |

方案 1 中 `StreamDataSource.close()` 不能真正关闭输入流，否则 ExoPlayer 重新 `open` 会拿到已关闭的 socket；`open(position)` 也只能丢弃字节，无法回到目标位置。

---

## 五、IO 与存储

| | 线上 | 方案 1 | 方案 2 |
| --- | --- | --- | --- |
| 磁盘占用 | 每集约 12 MB 明文常驻，无淘汰 | 0 | 每集约 12 MB 分片，切集删除 |
| 额外写放大 | `+faststart` 二次移动 moov | 无 | 无 |
| 内存 | 按需读文件 | ExoPlayer 可能整集缓冲 | 按分片缓冲 |
| 清理 | 只删 `.tmp`，旧集永久保留 | 无 | 切集/退出删除对应目录 |

---

## 六、网络与容错

| | 线上 | 方案 1 | 方案 2 |
| --- | --- | --- | --- |
| 弱网起播 | 等整集，慢 | 快（首片即播） | 等 `ENDLIST`，慢网退化为等整集 |
| 中途断网 | 失败，重试整集 | 读端 EOF，播放提前结束 | 缺分片卡住，播放器报错 |
| 失败回退 | 无 | 回退线上路径 | 回退线上路径 |
| 切集取消 | 取消 ffmpeg | 取消 ffmpeg + 关 socket | 取消 ffmpeg + 关 HTTP + 删目录 |

---

## 七、兼容性

| | 线上 | 方案 1 | 方案 2 |
| --- | --- | --- | --- |
| HEVC | 正常（mp4） | 正常（fMP4） | 正常（HEVC in MPEG-TS，本机可解） |
| 老旧设备 | 最好 | 好 | 一般（部分设备 TS 内 HEVC 支持差） |
| 音画分离 | 不支持（只取 `main_url`） | 不支持 | 不支持 |

需要注意：HLS 使用 MPEG-TS 分片，部分设备对 TS 内的 HEVC 支持不完整。若后续要覆盖更多机型，可把 `-hls_segment_type` 改为 `fmp4`。

---

## 八、实现复杂度

| | 线上 | 方案 1 | 方案 2 |
| --- | --- | --- | --- |
| 新增代码 | 0 | 约 170 行（socket 服务 + DataSource） | 约 250 行（HTTP 服务 + 分片管理） |
| 关键难点 | 无 | socket 生命周期、IPv4/IPv6、EOF 语义 | `ENDLIST` 等待时机、目录清理、Range |
| 当前状态 | 线上稳定 | 能播，但时长/seek 不可用 | 能播、时长正确、连播正常；seek 待手动确认 |

实现中遇到的真实问题：

1. `InetAddress.getLoopbackAddress()` 在双栈设备返回 `::1`，ffmpeg 连 `127.0.0.1` 被拒；改为显式绑定 IPv4。
2. ffmpeg 正常结束后主动关闭 socket，导致 ExoPlayer 缓冲耗尽后重新 `open` 报 `Socket is closed`；改为只关闭监听端口，让读端自然 EOF。
3. `multi_video_model` 的 `video_duration` 不可靠（实测返回 10 s，实际 146 s），不能用来固定时间轴；已移除基于它的 `ClippingMediaSource`。

---

## 九、结论

| 方式 | 首播 | 时长 | seek | 磁盘 | 适用 |
| --- | --- | --- | --- | --- | --- |
| 线上 | 3.5–4 s | 准确 | 完整 | 12 MB/集常驻 | 稳定优先 |
| 方案 1 | ≈0.6 s | 0 s | 不可用 | 无 | 仅验证可行性 |
| 方案 2 | ≈0.9 s | 准确 | 可用 | 12 MB/集临时 | 首播与正确性折中 |

- 方案 1 的 fMP4 + TCP 组合缺失时长与索引，且不可重读，不建议上线。
- 方案 2 是三者中体验与正确性的最佳折中，建议作为后续优化方向；慢网下仍要等整集转封装，这是当前选择的代价。
- 若要在慢网也立即出画，只能放弃「时长固定」，回到首片起播 + 时长增长；两者目前不可兼得。

---

## 十、复现

```bash
# 构建并安装分支版本
cd android && ./gradlew :app:assembleRelease
adb install -r app/build/outputs/apk/release/app-release.apk

# 观察关键日志
adb logcat -c
# 点击详情页「播放第 1 集」后
adb logcat -d | grep -E "HlsStreamer|Fmp4Streamer|HLS 就绪|PlayerActivity"

# 分片目录（方案 2）
adb shell run-as com.duanju.player ls cache/hls
```
