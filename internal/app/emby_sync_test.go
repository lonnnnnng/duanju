package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func embySyncTestChapters() []Chapter {
	return []Chapter{{ID: "fixture-1", Title: "第一集"}, {ID: "fixture-2", Title: "第二集"}}
}

func embyTestSTRMs(t *testing.T, season string) []string {
	t.Helper()
	entries, err := os.ReadDir(season)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".strm") {
			paths = append(paths, filepath.Join(season, entry.Name()))
		}
	}
	return paths
}

func TestEmbySyncFilesIdempotentStableAndNonDestructive(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test:8998"}
	drama := Drama{ID: historyFixtureDramaID, Title: "合成测试 <&>"}
	key := make([]byte, 32)
	chapters := embySyncTestChapters()
	folder, written, total, err := syncEmbyDramaFiles(context.Background(), settings, drama, chapters, "", key, "fixture-owner")
	if err != nil || written != 5 || total != 2 {
		t.Fatal(folder, written, total, err)
	}
	first := filepath.Join(settings.OutputDir, folder, "Season 01", "S01E001.strm")
	before, _ := os.Stat(first)
	original, _ := os.ReadFile(first)
	if !strings.Contains(string(original), "account=fixture-owner") {
		t.Fatal("automated link not bound to owner")
	}
	_, written, _, err = syncEmbyDramaFiles(context.Background(), settings, drama, chapters, folder, key, "fixture-owner")
	after, _ := os.Stat(first)
	if err != nil || written != 0 || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("unchanged export rewrote files", written, err)
	}
	note := filepath.Join(settings.OutputDir, folder, "my-notes.txt")
	if err = os.WriteFile(note, []byte("user text"), 0600); err != nil {
		t.Fatal(err)
	}
	chapters = []Chapter{chapters[1], chapters[0], {ID: "fixture-3", Title: "第三集"}}
	drama.Title = "资料更新后的合成剧名"
	same, written, total, err := syncEmbyDramaFiles(context.Background(), settings, drama, chapters, folder, key, "fixture-owner")
	if err != nil || same != folder || total != 3 {
		t.Fatal("renamed/reordered series duplicated or renumbered", same, total, err)
	}
	current, _ := os.ReadFile(first)
	if string(current) != string(original) {
		t.Fatal("existing episode link changed after source order changed")
	}
	if body, _ := os.ReadFile(note); string(body) != "user text" {
		t.Fatal("unrelated file was changed")
	}
	_, _, total, err = syncEmbyDramaFiles(context.Background(), settings, drama, chapters[:1], folder, key, "fixture-owner")
	if err != nil || total != 3 {
		t.Fatal("temporarily shorter upstream deleted previous episodes", total, err)
	}
	marker := filepath.Join(settings.OutputDir, folder, ".duanju-emby.json")
	var manifest embyFolderManifest
	if err = readEmbyJSON(marker, &manifest, 8<<20); err != nil || len(manifest.Chapters) != 3 {
		t.Fatal(err)
	}
}

func TestEmbySyncMergedOnlySwitchAndDirectFiles(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	owner := app.browserViewers().accountStore().state.Accounts["admin"]
	output := t.TempDir()
	manager.document.Settings = embySyncSettings{Enabled: true, OutputDir: output, BaseURL: "http://library.test", IntervalMinutes: 60, Following: true}
	manager.document.OwnerID = owner.ID
	manager.runOnce(context.Background())
	entry := manager.document.Entries[historyFixtureDramaID]
	season := filepath.Join(output, entry.Folder, "Season 01")
	if entry.Episodes != 3 {
		t.Fatal("分集模式没有生成完整分集", entry.Episodes, manager.document.Error)
	}
	// long: 现网旧清单没有 mode 字段，切换全集时仍须识别并撤下其分集入口。
	marker := filepath.Join(output, entry.Folder, ".duanju-emby.json")
	var legacy embyFolderManifest
	if err := readEmbyJSON(marker, &legacy, 8<<20); err != nil {
		t.Fatal(err)
	}
	legacy.Mode = ""
	if err := writeEmbyJSON(marker, legacy, 0600); err != nil {
		t.Fatal(err)
	}
	mergedPath := filepath.Join(t.TempDir(), "全集.mp4")
	app.cfg.OutputDir = filepath.Dir(mergedPath)
	if err := os.WriteFile(mergedPath, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	app.merges = map[string]*UIMergeState{historyFixtureDramaID: {DramaID: historyFixtureDramaID, DramaTitle: "合并全集", Status: "success", StartEpisode: 1, EndEpisode: 3, OutputPath: mergedPath}}
	app.mu.Unlock()
	manager.document.Settings.MergedOnly = true
	entry.CheckedAt = time.Time{}
	manager.document.Entries[historyFixtureDramaID] = entry
	manager.runOnce(context.Background())
	entry = manager.document.Entries[historyFixtureDramaID]
	if manager.document.Error != "" || entry.Episodes != 1 {
		t.Fatal("全集模式同步失败", manager.document.Error, entry.Error, entry.Episodes)
	}
	links := embyTestSTRMs(t, season)
	if len(links) != 0 {
		t.Fatal("全集模式仍保留分集播放入口", links)
	}
	media := filepath.Join(season, "S01E001.mp4")
	if body, err := os.ReadFile(media); err != nil || string(body) != "0123456789" {
		t.Fatal("全集没有作为真实 MP4 发布", err)
	}
	if body, err := os.ReadFile(filepath.Join(output, entry.Folder, "tvshow.nfo")); err != nil || strings.Contains(string(body), "/api/emby/") {
		t.Fatal("本地全集元数据仍依赖剧库地址", err)
	}
	app.mu.Lock()
	app.merges[historyFixtureDramaID].Status = "failed"
	app.mu.Unlock()
	manager.runOnce(context.Background())
	links = embyTestSTRMs(t, season)
	if len(links) != 0 || manager.document.Entries[historyFixtureDramaID].Episodes != 0 {
		t.Fatal("失效全集仍在 Emby 输出目录中", links)
	}
	if _, err := os.Stat(media); !os.IsNotExist(err) {
		t.Fatal("失效全集的入库 MP4 没有撤下", err)
	}
	if _, err := os.Stat(filepath.Join(output, entry.Folder)); !os.IsNotExist(err) {
		t.Fatal("失效全集的空剧集目录仍被保留", err)
	}
	if _, err := os.Stat(mergedPath); err != nil {
		t.Fatal("撤下入口误删了原始合并 MP4", err)
	}
	manager.document.Settings.MergedOnly = false
	entry = manager.document.Entries[historyFixtureDramaID]
	entry.CheckedAt = time.Time{}
	manager.document.Entries[historyFixtureDramaID] = entry
	manager.runOnce(context.Background())
	links = embyTestSTRMs(t, season)
	if len(links) != 3 {
		t.Fatal("切回分集模式没有恢复分集", links, manager.document.Error)
	}
}

func TestEmbyRetirePreservesChangedSTRM(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Title: "测试剧"}
	key := make([]byte, 32)
	folder, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(settings.OutputDir, folder, "Season 01", "S01E002.strm")
	if err := os.WriteFile(path, []byte("https://custom.example/video.mp4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key); err == nil {
		t.Fatal("用户修改的播放入口被当成程序文件撤下")
	}
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "https://custom.example/video.mp4\n" {
		t.Fatal("用户修改的入口没有保留", err)
	}
}

func TestEmbyRetireRemovesManagedMetadataAndEmptyFolders(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Title: "完整清理测试剧"}
	key := make([]byte, 32)
	folder, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	removed, retired, err := retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key)
	if err != nil || !retired || removed != 5 {
		t.Fatal("程序生成的 Emby 文件没有完整清理", removed, retired, err)
	}
	if _, err := os.Stat(filepath.Join(settings.OutputDir, folder)); !os.IsNotExist(err) {
		t.Fatal("清理后仍残留空剧集目录", err)
	}
}

func TestEmbyRetirePreservesUserFiles(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Title: "保留用户文件测试剧"}
	key := make([]byte, 32)
	folder, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(settings.OutputDir, folder)
	userFile := filepath.Join(target, "poster.jpg")
	if err := os.WriteFile(userFile, []byte("user"), 0600); err != nil {
		t.Fatal(err)
	}
	userNFO := filepath.Join(target, "Season 01", "S01E001.nfo")
	if err := os.WriteFile(userNFO, []byte("<episodedetails><title>用户改过的标题</title><season>1</season><episode>1</episode></episodedetails>\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, retired, err := retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key); err != nil || !retired {
		t.Fatal("包含用户文件时退休失败", retired, err)
	}
	if body, err := os.ReadFile(userFile); err != nil || string(body) != "user" {
		t.Fatal("用户文件被误删或修改", err)
	}
	if body, err := os.ReadFile(userNFO); err != nil || !strings.Contains(string(body), "用户改过的标题") {
		t.Fatal("用户修改的 NFO 被误删或修改", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("包含用户文件的剧集目录被误删", err)
	}
	if _, err := os.Stat(filepath.Join(target, "Season 01")); err != nil {
		t.Fatal("用户修改的 NFO 所在目录被误删", err)
	}
}

func TestEmbyRetireCleansLegacyRetiredDirectory(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Title: "旧退休清理测试剧"}
	key := make([]byte, 32)
	folder, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(settings.OutputDir, folder)
	season := filepath.Join(target, "Season 01")
	for _, name := range []string{"S01E001.strm", "S01E002.strm"} {
		if err := os.Remove(filepath.Join(season, name)); err != nil {
			t.Fatal(err)
		}
	}
	marker := filepath.Join(target, ".duanju-emby.json")
	var manifest embyFolderManifest
	if err := readEmbyJSON(marker, &manifest, 8<<20); err != nil {
		t.Fatal(err)
	}
	manifest.Mode = "retired"
	if err := writeEmbyJSON(marker, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if _, retired, err := retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key); err != nil || !retired {
		t.Fatal("旧退休清单没有继续清理", retired, err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("旧退休清理后仍残留空目录", err)
	}
}

func TestEmbyRetireCleansLegacyMergedNFOWithoutSTRM(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Title: "旧全集退休测试剧"}
	key := make([]byte, 32)
	mergedPath := filepath.Join(t.TempDir(), "全集.mp4")
	if err := os.WriteFile(mergedPath, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	folder, _, _, err := syncEmbyLegacyMergedFilesForTest(context.Background(), settings, drama, UIMergeState{
		DramaID: drama.ID, Status: "success", StartEpisode: 1, EndEpisode: 3, OutputPath: mergedPath,
	}, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(settings.OutputDir, folder)
	season := filepath.Join(target, "Season 01")
	if err := os.Remove(filepath.Join(season, "S01E001.strm")); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(target, ".duanju-emby.json")
	var manifest embyFolderManifest
	if err := readEmbyJSON(marker, &manifest, 8<<20); err != nil {
		t.Fatal(err)
	}
	manifest.Mode = "retired"
	manifest.RetiredMode = ""
	if err := writeEmbyJSON(marker, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	removed, retired, err := retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key)
	if err != nil || !retired || removed != 2 {
		t.Fatal("旧全集 NFO 没有被清理", removed, retired, err)
	}
	if _, err := os.Lstat(target); !os.IsNotExist(err) {
		t.Fatal("旧全集 NFO 清理后残留目录", err)
	}
}

func TestEmbyRetirePreservesModifiedMergedNFOAcrossRetry(t *testing.T) {
	settings := embySyncSettings{OutputDir: t.TempDir(), BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Title: "用户修改全集元数据"}
	key := make([]byte, 32)
	mergedPath := filepath.Join(t.TempDir(), "全集.mp4")
	if err := os.WriteFile(mergedPath, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	folder, _, _, err := syncEmbyLegacyMergedFilesForTest(context.Background(), settings, drama, UIMergeState{
		DramaID: drama.ID, Status: "success", StartEpisode: 1, EndEpisode: 3, OutputPath: mergedPath,
	}, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(settings.OutputDir, folder)
	nfo := filepath.Join(target, "Season 01", "S01E001.nfo")
	body, err := os.ReadFile(nfo)
	if err != nil {
		t.Fatal(err)
	}
	modified := append(append([]byte(nil), body...), []byte("<!-- user metadata -->\n")...)
	if err := os.WriteFile(nfo, modified, 0600); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, retired, err := retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key); err != nil || !retired {
			t.Fatal("全集退休或重试失败", attempt, retired, err)
		}
		if current, err := os.ReadFile(nfo); err != nil || string(current) != string(modified) {
			t.Fatal("用户修改的全集 NFO 被误删", attempt, err)
		}
	}
	var manifest embyFolderManifest
	if err := readEmbyJSON(filepath.Join(target, ".duanju-emby.json"), &manifest, 8<<20); err != nil || manifest.Mode != "retired" || manifest.RetiredMode != "merged" {
		t.Fatal("退休清单没有保留全集模式", manifest, err)
	}
}

func TestEmbySyncRefusesUnmanagedFoldersAndSymlinks(t *testing.T) {
	root := t.TempDir()
	settings := embySyncSettings{OutputDir: root, BaseURL: "http://library.test"}
	drama := Drama{ID: historyFixtureDramaID, Title: "独立目录"}
	folder := embyFolderName(drama)
	if err := os.Mkdir(filepath.Join(root, folder), 0755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, folder, "sentinel.txt")
	os.WriteFile(sentinel, []byte("preserved"), 0600)
	if _, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", make([]byte, 32), "owner"); err == nil {
		t.Fatal("unmanaged directory adopted without permission")
	}
	if data, _ := os.ReadFile(sentinel); string(data) != "preserved" {
		t.Fatal("unmanaged directory modified")
	}
	settings.OutputDir = t.TempDir()
	folder, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), "", make([]byte, 32), "owner")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(settings.OutputDir, folder, "Season 01", "S01E001.strm")
	if err = os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err = os.Symlink(sentinel, target); err != nil {
		t.Skip("symlinks unavailable")
	}
	if _, _, _, err = syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), folder, make([]byte, 32), "owner"); err == nil {
		t.Fatal("managed file symlink followed")
	}
	if data, _ := os.ReadFile(sentinel); string(data) != "preserved" {
		t.Fatal("symlink target modified")
	}
	for _, folder := range []string{"../escape", "..", "/absolute", "nested/path", "nested\\path"} {
		if _, _, _, err := syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), folder, make([]byte, 32), "owner"); err == nil {
			t.Fatal("unsafe folder accepted", folder)
		}
	}
}

func waitEmbySync(t *testing.T, manager *embySyncManager, after time.Time) embySyncDocument {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		manager.mu.Lock()
		done := !manager.running && manager.document.LastFinishedAt.After(after)
		snapshot := manager.document
		manager.mu.Unlock()
		if done {
			return snapshot
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("automatic Emby synchronization did not finish")
	return embySyncDocument{}
}

func TestEmbySyncAutomaticFollowedImportRefreshAndRestart(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	ordinary := newAccountTestBrowser(t, app)
	ordinary.register(t, "other-viewer")
	otherID := "hongguo:7000000000000000002"
	app.dramas = append(app.dramas, Drama{ID: otherID, Source: sourceHongguo, Title: "另一个用户的剧"})
	viewerResultOK(t, ordinary.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": otherID, "saved": true}))
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	var scans atomic.Int32
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emby/Library/Refresh" || r.Header.Get("X-Emby-Token") != "private-test-key" || r.URL.RawQuery != "" {
			t.Error("invalid Emby refresh request")
		}
		scans.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer emby.Close()
	settings := map[string]any{"enabled": true, "outputDir": t.TempDir(), "baseUrl": "http://library.test:8998", "intervalMinutes": 60, "following": true, "downloads": false, "serverUrl": emby.URL + "/emby", "apiKey": "private-test-key"}
	started := time.Now()
	response := admin.request(t, http.MethodPost, "/api/ui/admin/emby", settings)
	viewerResultOK(t, response)
	if strings.Contains(response.Body.String(), "private-test-key") {
		t.Fatal("API response leaked the Emby key")
	}
	result := waitEmbySync(t, manager, started)
	if result.Error != "" || result.Succeeded != 1 || len(result.Entries) != 1 || result.Entries[historyFixtureDramaID].Episodes != 3 || scans.Load() != 1 {
		t.Fatal("automatic import failed or crossed accounts", result.Error, result.Succeeded, len(result.Entries), scans.Load())
	}
	output := settings["outputDir"].(string)
	entry := result.Entries[historyFixtureDramaID]
	strm, err := os.ReadFile(filepath.Join(output, entry.Folder, "Season 01", "S01E001.strm"))
	if err != nil {
		t.Fatal(err)
	}
	link := strings.TrimSpace(string(strm))
	parsed, _ := url.Parse(link)
	if parsed.Query().Get("account") == "" {
		t.Fatal("automatic export created a guest link")
	}
	writer := httptest.NewRecorder()
	app.handleEmbyStream(writer, httptest.NewRequest(http.MethodHead, link, nil))
	if writer.Code != 200 {
		t.Fatal("automatic signed link unusable", writer.Code)
	}
	info, err := os.Stat(manager.path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("sync settings not saved privately")
	}
	manager.stop()
	restarted := &UIApp{cfg: app.cfg, downloader: app.downloader, dramas: app.dramas}
	next := restarted.embySyncer()
	if next.loadErr != nil || !next.document.Settings.Enabled || next.document.Settings.APIKey != "private-test-key" {
		t.Fatal("restart lost configuration", next.loadErr)
	}
	for id, entry := range next.document.Entries {
		entry.CheckedAt = time.Time{}
		next.document.Entries[id] = entry
	}
	next.runOnce(context.Background())
	if next.document.WrittenFiles != 0 || scans.Load() != 1 || next.document.Error != "" {
		t.Fatal("restart duplicated exports or scans", next.document.WrittenFiles, scans.Load(), next.document.Error)
	}
	if data, err := os.ReadFile(filepath.Join(output, entry.Folder, "Season 01", "S01E001.strm")); err != nil || string(data) != string(strm) {
		t.Fatal("restart changed stable link")
	}
}

func TestEmbySyncAdminGateDisabledDefaultsAndValidation(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	guest := newAccountTestBrowser(t, app)
	member := newAccountTestBrowser(t, app)
	member.register(t, "online-only-fixture")
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/accounts/permissions", map[string]any{"username": "online-only-fixture", "onlineOnly": true}))
	for _, browser := range []*accountTestBrowser{guest, member} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			response := browser.request(t, method, "/api/ui/admin/emby", map[string]any{})
			if response.Code != http.StatusForbidden {
				t.Fatal("non-admin accessed server export settings", response.Code)
			}
		}
		// long: 即使同步配置稍后启用，普通账号也不能强制唤醒管理员的全库同步。
		if response := browser.request(t, http.MethodPost, "/api/ui/admin/emby/sync", map[string]any{}); response.Code != http.StatusForbidden {
			t.Fatal("non-admin started server export sync", response.Code)
		}
	}
	response := admin.request(t, http.MethodGet, "/api/ui/admin/emby", nil)
	viewerResultOK(t, response)
	if manager.document.Settings.Enabled {
		t.Fatal("automatic export enabled by default")
	}
	if _, err := os.Stat(manager.path); !os.IsNotExist(err) {
		t.Fatal("reading settings created an export job")
	}
	input := map[string]any{"enabled": true, "outputDir": t.TempDir(), "baseUrl": "file:///private/fixture", "intervalMinutes": 60, "following": true, "downloads": false}
	if result := admin.request(t, http.MethodPost, "/api/ui/admin/emby", input); result.Code != http.StatusBadRequest {
		t.Fatal("invalid playback address accepted", result.Code)
	}
	input["baseUrl"] = "http://library.test"
	input["intervalMinutes"] = 1
	if result := admin.request(t, http.MethodPost, "/api/ui/admin/emby", input); result.Code != http.StatusBadRequest {
		t.Fatal("unbounded refresh frequency accepted", result.Code)
	}
	input["intervalMinutes"] = 60
	input["enabled"] = false
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/emby", input))
	if result := admin.request(t, http.MethodPost, "/api/ui/admin/emby/sync", map[string]any{}); result.Code != http.StatusConflict {
		t.Fatal("disabled sync executed", result.Code)
	}
	input["enabled"] = true
	input["following"] = false
	input["mergedOnly"] = true
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/emby", input))
	var saved embySyncDocument
	if err := readEmbyJSON(manager.path, &saved, 8<<20); err != nil || !saved.Settings.MergedOnly || saved.Settings.Following {
		t.Fatal("全集模式没有经设置接口持久化", err)
	}
}

func TestEmbySyncPartialFailurePreservesFilesAndRetriesScan(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	owner := app.browserViewers().accountStore().state.Accounts["admin"]
	var scans atomic.Int32
	emby := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := scans.Add(1)
		if count == 1 {
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(204)
	}))
	defer emby.Close()
	manager.document.Settings = embySyncSettings{Enabled: true, OutputDir: t.TempDir(), BaseURL: "http://library.test", IntervalMinutes: 60, Following: true, ServerURL: emby.URL, APIKey: "scan-secret"}
	manager.document.OwnerID = owner.ID
	manager.runOnce(context.Background())
	if !manager.document.RefreshPending || !strings.Contains(manager.document.Error, "HTTP 503") {
		t.Fatal("failed scan was not retained for retry", manager.document.Error)
	}
	entry := manager.document.Entries[historyFixtureDramaID]
	before, _ := os.ReadFile(filepath.Join(manager.document.Settings.OutputDir, entry.Folder, "Season 01", "S01E001.strm"))
	manager.runOnce(context.Background())
	if manager.document.RefreshPending || scans.Load() != 2 || manager.document.WrittenFiles != 0 {
		t.Fatal("scan retry rewrote files or was skipped")
	}
	client := app.downloader.hongguoClient()
	broken := client.details["7000000000000000001"]
	broken.Chapters = nil
	client.details["7000000000000000001"] = broken
	entry.CheckedAt = time.Time{}
	manager.document.Entries[historyFixtureDramaID] = entry
	manager.runOnce(context.Background())
	if manager.document.Failed != 1 || manager.document.Entries[historyFixtureDramaID].Episodes != 3 {
		t.Fatal("upstream failure lost previous imported episodes")
	}
	after, _ := os.ReadFile(filepath.Join(manager.document.Settings.OutputDir, entry.Folder, "Season 01", "S01E001.strm"))
	if string(before) != string(after) {
		t.Fatal("failed source response overwrote completed export")
	}
	state, _ := json.Marshal(manager.view())
	if strings.Contains(string(state), "scan-secret") {
		t.Fatal("status leaked API key")
	}
}

func TestEmbySyncDownloadedCollectionAndLocalPlaybackPriority(t *testing.T) {
	app, _ := administratorFixture(t)
	owner := app.browserViewers().accountStore().state.Accounts["admin"]
	cached := app.downloader.hongguoClient().details["7000000000000000001"]
	path := filepath.Join(t.TempDir(), "local-fixture.mp4")
	if err := os.WriteFile(path, []byte("synthetic file identity only"), 0600); err != nil {
		t.Fatal(err)
	}
	task := Task{DramaID: historyFixtureDramaID, DramaTitle: "下载完成的合成剧", Chapter: cached.Chapters[0], OutPath: path, Index: 1, Total: 3}
	app.tasks = map[string]*UITask{"download-fixture": {ID: "download-fixture", DramaID: historyFixtureDramaID, DramaTitle: task.DramaTitle, Path: path, Status: uiStatusSuccess, Source: task}}
	manager := app.embySyncer()
	manager.document.OwnerID = owner.ID
	manager.document.Settings = embySyncSettings{Enabled: true, OutputDir: t.TempDir(), BaseURL: "http://library.test", IntervalMinutes: 60, Downloads: true}
	manager.runOnce(context.Background())
	if manager.document.Error != "" || manager.document.Entries[historyFixtureDramaID].Episodes != 3 {
		t.Fatal("download collection not imported", manager.document.Error)
	}
	local, downloadID, err := app.embyTask(context.Background(), historyFixtureDramaID, task.Chapter.ID)
	if err != nil || downloadID != "download-fixture" || local.OutPath != path {
		t.Fatal("Emby did not prefer completed local file", downloadID, err)
	}
	firstRun := manager.document.LastFinishedAt
	nextRun := manager.document.NextRunAt
	manager.runOnce(context.Background())
	if manager.document.LastFinishedAt != firstRun || manager.document.NextRunAt.After(nextRun.Add(time.Second)) {
		t.Fatal("unrelated notification postponed scheduled updates")
	}
}

func TestEmbySyncStartupDoesNotImmediatelyRepeatFailedScan(t *testing.T) {
	app, admin := administratorFixture(t)
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/following", map[string]any{"dramaId": historyFixtureDramaID, "saved": true}))
	scans := make(chan struct{}, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { scans <- struct{}{}; w.WriteHeader(503) }))
	defer server.Close()
	started := time.Now()
	input := map[string]any{"enabled": true, "outputDir": t.TempDir(), "baseUrl": "http://library.test", "intervalMinutes": 60, "following": true, "downloads": false, "serverUrl": server.URL, "apiKey": "fixture-scan-key"}
	viewerResultOK(t, admin.request(t, http.MethodPost, "/api/ui/admin/emby", input))
	result := waitEmbySync(t, manager, started)
	if result.Succeeded != 1 || !result.RefreshPending || !strings.Contains(result.Error, "HTTP 503") {
		t.Fatal("failed scan status not preserved", result.Error)
	}
	<-scans
	select {
	case <-scans:
		t.Fatal("startup and settings wake triggered duplicate immediate scan")
	case <-time.After(500 * time.Millisecond):
	}
}
