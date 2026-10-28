# 发布 Release

发版流水线：[`release.yml`](../.github/workflows/release.yml)。仅支持手动触发，普通推送、PR、标签推送都不会发版。用户明确说“发版”后才执行以下流程。

## 操作步骤

1. 确认本次改动、工作区及新版本号（正式版本使用 `vX.Y.Z`）。Web 端与安卓共用同一版本号，发版前先把 `android/app/build.gradle.kts` 的 `versionName` 改为同一版本，`versionCode` 递增。
2. 使用中文说明提交代码，推送到 `origin`（`lonnnnnng/duanju`）的 `main`。
3. 推送成功后手动启动流水线，例如发布 `v0.1.12`：

   ```bash
   git push origin HEAD:main
   gh workflow run release.yml --repo lonnnnnng/duanju --ref main -f version=v0.1.12
   gh run list --repo lonnnnnng/duanju --workflow release.yml --event workflow_dispatch --limit 5
   gh run watch <本次运行ID> --repo lonnnnnng/duanju --exit-status
   ```

   可通过 `-f notes='本次发布说明'` 补充说明；变更记录会自动生成。也可以在 GitHub **Actions → 手动构建并发布 Release → Run workflow** 中选 `main`、输入版本。

   仅安卓改动时加上 `-f android_only=true`：流水线不再构建 Go 程序，而是从上一版本 Release 原样取回五个平台程序并改成新版本名，只重新构建 APK。流水线会先用 GitHub compare 接口确认 `android/`、`docs/`、`.github/` 之外没有任何改动，否则直接失败。

4. 核对本次运行的 `headSha` 与推送提交一致，确认任务成功、Release 已正式公开、七个资产完整（五个平台程序、安卓 APK、`SHA256SUMS`）：

   ```bash
   gh run view <本次运行ID> --repo lonnnnnng/duanju --json headSha,status,conclusion,url
   gh release view v0.1.12 --repo lonnnnnng/duanju --json tagName,isDraft,url,assets
   ```

## 流水线内容

- 使用触发时 `main` 的确切提交；校验版本格式、版本递增及标签/Release 不重复。
- 安装 Go 稳定版、FFmpeg、Nginx、JDK 17；运行 `go test ./... -count=1` 和 `go vet ./...`（`android_only` 时跳过）。
- 复用 `scripts/build.sh`，构建 macOS arm64/amd64、Linux amd64/arm64、Windows amd64，注入版本、提交和构建时间（`android_only` 时改为从上一版本 Release 取回这五个文件）。
- 用仓库 Secrets 中的 `ANDROID_KEYSTORE_BASE64`、`ANDROID_KEYSTORE_PROPERTIES` 还原签名，运行 `:app:testDebugUnitTest :app:assembleRelease`，把 APK 以 `duanju-<版本>-android.apk` 归集到 `dist/` 并写入 `SHA256SUMS`。
- 完整重建 `SHA256SUMS`（六个产物），先创建草稿并上传五个程序、APK 和清单；校验远端大小和 SHA-256 后正式发布为 latest。
- 使用流水线自带的 `GITHUB_TOKEN` 与安卓签名 Secrets，无需额外发布密钥。真实源站和浏览器测试仍按项目现有开关手动验收。

若失败，查看该次运行日志；测试或构建失败时不会创建 Release，上传或校验失败可能保留草稿，正式发布后的复查失败需先核实远端实际状态。不要直接覆盖或删除历史版本。

发布本身不会部署服务器。用户同时要求部署时，再执行服务器的 `duanju-service update`，核对健康接口版本、文件摘要和实际播放。
