package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type updateRoundTripper func(*http.Request) (*http.Response, error)

func (roundTripper updateRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTripper(request)
}

func TestFetchLatestReleaseFindsCurrentPlatformAsset(t *testing.T) {
	oldClientFactory := newUpdateHTTPClient
	t.Cleanup(func() { newUpdateHTTPClient = oldClientFactory })
	t.Setenv("DUANJU_GITHUB_REPO", "test/duanju")
	assetName := releaseAssetNameFor("v9.9.9", runtime.GOOS, runtime.GOARCH)
	release := githubRelease{TagName: "v9.9.9", Name: "测试版本", Assets: []releaseAsset{{Name: assetName, BrowserDownloadURL: "https://github.com/test/duanju/releases/download/v9.9.9/" + assetName}}}
	body, err := json.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	newUpdateHTTPClient = func() *http.Client {
		return &http.Client{Transport: updateRoundTripper(func(request *http.Request) (*http.Response, error) {
			if got := request.URL.String(); got != "https://api.github.com/repos/test/duanju/releases/latest" {
				t.Fatalf("unexpected release URL: %s", got)
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
		})}
	}
	app := &UIApp{}
	gotRelease, info, err := app.fetchLatestRelease(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if gotRelease.TagName != release.TagName || info.AssetName != assetName || !info.UpdateReady || !info.UpdateAvailable {
		t.Fatalf("unexpected release info: %+v %+v", gotRelease, info)
	}
}

func TestApplyReleaseAtReplacesBinaryAndStagesPendingUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不支持原位替换正在运行的程序")
	}
	oldClientFactory := newUpdateHTTPClient
	t.Cleanup(func() { newUpdateHTTPClient = oldClientFactory })
	newBinary := []byte("new-duanju-binary")
	digest := sha256.Sum256(newBinary)
	assetName := "duanju-v9.9.9-linux-amd64"
	binaryURL := "https://github.com/test/release/binary"
	checksumURL := "https://github.com/test/release/SHA256SUMS"
	newUpdateHTTPClient = func() *http.Client {
		return &http.Client{Transport: updateRoundTripper(func(request *http.Request) (*http.Response, error) {
			var body []byte
			switch request.URL.String() {
			case binaryURL:
				body = newBinary
			case checksumURL:
				body = []byte(hex.EncodeToString(digest[:]) + "  " + assetName + "\n")
			default:
				t.Fatalf("unexpected update request: %s", request.URL.String())
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
		})}
	}

	root := t.TempDir()
	target := filepath.Join(root, "duanju")
	dataDir := filepath.Join(root, "data")
	if err := os.WriteFile(target, []byte("old-duanju-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := &UIApp{cfg: Config{dataDir: dataDir}}
	release := githubRelease{TagName: "v9.9.9", Assets: []releaseAsset{
		{Name: assetName, BrowserDownloadURL: binaryURL},
		{Name: "SHA256SUMS", BrowserDownloadURL: checksumURL},
	}}
	if err := app.applyReleaseAt(context.Background(), release, assetName, target); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(content, newBinary) {
		t.Fatalf("updated target = %q, err=%v", content, err)
	}
	pending, err := readPendingUpdate(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(pending.BackupPath)
	if err != nil || string(backup) != "old-duanju-binary" {
		t.Fatalf("backup = %q, err=%v", backup, err)
	}
	if err := markPendingUpdateReady(dataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pendingUpdatePath(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("pending marker still exists: %v", err)
	}
	if _, err := os.Stat(pending.BackupPath); !os.IsNotExist(err) {
		t.Fatalf("backup still exists after confirmation: %v", err)
	}
}

func TestApplyReleaseAtRejectsChecksumWithoutReplacingBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不支持原位替换正在运行的程序")
	}
	oldClientFactory := newUpdateHTTPClient
	t.Cleanup(func() { newUpdateHTTPClient = oldClientFactory })
	assetName := "duanju-v9.9.9-linux-amd64"
	binaryURL := "https://github.com/test/release/binary"
	checksumURL := "https://github.com/test/release/SHA256SUMS"
	newUpdateHTTPClient = func() *http.Client {
		return &http.Client{Transport: updateRoundTripper(func(request *http.Request) (*http.Response, error) {
			body := []byte("not-the-right-binary")
			if request.URL.String() == checksumURL {
				body = []byte(strings.Repeat("0", 64) + "  " + assetName + "\n")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
		})}
	}
	root := t.TempDir()
	target := filepath.Join(root, "duanju")
	dataDir := filepath.Join(root, "data")
	if err := os.WriteFile(target, []byte("old-duanju-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	app := &UIApp{cfg: Config{dataDir: dataDir}}
	release := githubRelease{Assets: []releaseAsset{{Name: assetName, BrowserDownloadURL: binaryURL}, {Name: "SHA256SUMS", BrowserDownloadURL: checksumURL}}}
	if err := app.applyReleaseAt(context.Background(), release, assetName, target); err == nil || !strings.Contains(err.Error(), "校验失败") {
		t.Fatalf("checksum failure = %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "old-duanju-binary" {
		t.Fatalf("target changed after checksum failure: %q, err=%v", content, err)
	}
	if _, err := os.Stat(pendingUpdatePath(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("pending marker created after checksum failure: %v", err)
	}
	backupDir := filepath.Join(dataDir, "update-backups")
	entries, err := os.ReadDir(backupDir)
	if err == nil && len(entries) != 0 {
		t.Fatalf("backup created after checksum failure: %v", entries)
	}
}

func TestApplyReleaseAtRemovesBackupWhenPendingMarkerCannotBeWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不支持原位替换正在运行的程序")
	}
	oldClientFactory := newUpdateHTTPClient
	t.Cleanup(func() { newUpdateHTTPClient = oldClientFactory })
	assetName := "duanju-v9.9.9-linux-amd64"
	binary := []byte("new-duanju-binary")
	digest := sha256.Sum256(binary)
	checksumURL := "https://github.com/test/release/SHA256SUMS"
	newUpdateHTTPClient = func() *http.Client {
		return &http.Client{Transport: updateRoundTripper(func(request *http.Request) (*http.Response, error) {
			body := binary
			if request.URL.String() == checksumURL {
				body = []byte(hex.EncodeToString(digest[:]) + "  " + assetName + "\n")
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header), Request: request}, nil
		})}
	}
	root := t.TempDir()
	target := filepath.Join(root, "duanju")
	dataDir := filepath.Join(root, "data")
	if err := os.WriteFile(target, []byte("old-duanju-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(pendingUpdatePath(dataDir), 0o700); err != nil {
		t.Fatal(err)
	}
	app := &UIApp{cfg: Config{dataDir: dataDir}}
	release := githubRelease{Assets: []releaseAsset{{Name: assetName, BrowserDownloadURL: "https://github.com/test/release/binary"}, {Name: "SHA256SUMS", BrowserDownloadURL: checksumURL}}}
	if err := app.applyReleaseAt(context.Background(), release, assetName, target); err == nil || !strings.Contains(err.Error(), "写入更新启动标记失败") {
		t.Fatalf("pending marker failure = %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "old-duanju-binary" {
		t.Fatalf("target changed after marker failure: %q, err=%v", content, err)
	}
	entries, err := os.ReadDir(filepath.Join(dataDir, "update-backups"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("orphaned backups after marker failure: %v, err=%v", entries, err)
	}
}

func TestReleaseAssetNameMatchesPublishedNaming(t *testing.T) {
	if got := releaseAssetNameFor("v0.1.7", "linux", "amd64"); got != "duanju-v0.1.7-linux-amd64" {
		t.Fatalf("unexpected Linux asset name: %s", got)
	}
	if got := releaseAssetNameFor("v0.1.7", "windows", "amd64"); got != "duanju-v0.1.7-windows-amd64.exe" {
		t.Fatalf("unexpected Windows asset name: %s", got)
	}
}

func TestReleaseIsNewer(t *testing.T) {
	for _, item := range []struct {
		current string
		latest  string
		want    bool
	}{{"dev", "v0.1.7", true}, {"v0.1.6", "v0.1.7", true}, {"0.1.7", "v0.1.7", false}, {"v0.1.8", "v0.1.7", false}, {"custom", "v0.1.7", false}} {
		if got := releaseIsNewer(item.current, item.latest); got != item.want {
			t.Errorf("releaseIsNewer(%q, %q) = %t, want %t", item.current, item.latest, got, item.want)
		}
	}
}

func TestChecksumForAsset(t *testing.T) {
	body := []byte(strings.Repeat("a", 64) + "  duanju-v0.1.7-linux-amd64\n" + strings.Repeat("b", 64) + "  duanju-v0.1.7-linux-arm64\n")
	got, err := checksumForAsset(body, "duanju-v0.1.7-linux-amd64")
	if err != nil || got != strings.Repeat("a", 64) {
		t.Fatalf("checksum parse failed: %q %v", got, err)
	}
	if _, err := checksumForAsset(body, "other-binary"); err == nil {
		t.Fatal("missing asset checksum was accepted")
	}
}

func TestHealthzDoesNotRequireAdministratorSession(t *testing.T) {
	app := &UIApp{}
	request := httptest.NewRequest(http.MethodGet, "http://localhost/healthz", nil)
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"status":"ok"`) || !strings.Contains(response.Body.String(), `"version":"`+Version+`"`) {
		t.Fatalf("unexpected health response: %d %s (runtime %s/%s)", response.Code, response.Body.String(), runtime.GOOS, runtime.GOARCH)
	}
}

func TestLANUpdateRequiresNoLogin(t *testing.T) {
	_, browser := lanFixture(t)
	// long: 无效仓库让请求停在更新源校验，证明匿名浏览器可进入设置且不会访问真实 Release。
	t.Setenv("DUANJU_GITHUB_REPO", "invalid repo")
	result := browser.request(t, http.MethodGet, "/api/ui/settings/update", nil)
	if result.Code != http.StatusBadGateway {
		t.Fatalf("LAN browser did not reach update check: %d %s", result.Code, result.Body.String())
	}
}

func TestAdminUpdateRejectsSecondApplyBeforeRestart(t *testing.T) {
	app, admin := lanFixture(t)
	if err := stagePendingUpdate(app.cfg.dataDirectory(), "/tmp/duanju-test-target", "/tmp/duanju-test-backup"); err != nil {
		t.Fatal(err)
	}
	// long: 待启动标记存在时应直接拒绝第二次 POST，避免再次下载后覆盖首次更新的回滚备份。
	result := admin.request(t, http.MethodPost, "/api/ui/settings/update", map[string]any{})
	if result.Code != http.StatusConflict || !strings.Contains(result.Body.String(), "已有更新等待服务重启") {
		t.Fatalf("second update was not rejected: status=%d body=%s", result.Code, result.Body.String())
	}
}
