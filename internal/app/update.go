package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	githubReleaseAPIBase = "https://api.github.com/repos/"
	updateHTTPTimeout    = 2 * time.Minute
	updateDownloadLimit  = 512 << 20
)

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	Name    string         `json:"name"`
	Draft   bool           `json:"draft"`
	Pre     bool           `json:"prerelease"`
	Assets  []releaseAsset `json:"assets"`
}

type updateInfo struct {
	CurrentVersion  string `json:"currentVersion"`
	CurrentCommit   string `json:"currentCommit"`
	BuildTime       string `json:"buildTime"`
	Repository      string `json:"repository"`
	LatestVersion   string `json:"latestVersion,omitempty"`
	LatestName      string `json:"latestName,omitempty"`
	AssetName       string `json:"assetName,omitempty"`
	UpdateReady     bool   `json:"updateReady"`
	UpdateAvailable bool   `json:"updateAvailable"`
	UpdateApplied   bool   `json:"updateApplied,omitempty"`
	RestartRequired bool   `json:"restartRequired,omitempty"`
	AutoRestart     bool   `json:"autoRestart,omitempty"`
	Message         string `json:"message,omitempty"`
}

func currentUpdateInfo() updateInfo {
	return updateInfo{CurrentVersion: Version, CurrentCommit: Commit, BuildTime: BuildTime, Repository: updateRepository()}
}

func updateRepository() string {
	if value := strings.TrimSpace(os.Getenv("DUANJU_GITHUB_REPO")); value != "" {
		return value
	}
	return defaultGitHubRepository
}

func releaseAssetName(tag string) string {
	return releaseAssetNameFor(tag, runtime.GOOS, runtime.GOARCH)
}

func releaseAssetNameFor(tag, goos, goarch string) string {
	name := "duanju-" + tag + "-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

var newUpdateHTTPClient = func() *http.Client {
	return &http.Client{Timeout: updateHTTPTimeout}
}

func (a *UIApp) fetchLatestRelease(ctx context.Context) (githubRelease, updateInfo, error) {
	info := currentUpdateInfo()
	repository := info.Repository
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.ContainsAny(repository, " \t\r\n?#") {
		return githubRelease{}, info, errors.New("GitHub 仓库格式必须是 owner/repository")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, githubReleaseAPIBase+repository+"/releases/latest", nil)
	if err != nil {
		return githubRelease{}, info, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if token := strings.TrimSpace(os.Getenv("DUANJU_GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := newUpdateHTTPClient().Do(request)
	if err != nil {
		return githubRelease{}, info, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
		return githubRelease{}, info, fmt.Errorf("GitHub Release 请求失败（HTTP %d）：%s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var release githubRelease
	if err := json.NewDecoder(response.Body).Decode(&release); err != nil {
		return githubRelease{}, info, fmt.Errorf("解析 GitHub Release 失败：%w", err)
	}
	if release.Draft || release.Pre || strings.TrimSpace(release.TagName) == "" {
		return githubRelease{}, info, errors.New("最新 Release 不是可用的正式版本")
	}
	info.LatestVersion, info.LatestName = release.TagName, release.Name
	info.AssetName = releaseAssetName(release.TagName)
	info.UpdateAvailable = releaseIsNewer(Version, release.TagName)
	for _, asset := range release.Assets {
		if asset.Name == info.AssetName {
			info.UpdateReady = true
			break
		}
	}
	return release, info, nil
}

func releaseIsNewer(current, latest string) bool {
	if current == "dev" || current == "" || current == "unknown" {
		return true
	}
	parse := func(value string) ([]int, bool) {
		value = strings.TrimPrefix(value, "v")
		if suffix := strings.IndexAny(value, "-+"); suffix >= 0 {
			value = value[:suffix]
		}
		parts := strings.Split(value, ".")
		if len(parts) == 0 {
			return nil, false
		}
		result := make([]int, len(parts))
		for index, part := range parts {
			parsed, err := strconv.Atoi(part)
			if err != nil || parsed < 0 {
				return nil, false
			}
			result[index] = parsed
		}
		return result, true
	}
	currentParts, currentOK := parse(current)
	latestParts, latestOK := parse(latest)
	if !currentOK || !latestOK {
		return false
	}
	length := len(currentParts)
	if len(latestParts) > length {
		length = len(latestParts)
	}
	for index := 0; index < length; index++ {
		currentPart, latestPart := 0, 0
		if index < len(currentParts) {
			currentPart = currentParts[index]
		}
		if index < len(latestParts) {
			latestPart = latestParts[index]
		}
		if latestPart != currentPart {
			return latestPart > currentPart
		}
	}
	return false
}

func (a *UIApp) handleAdminUpdate(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if request.Method == http.MethodPost {
		a.updateMu.Lock()
		defer a.updateMu.Unlock()
		// long: 程序替换后到 systemd 重启前仍可能收到第二个 POST；保留首个备份和启动标记，避免二次覆盖使回滚失效。
		if _, err := os.Stat(pendingUpdatePath(a.cfg.dataDirectory())); err == nil {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "已有更新等待服务重启"})
			return
		} else if !errors.Is(err, os.ErrNotExist) {
			writeJSON(writer, http.StatusInternalServerError, map[string]string{"error": "无法检查更新启动状态：" + a.redactError(err)})
			return
		}
	}
	ctx, cancel := context.WithTimeout(request.Context(), updateHTTPTimeout)
	defer cancel()
	a.recordUpdateDiagnostic("app.update_check_started", "repository="+currentUpdateInfo().Repository)
	release, info, err := a.fetchLatestRelease(ctx)
	if err != nil {
		a.recordUpdateDiagnostic("app.update_check_failed", err.Error())
		writeJSON(writer, http.StatusBadGateway, map[string]any{"error": a.redactError(err), "data": info})
		return
	}
	if request.Method == http.MethodGet {
		a.recordUpdateDiagnostic("app.update_checked", fmt.Sprintf("current=%s latest=%s available=%t ready=%t asset=%s", info.CurrentVersion, info.LatestVersion, info.UpdateAvailable, info.UpdateReady, info.AssetName))
		writeJSON(writer, http.StatusOK, info)
		return
	}
	if !info.UpdateReady || !info.UpdateAvailable {
		a.recordUpdateDiagnostic("app.update_not_applied", fmt.Sprintf("current=%s latest=%s available=%t ready=%t", info.CurrentVersion, info.LatestVersion, info.UpdateAvailable, info.UpdateReady))
		info.Message = "当前系统没有匹配的 Release 程序包"
		if info.UpdateReady {
			info.Message = "当前程序已是最新版本"
		}
		writeJSON(writer, http.StatusConflict, info)
		return
	}
	if err := a.applyRelease(ctx, release, info.AssetName); err != nil {
		a.recordUpdateDiagnostic("app.update_failed", err.Error())
		writeJSON(writer, http.StatusInternalServerError, map[string]any{"error": a.redactError(err), "data": info})
		return
	}
	info.UpdateApplied = true
	info.RestartRequired = true
	info.AutoRestart = os.Getenv("DUANJU_AUTO_RESTART") == "1"
	info.Message = "程序已更新到磁盘，请手动重启服务"
	if info.AutoRestart {
		info.Message = "程序已更新到磁盘，服务即将重启"
	}
	a.recordUpdateDiagnostic("app.updated", "已下载并替换应用程序："+info.AssetName)
	writeJSON(writer, http.StatusOK, info)
	if info.AutoRestart {
		// long: systemd 通过非零退出触发 Restart=on-failure；延迟退出让 HTTP 响应先回到管理员浏览器。
		time.AfterFunc(250*time.Millisecond, func() { os.Exit(75) })
	}
}

func (a *UIApp) recordUpdateDiagnostic(event, message string) {
	if a != nil && a.downloader != nil {
		a.downloader.recordDiagnostic(diagnosticEvent{Level: "info", Event: event, Message: a.redactString(message)})
	}
}

func (a *UIApp) handleHealthz(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok", "version": Version, "commit": Commit})
}

func (a *UIApp) applyRelease(ctx context.Context, release githubRelease, assetName string) error {
	if runtime.GOOS == "windows" {
		return errors.New("Windows 不允许进程直接替换正在运行的程序；请下载 Release 程序并在停止服务后手动替换")
	}
	path, err := os.Executable()
	if err != nil {
		return fmt.Errorf("获取当前程序路径失败：%w", err)
	}
	path, err = filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("解析当前程序路径失败：%w", err)
	}
	return a.applyReleaseAt(ctx, release, assetName, path)
}

func (a *UIApp) applyReleaseAt(ctx context.Context, release githubRelease, assetName, path string) error {
	var binaryURL, checksumURL string
	for _, asset := range release.Assets {
		switch asset.Name {
		case assetName:
			binaryURL = asset.BrowserDownloadURL
		case "SHA256SUMS":
			checksumURL = asset.BrowserDownloadURL
		}
	}
	if binaryURL == "" || checksumURL == "" {
		return errors.New("Release 缺少匹配程序或 SHA256SUMS")
	}
	a.recordUpdateDiagnostic("app.update_download_started", "asset="+assetName)
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".duanju-update-*")
	if err != nil {
		return fmt.Errorf("创建更新临时文件失败：%w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := downloadReleaseAsset(ctx, binaryURL, temporary); err != nil {
		_ = temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if stat, statErr := os.Stat(temporaryName); statErr == nil {
		a.recordUpdateDiagnostic("app.update_download_completed", fmt.Sprintf("asset=%s bytes=%d", assetName, stat.Size()))
	}
	checksumBody, err := downloadReleaseBytes(ctx, checksumURL, 1<<20)
	if err != nil {
		return err
	}
	expected, err := checksumForAsset(checksumBody, assetName)
	if err != nil {
		return err
	}
	actual, err := fileSHA256(temporaryName)
	if err != nil {
		return err
	}
	if !strings.EqualFold(expected, actual) {
		return fmt.Errorf("更新程序校验失败：期望 %s，实际 %s", expected, actual)
	}
	a.recordUpdateDiagnostic("app.update_checksum_verified", "asset="+assetName)
	backupDir := filepath.Join(a.cfg.dataDirectory(), "update-backups")
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return fmt.Errorf("创建更新备份目录失败：%w", err)
	}
	backup := filepath.Join(backupDir, filepath.Base(path)+"."+strconv.FormatInt(time.Now().Unix(), 10)+".bak")
	if err := copyFile(path, backup, 0700); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("备份当前程序失败：%w", err)
	}
	a.recordUpdateDiagnostic("app.update_backup_created", "current executable backup created")
	if err := stagePendingUpdate(a.cfg.dataDirectory(), path, backup); err != nil {
		_ = os.Remove(backup)
		return fmt.Errorf("写入更新启动标记失败：%w", err)
	}
	a.recordUpdateDiagnostic("app.update_pending_staged", "restart marker written")
	mode := os.FileMode(0755)
	if stat, statErr := os.Stat(path); statErr == nil {
		mode = stat.Mode().Perm() & 0755
		if mode&0111 == 0 {
			mode = 0755
		}
	}
	if err := os.Chmod(temporaryName, mode); err != nil {
		_ = markPendingUpdateReady(a.cfg.dataDirectory())
		return err
	}
	if err := os.Rename(temporaryName, path); err != nil {
		_ = markPendingUpdateReady(a.cfg.dataDirectory())
		return fmt.Errorf("替换当前程序失败（旧程序仍保留）：%w", err)
	}
	a.recordUpdateDiagnostic("app.update_replaced", "asset="+assetName)
	return nil
}

func downloadReleaseAsset(ctx context.Context, assetURL string, destination *os.File) error {
	body, err := downloadReleaseResponse(ctx, assetURL)
	if err != nil {
		return err
	}
	defer body.Close()
	written, err := io.Copy(destination, io.LimitReader(body, updateDownloadLimit+1))
	if err != nil {
		return fmt.Errorf("下载更新程序失败：%w", err)
	}
	if written > updateDownloadLimit {
		return errors.New("更新程序超过大小限制")
	}
	return nil
}

func downloadReleaseBytes(ctx context.Context, assetURL string, limit int64) ([]byte, error) {
	body, err := downloadReleaseResponse(ctx, assetURL)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("Release 校验文件超过大小限制")
	}
	return data, nil
}

func downloadReleaseResponse(ctx context.Context, assetURL string) (io.ReadCloser, error) {
	parsed, err := url.Parse(assetURL)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() != "github.com" {
		return nil, errors.New("Release 资产地址不是可信的 GitHub 下载地址")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/octet-stream")
	if token := strings.TrimSpace(os.Getenv("DUANJU_GITHUB_TOKEN")); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	client := newUpdateHTTPClient()
	client.CheckRedirect = func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 || request.URL.Scheme != "https" {
			return errors.New("GitHub Release 下载重定向无效")
		}
		switch request.URL.Hostname() {
		case "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com":
			return nil
		default:
			return errors.New("GitHub Release 下载重定向到非可信域名")
		}
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("下载 Release 资产失败：%w", err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("下载 Release 资产失败（HTTP %d）", response.StatusCode)
	}
	return response.Body, nil
}

func checksumForAsset(body []byte, assetName string) (string, error) {
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && filepath.Base(fields[len(fields)-1]) == assetName && len(fields[0]) == sha256.Size*2 {
			if _, err := hex.DecodeString(fields[0]); err == nil {
				return fields[0], nil
			}
		}
	}
	return "", fmt.Errorf("SHA256SUMS 中没有 %s", assetName)
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func copyFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(output, input); err != nil {
		_ = output.Close()
		return err
	}
	return output.Close()
}
