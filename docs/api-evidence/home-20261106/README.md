# 首页 API 原始取证（2026-11-06）

完整结论、全部枚举和逐项请求索引见 [实测报告](../../2026-11-06-home-api-verification.md)。

正式轮从 `001-default` 开始，共 68 次发送；63 个去重筛选 ID 逐个请求。每个目录的 `request.json` 是完整入参，`response.body` 是原始出参，`response.json` 是状态/头/耗时/哈希，`analysis.json` 是可查询摘要。

`run.json` 记录环境；`recipe.json` 和 `probe-home-api.py` 是实际基线快照；`index.json` 是全量索引；`verification.json` 保存逐份正文哈希核验结果。无自动重试，无隐藏补发。Python HTTP 栈和公共参数编码与 Android 有差异，见报告边界。

公开归档中的 `response.json` 已将 `x-request-ip` 和 `X-Agw-Info` 响应头替换为 `<redacted>`，不再视为未脱敏的原始头；`response.body` 正文和正文哈希未改动。逐文件脱敏记录见 `redaction.json`，`SHA256SUMS` 校验公开版本。原始元数据仅保存在本地被 Git 忽略的 `android/app/build/reports/home-evidence-original-headers/`，不会随仓库推送。目录内脚本快照仍为当时实际运行版本，后续采集使用仓库 `tools/probe-home-api.py` 的脱敏逻辑。
