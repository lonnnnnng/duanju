# 设备池与 CDN 主机健康排序（2026-11-14）

- 影响范围：Web 端取流稳定性、下载成功率
- 状态：已实现、已部署到 `192.168.1.2`，服务器实测通过

## 1. 背景

红果上游对 App 接口有频率风控：已注册设备在短时间高频请求后仍会返回
`200` + 0 字节（`444` 同理）。此前的处理只有「单设备 + 失败时重新注册」，
既浪费注册次数，也会在单次请求内耗尽重试预算。

站点/CDN 侧也一样：`play-info` 与媒体 CDN 域名偶发 `403`/超时，
原实现只按固定顺序重试，不会记住哪个域名刚才失败过。

## 2. 设备池

- 注册设备保存在设备池（上限 3 个），持久化到 `data/hongguo-devices.json`，
  重启后直接复用，不再重新注册。
- 请求返回空正文时，先切换到池中**未被拒绝**的设备重试；池中没有可用设备时才注册新设备，
  超过上限淘汰最早的一个。
- 注册失败仍有 10 分钟冷却，避免连续触发风控。
- 兼容旧的 `library.json` 中的 `hongguoApp` 状态，首次启动会并入设备池。

实现：`provider_hongguo_app.go`（设备池结构与轮换）、
`provider_hongguo_device.go`（注册、持久化、恢复）。

## 3. CDN 主机健康排序

新增 `provider_host_health.go`：被动记录每次请求的结果与耗时，不额外发探测请求。

- 失败主机进入 2 分钟冷却，排序时排到最后；
- 成功主机按延迟 EWMA 排序；
- 未观测过的主机使用中性延迟，保持原有候选顺序。

接入点：

| 位置 | 作用 |
| --- | --- |
| `providerURLCandidates` | 站点域名候选按健康度排序 |
| `fetchProviderText` | 记录站点页面请求结果 |
| `resolveHongguoProtectedMedia` | `play-info` 按候选域名依次尝试并记录 |
| `selectHongguoAppMediaOrdered` | 同清晰度的多个 CDN 备用地址按健康度排序 |
| `hlsProxy` | 记录媒体分片请求的成功/失败与耗时 |

## 4. 验证

服务器实测：

```text
# 下载整部 78 集全部 success（约 3 分钟）
tasks 78 Counter({'success': 78})

# 设备池落盘
data/hongguo-devices.json:
{"version":1,"deviceId":"1518904504239067",...,"devices":[{...}]}

# 重启后复用设备池；空正文时自动切换到池中另一台设备并持久化
```

单元测试新增：

- `TestProviderHostHealthOrdersFailedHostLast` / `RecoversAfterSuccess` / `OrderMediaAddresses`
- `TestHongguoDevicePoolNormalizeAndRotate` / `CapsSize` / `PersistsAcrossRestart` / `SkipsCorruptFile`
- `TestHongguoAppRequestRotatesDeviceOnEmptyBody`

`go test ./...`、`go vet ./...` 全部通过。

## 5. 说明

- 主机健康数据只在内存中，进程重启后从零开始（不影响正确性）。
- 设备池文件仅含设备号与安装号，不含 Cookie 或账号信息。
- 高频批量下载仍可能触发上游限流，此时会看到「空正文」日志，
  设备池会轮换设备、媒体地址会换备用域名，属于预期行为。
