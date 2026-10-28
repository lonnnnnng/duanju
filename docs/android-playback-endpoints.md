# Web 服务端取流接口对照与排查手册

- 记录时间：2026-10-27（2026-10-28 拆分：安卓内容移出到 [android-app.md](android-app.md)）
- 相关版本：服务端 `v0.1.23`（取流仍走签名接口）
- 相关提交：`4c8bd18`（安卓改用开放接口，Web 端未变）
- 结论来源：代码阅读（`internal/app/provider_hongguo_*.go`）+ Mac 端实测

> 本文只覆盖 Web（Go 服务）侧的取流链路。**安卓客户端的功能与接口清单见
> [android-app.md](android-app.md)**，两边不要互相套用参数。

---

## 1. 一句话结论

| 端 | 取流接口 | 签名 | 版本号 |
| --- | --- | --- | --- |
| Web（服务器） | `POST api5-normal-sinfonlineb.fqnovel.com/novel/player/multi_video_model/v1/` | 全套（Gorgon + STUB + 占位 Argus/Ladon） | 73532 |
| Android | `GET api5-normal-sinfonlinec.fqnovel.com/reading/bookapi/video_tab/multi_video_model/v/` | **无** | **74232** |

安卓侧的两个必要条件（无签名接口 + 74232）与具体调用方式，见 [android-app.md](android-app.md)。

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
---

## 3. 签名接口（历史路径）判断

Android 早期实现与当前 Web 端共用这条签名路径：

| 返回 | 含义 |
| --- | --- |
| `200` + 0 字节 | 网关拒绝（签名不匹配/接口下线） |
| `200` + 53 字节 `{"code":101116,...}` | 过了网关，业务层拒绝 |
| `200` + `{"code":101000,"StatusMessage":"服务异常"}` | 设备身份不被接受 |

2026-10-27 实测：随机 id → 0 字节；文档真机 id → 101000。安卓已弃用该路径，
Web 端仍在使用（`version_code=73532`）。

---

## 4. 变更历史（Web 侧）

| 日期 | 变更 |
| --- | --- |
| 2026-10-28 | 文档拆分：安卓接口/排查移入 `docs/android-app.md` |
| 2026-10-27 | 安卓改为无签名开放接口与 74232（Web 端未变） |
| 2026-10-27 | 首页/搜索/详情切到 `bookmall/tab`、`search/fetch-debug`、`multi_video_detail`（安卓侧） |
| 2026-10-26 | 搜索曾临时对齐官网 `/api/search/result`；因移动网络不可达而放弃 |

---

## 5. 维护提醒（Web 侧）

1. Web 端取流参数、签名头与 body 结构改动后，用 `go test ./...` + 服务器真机播放各测一遍。
2. Web 端固定 `version_code=73532`，不要跟随安卓端的 65532/74232 改动。
3. 任何「空响应」都不要用简单重试掩盖，先确认接口是否还活着。
4. 安卓端的排查请走 [android-app.md](android-app.md#3-排查手册安卓)。
