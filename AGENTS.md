# 项目协作规则

## 发布

- 仅当用户明确要求“发版 / 发布 Release”时，才启动发版流水线。普通提交、推送、标签推送均不触发发版。
- 发布到 `lonnnnnng/duanju`；本地 `duanju` 远端对应此仓库。旧 `guoguo-juku` 仓库保持不动。
- 先确认工作区和版本，将代码推送到 `duanju/main`，再手动触发 `.github/workflows/release.yml`。构建和 Release 资产上传统一由流水线完成，不再本地构建后手动上传。
- 不提前创建版本标签或 Release，流水线在测试和构建通过后创建。已有版本不覆盖；若失败留下草稿，先检查失败原因，未经用户授权不删除已有发布或标签。
- 等待流水线完成，核对运行提交、Release、五个平台程序和 `SHA256SUMS`；不能仅凭成功触发流水线宣告发布完成。
- 服务器部署仅在用户要求部署时执行；发布流程见 [docs/releasing.md](docs/releasing.md)。
