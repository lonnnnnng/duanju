# 播放取流接口对照与排查手册

- 记录时间：2026-10-27
- 相关版本：android-v0.2.5（取流改无签名接口）
- 相关提交：`4c8bd18`（fix: 取流改用开放接口 + 74232）
- 结论来源：代码阅读（`internal/app/provider_hongguo_*.go`、`android/.../core/DirectClient.kt`）+ Mac 端实测

> 服务器在局域网，部分结论标注「未实测」的请勿当成已验证事实。

---

## 1. 一句话结论

| 端 | 取流接口 | 签名 | 版本号 |
| --- | --- | --- | --- |
| Web（服务器） | `POST api5-normal-sinfonlineb.fqnovel.com/novel/player/multi_video_model/v1/` | 全套（Gorgon + STUB + 占位 Argus/Ladon） | 73532 |
| Android | `GET api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/video_tab/multi_video_model/v/` | **无** | **74232（取流专用）** |

Android 能播的两个必要条件：**无签名接口** + **version_code=74232**。少任意一个，就会退化到「200 空正文」或「只有 bytevc2(H.266)」。

---

## 2. Web 端（服务器）

### 2.1 浏览器 → 服务器

```
POST /api/ui/playback/hls/open
Content-Type: application/json

{"dramaId":"hongguo:<seriesId>","episode":1}
→ {"url":"/api/ui/playback/native/hls/<id>/index.m3u8","duration":…,"quality":…,"title":…}
```

之后播放 `GET /api/ui/playback/native/hls/<id>/index.m3u8`（分片走 `/api/ui/playback/media/`）。
服务器自己解密并切片，不把官方直链暴露给浏览器。

### 2.2 服务器 → 官方

`internal/app/provider_hongguo_native_media.go` → `resolveHongguoAppMedia` → `hongguoAppRequest`

```
POST https://api5-normal-sinfonlineb.fqnovel.com/novel/player/multi_video_model/v1/
```

Query（21 项，`_rticket` 在发送前填入毫秒时间戳）：

```
aid=8662 & app_name=novelread & version_code=73532 & version_name=7.3.5.32
& manifest_version_code=73532 & update_version_code=73532 & channel=update_64
& device_platform=android & os=android & ssmix=a & device_type=25053RT47C
& device_brand=Redmi & language=zh & os_api=36 & os_version=16
& resolution=1280*2772 & dpi=520 & ac=wifi
& device_id=<随机持久化> & iid=<随机持久化> & _rticket=<ms>
```

Headers（14 项）：

```
User-Agent: com.phoenix.read/73532 (…Cronet/TTNet…)
Accept: application/json
Content-Type: application/json; charset=utf-8
X-XS-From-Web: 0
Sdk-Version: 2
X-SS-STUB: MD5(body) 大写
X-Khronos: <秒>
X-Gorgon: modelGorgon(query, body, ts, random)
X-SS-Req-Ticket: <毫秒>
X-Ladon: base64(ts 4 字节，高→低)   ← 占位
X-Argus: base64(ts 4 字节，低→高)   ← 占位
X-Tt-Request-Tag: t=0;n=0
Passport-Sdk-Version: 50561
X-Vc-Bdturing-Sdk-Version: 3.7.2.cn
```

Body：

```json
{
  "mixed_video_id_map": { "1004": ["<vid>"] },
  "biz_param": {
    "detail_page_version": 0, "device_level": 3, "disable_digg_stat": false,
    "need_all_video_definition": true, "need_mp4_align": false,
    "use_os_player": false, "use_server_dns": false, "video_platform": 1024
  }
}
```

重试：最多 3 次（1s/2s 退避），单次超时 20s；200 + 空正文直接报错。

解析：`data[<vid>].video_model`（字符串需二次解析）→ `video_list[]`
- 跳过 `bytevc2` / `gear_des_key` 含 bytevc2
- `spade_a` → `hongguoContentKey()` → AES-128 密钥
- 按 `definition` 取最高（同高度 h264 优先）；`Referer=https://novel.snssdk.com/`

---

## 3. Android 端

### 3.1 取流（`DirectClient.media`）

```
GET https://api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/video_tab/multi_video_model/v/
```

Query（33 项）：

```
iid & device_id & ac=wifi & channel=hongguo_baidu_pz_kaiping0105_android
& aid=8662 & app_name=novelread
& version_code=74232 & version_name=7.4.2.32 & manifest_version_code=74232
& update_version_code=74232 & pv_player=74232
& device_platform=android & os=android & ssmix=a
& device_type=Redmi+Note+8+Pro & device_brand=Redmi & language=zh
& os_api=36 & os_version=16 & resolution=1080*2198 & dpi=440
& gender=2 & need_personal_recommend=1 & player_so_load=1
& is_android_pad_screen=0 & host_abi=arm64-v8a & dragon_device_type=phone
& rom_version=BP4A.251205.006+release-keys & compliance_status=0
& cdid=1f1d14b3-8c25-4733-9f6b-2fdc01640081
& video_id=<vid> & video_platform=1024 & need_all_video_definition=1
```

Headers：**无**（不设 UA、不设 Accept、不带签名）。
重试：3 次（600ms/1200ms），15s 连接 / 30s 读取。

解析：`data.video_model_data[<vid>].video_model`（JSON 字符串）→ `video_list[]`
- 跳过 `bytevc2`
- 只接受 `https://` 的 `main_url`（不用 `backup_url`）
- 取最高 quality；`spade_a` → `HongguoSign.contentKey()`
- 本地 ffmpeg `-decryption_key` 解密 → mp4 → ExoPlayer

### 3.2 相关配套接口（同一域名族，均无签名）

| 用途 | 接口 |
| --- | --- |
| 首页 | `GET /reading/bookapi/bookmall/tab/v/`（tab_type=8） |
| 搜索 | `GET /reading/bookapi/search/fetch-debug/v/` |
| 详情/选集 | `GET /reading/bookapi/video_tab/multi_video_detail/v/`（vs_id_type=1） |
| 热词 | `GET /reading/bookapi/search/hot-search/v/` |

首页/搜索/详情使用 `version_code=65532`（筛选器更全、每页 9 部）；只有取流用 74232。

---

## 4. 失效时的快速排查

### 4.1 第一步：判断是「接口挂了」还是「编码不支持」

在 Mac 上直接跑（无需 App、无需签名）：

```bash
python3 - <<'PY'
import urllib.request, json
H="https://api5-normal-sinfonlinec.fqnovel.com"
C="aid=8662&device_id=1849336150284715062&version_code=74232&update_version_code=74232&device_platform=android"
sid="7688352674999176254"
d=json.loads(urllib.request.urlopen(f"{H}/reading/bookapi/video_tab/multi_video_detail/v/?{C}&video_series_id={sid}&vs_id_type=1").read())
vd=next(iter(d["data"]["video_detail_data"].values()))["video_data"]
vid=vd["video_list"][0]["vid"]
d=json.loads(urllib.request.urlopen(f"{H}/reading/bookapi/video_tab/multi_video_model/v/?{C}&video_id={vid}&video_platform=1024&need_all_video_definition=1").read())
m=json.loads(d["data"]["video_model_data"][vid]["video_model"])
print(sorted({((r.get("video_meta") or {}).get("definition"),(r.get("video_meta") or {}).get("codec_type")) for r in m["video_list"]}))
PY
```

### 4.2 症状对照表

| 现象 | 含义 | 处理 |
| --- | --- | --- |
| `失败：空响应(HTTP 200)` | 接口拒绝（签名/风控/下线） | 先跑 4.1；若开放接口也空，说明域名族整体变动，需要重新逆向 |
| `失败：该剧仅提供 H.266(bytevc2) 编码` | 返回里没有 bytevc1/h264 | 检查 `version_code` 是否被改回 65532；74232 才会同时给 bytevc1 |
| `失败：取流失败(101116)` | 业务层 SERVICE_ERROR | 换设备 id 重试；持续则接口策略变更 |
| `失败：取流失败(100103)` | PARAM_INVALID | `video_platform` 必须是 1024 |
| 有 URL 但 ffmpeg 报解密失败 | `spade_a` → 密钥派生问题 | 用 `ContentKeyVectorTest` 校验派生算法是否仍与样本一致 |
| 有 URL、解密成功但黑屏 | 编码/容器问题（H.266、bytevc2） | 确认选档逻辑仍跳过 bytevc2 |

### 4.3 签名接口（历史路径）判断

旧 Android 实现用的 `POST api5-normal-sinfonlineb.fqnovel.com/novel/player/multi_video_model/v1/`：

| 返回 | 含义 |
| --- | --- |
| `200` + 0 字节 | 网关拒绝（签名不匹配/接口下线） |
| `200` + 53 字节 `{"code":101116,...}` | 过了网关，业务层拒绝 |
| `200` + `{"code":101000,"StatusMessage":"服务异常"}` | 设备身份不被接受（用文档真机 id 复测时出现过） |

2026-10-27 实测：随机 id → 0 字节；文档真机 id → 101000。**该接口当前不可用**，Android 已弃用。

---

## 5. 变更历史

| 日期 | 变更 |
| --- | --- |
| 2026-10-27 | 取流从签名接口切到无签名 `reading/bookapi/video_tab/multi_video_model`；取流版本号提到 74232 以获取 HEVC；版本 v0.2.5 |
| 2026-10-27 | 首页/搜索/详情切到 `bookmall/tab`、`search/fetch-debug`、`multi_video_detail`（均无签名）；版本 v0.2.4 |
| 2026-10-26 | 搜索曾临时对齐官网 `/api/search/result`；因移动网络不可达而放弃 |

---

## 6. 维护提醒

1. 改动取流相关代码后，必须用一个 HEVC 剧和一个曾经「仅 bytevc2」的剧各测一遍（例如 `7698294694886788121`）。
2. `version_code` 在首页/搜索/详情（65532）与取流（74232）之间**故意不同**，不要统一。
3. 任何「空响应」都不要用简单重试掩盖，先跑 4.1 确认接口是否还活着。
