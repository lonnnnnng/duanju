# 红果联网搜索失败修复报告

- 日期：2026-10-22
- 版本：v0.1.17
- 影响范围：剧库“联网搜索”
- 状态：已修复，已发布并在服务器 `192.168.1.2` 验证部署

## 1. 故障现象

在剧库中点击“联网搜索”时提示：

```text
联网搜索暂不可用：联网搜索暂不可用，请稍后重试；本地筛选仍可使用；可重试，本地筛选仍可使用
```

服务日志对应记录：

```text
联网搜索失败: 搜索未返回有效结果
```

本地筛选不受影响，只有联网搜索失败。

## 2. 根因

旧实现直接请求官网搜索页：

```text
https://hongguoduanju.com/search/<关键词>
```

并从页面内嵌的 `window._ROUTER_DATA` 中读取：

```text
loaderData["search_(keyword)/page"].searchList
```

上游已把搜索页改为客户端渲染。现在该 URL 返回的是 JavaScript 应用壳，不再包含 `_ROUTER_DATA`，因此旧解析逻辑拿不到 `searchList`，最终报“搜索未返回有效结果”。

## 3. 新接口

搜索页内嵌脚本中的 `_SdkGlueInit` 暴露了新的搜索接口：

```text
GET https://hongguoduanju.com/api/search/result?query=<关键词>
```

响应结构：

```json
{
  "searchList": [
    {
      "video_data": {
        "series_id": "7688740767782669374",
        "series_title": "一道划痕，满城风雨",
        "series_cover": "https://...",
        "series_intro": "...",
        "episode_cnt": 51,
        "series_status": 1,
        "episode_right_text": "全51集",
        "hot_score_data": {"score": 20019, "text": "2万热度"}
      }
    }
  ],
  "totalCount": "100",
  "pageNum": 1,
  "totalPages": 10,
  "hasMore": true
}
```

接口要求：

- `query` 不能为空，最长 30 个字符。
- 默认每页 10 条。
- 支持 `page` 参数翻页。

## 4. 修复内容

### 4.1 改用搜索接口

`fetchHongguoSearch` 不再解析搜索页 HTML，改为：

```text
GET /api/search/result?query=<URL 编码后的关键词>
Referer: https://hongguoduanju.com/search/<关键词>
```

响应按 JSON 解析：

- `searchList` 为数组时才视为成功。
- 存在 `code` 且不为 `0` 时返回接口错误。
- 继续复用 `hongguoDramaFromAny` 解析剧集，保留热度、简介、封面、集数等字段。
- `totalCount` 作为结果总数。

### 4.2 搜索词长度

上游限制为 30 个字符，因此把本地校验从 80 调整为 30，提前返回更明确的提示：

```text
请输入 1 至 30 个字符的搜索词
```

### 4.3 测试更新

- `searchSortFixture` 改为新接口的 JSON 结构。
- 搜索测试校验请求路径为 `/api/search/result`，且 `query` 参数正确。
- 保留原有的排序元数据合并、热度解析和缓存复用测试。

## 5. 验证

### 5.1 自动化测试

```sh
go test ./... -count=1
go vet ./...
go build ./...
```

全部通过。

### 5.2 服务器真实接口

在服务器网络环境直接调用新接口：

```text
状元  total=105 返回 10 条  首条：谁说纨绔不能当状元
瀚瀚  total=100 返回 10 条  首条：一道划痕，满城风雨
总裁  total=99  返回 10 条  首条：总裁是我前女友
```

### 5.3 应用接口

部署 v0.1.17 后通过应用自身的 `/api/ui/search` 验证：

```text
状元  status=200 total=105 returned=10
瀚瀚  status=200 total=100 returned=10
总裁  status=200 total=99  returned=10
```

重启后日志中没有新的“联网搜索失败”记录。

## 6. 发布与部署

- 提交：`4e06e81 fix: 修复红果官网搜索页改版导致的联网搜索失败`
- 发布：`v0.1.17`
- 发布仓库：`lonnnnnng/duanju`
- 发布资产：五个平台程序与 `SHA256SUMS` 已核对
- 服务器：`192.168.1.2:3333`，`/healthz` 返回 `status=ok`、`version=v0.1.17`
- 部署前备份：`/home/lee/duanju/duanju.bak-20261022122858`

## 7. 已知边界

- 当前应用只请求搜索结果第一页（10 条），接口本身支持 `page` 翻页；如需要更多结果可后续增加分页。
- 上游搜索接口和参数仍可能调整，本次修复不保证长期稳定。
- 搜索词上限与上游保持一致为 30 个字符。

## 8. 变更文件

```text
internal/app/provider_hongguo_search.go
internal/app/provider_hongguo_search_test.go
docs/2026-10-22-search-api-fix.md
```
