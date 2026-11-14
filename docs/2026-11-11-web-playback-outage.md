# Web 端播放全链路失败修复报告

- 日期：2026-11-11
- 影响范围：Web 端在线播放/下载取流
- 状态：已修复，已部署到 `192.168.1.2` 并验证

## 1. 现象

Web 端播放报错：

```text
播放地址解析失败： App 取流失败： App 接口返回空正文（HTTP 200）；
网页取流失败：hongguoduanju.com HTTP 404；
备用取流失败：Get "https://djapi.999888456.xyz/api/hongguo/play?..."：
remote error: tls: no application protocol
```

三条降级链路同时失效。

## 2. 排查结果

从服务器（出口 IP 与开发机不同）逐条实测：

| 链路 | 实测 | 结论 |
| --- | --- | --- |
| 签名 POST `/novel/player/multi_video_model/v1/` | `200` + 0 字节 | 上游网关已拒绝（与 11-06 遇到的情况一致） |
| 签名 GET `reading/.../multi_video_model/v/` | `200` + 0 字节 | 本机也已被限流/拒绝；详情接口同签名仍正常（91KB），说明不是签名算法问题 |
| 站点 HTML `/player/<sid>/<vid>` | `200`，但 `_ROUTER_DATA` 已不存在 | 站点早已改为 React，旧的页面解析路径无效；且解析失败会直接 return，堵死后面的降级 |
| 站点 `POST /api/video/protected/play-info` | `200`，返回明文 h264+aac 直链 | **可用**，且不需要 App 签名 |
| 备用 `djapi.999888456.xyz` | TLS `no application protocol` | 该服务本身已不可用（TLS/ALPN 配置异常） |

## 3. 根因

1. 主链路签名 POST 被上游关闭（11-06 起）。
2. 网页兜底代码仍按旧版 SSR 页面解析，站点改 React 后必然失败，且**错误直接返回**，导致后面可用的 `play-info` 永远执行不到。
3. 备用接口独立失效。
4. 我们新增的开放接口签名 GET 也处于上游限流状态（同签名调详情接口正常，可排除算法问题）。

## 4. 修复

`internal/app/provider_hongguo.go` 调整取流顺序为：

1. **站点 `play-info`**（新增首选，无需签名，返回明文 MP4）
2. 开放接口签名 GET（`provider_hongguo_open_media.go` 新增，恢复后自动启用）
3. 签名 POST（保留，作为历史路径）
4. 站点 HTML 解析（保留，作为最后兜底）
5. 备用接口（保留）

同时 `resolveHongguoWebMedia` 不再阻断后续降级（`play-info` 已提前）。

新增 `internal/app/provider_hongguo_open_media.go`：使用旧版全套签名的
`reading/bookapi/video_tab/multi_video_model/v/`（`version_code=74232`）+
旧版全套签名（X-Gorgon/X-Argus/X-Ladon/X-Khronos/X-SS-Req-Ticket/X-SS-STUB），
空正文重试 3~4 次。

## 5. 验证

服务器实测：

```text
# 正常剧集（protected 优先，0.38s）
https://v3-hgweb.qznovelvod.com/... | quality=0

# ffprobe 校验返回的直链
stream,h264,video
stream,aac,audio
```

连续高频探测后，站点会返回 403 并暂停该域名的请求（程序已有冷却机制），
冷却后恢复，属站点自身限流。

## 6. 后续（2026-11-14）

开放签名 GET 与备用接口 `djapi.999888456.xyz` 在后续版本中一直不可用，
已从取流链路中移除；站点 `play-info` 也因最高 720p 且会与 App 链路混出不同编码，
在 2026-11-14 一并移除。当前链路见 [红果接口说明](hongguo-api.md)。

## 7. 遗留

- `djapi.999888456.xyz` 已不可用，仅保留为最后兜底；如长期无效可考虑移除。
- 开放接口签名 GET 目前仍被上游限流，代码保留自动重试与降级。
- 站点 `play-info` 有频率限制，短时间内大量取流会 403，需要冷却。
