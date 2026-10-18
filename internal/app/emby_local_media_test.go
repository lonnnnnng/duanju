package app

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func syncEmbyLegacyMergedFilesForTest(ctx context.Context, settings embySyncSettings, drama Drama, merge UIMergeState, folder string, key []byte, owner string) (string, int, int, error) {
	// long: 旧版清单与 HTTP STRM 必须由独立样本构造，才能验证升级不会把历史文件误认为新格式。
	if folder == "" {
		folder = embyFolderName(drama)
	}
	root := filepath.Join(settings.OutputDir, folder)
	if err := os.MkdirAll(filepath.Join(root, "Season 01"), 0755); err != nil {
		return folder, 0, 0, err
	}
	if err := writeEmbyMergedFiles(drama, merge, settings.BaseURL, key, owner, func(name string, body []byte) error {
		return os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), body, 0664)
	}); err != nil {
		return folder, 0, 0, err
	}
	err := writeEmbyJSON(filepath.Join(root, ".duanju-emby.json"), embyFolderManifest{Version: 1, DramaID: drama.ID, Mode: "merged"}, 0600)
	return folder, 3, 1, err
}

func localEmbyFixture(t *testing.T, mode os.FileMode) (embySyncSettings, Drama, UIMergeState, []byte) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "全集.mp4")
	if err := os.WriteFile(source, []byte("complete video bytes"), mode); err != nil {
		t.Fatal(err)
	}
	settings := embySyncSettings{OutputDir: t.TempDir(), MergedOnly: true, IntervalMinutes: 60}
	drama := Drama{ID: historyFixtureDramaID, Title: "本地全集测试"}
	merge := UIMergeState{DramaID: drama.ID, DramaTitle: drama.Title, Status: "success", StartEpisode: 1, EndEpisode: 3, OutputPath: source}
	return settings, drama, merge, bytes.Repeat([]byte{6}, 32)
}

func TestEmbyLocalMergedPublicationAndUpdate(t *testing.T) {
	for _, mode := range []os.FileMode{0640, 0600} {
		t.Run(mode.String(), func(t *testing.T) {
			settings, drama, merge, key := localEmbyFixture(t, mode)
			folder, written, count, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
			if err != nil || written != 3 || count != 1 {
				t.Fatal("本地全集发布失败", written, count, err)
			}
			root := filepath.Join(settings.OutputDir, folder)
			media := filepath.Join(root, filepath.FromSlash(embyLocalMergedName))
			sourceInfo, _ := os.Stat(merge.OutputPath)
			targetInfo, err := os.Stat(media)
			if err != nil || targetInfo.Mode().Perm()&0040 == 0 || os.SameFile(sourceInfo, targetInfo) != (mode&0040 != 0) || sourceInfo.Mode().Perm() != mode {
				t.Fatal("硬链接、复制或共享组权限不正确", err)
			}
			if _, written, _, err = syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err != nil || written != 0 {
				t.Fatal("重复同步改写已入库内容", written, err)
			}
			// long: 重新合并替换源路径后，Emby 的稳定路径必须更新到新视频，不能一直播放旧硬链接。
			replacement := merge.OutputPath + ".replacement"
			if err = os.WriteFile(replacement, []byte("new complete video"), mode); err != nil {
				t.Fatal(err)
			}
			if err = os.Rename(replacement, merge.OutputPath); err != nil {
				t.Fatal(err)
			}
			if _, written, _, err = syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err != nil || written != 1 {
				t.Fatal("重新合并没有更新 MP4", written, err)
			}
			if body, err := os.ReadFile(media); err != nil || string(body) != "new complete video" {
				t.Fatal("Emby 仍读取旧全集", err)
			}
			if removed, retired, err := retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key); err != nil || !retired || removed != 3 {
				t.Fatal("删除全集后没有回收入库文件", removed, retired, err)
			}
			if _, err := os.Stat(merge.OutputPath); err != nil {
				t.Fatal("撤下 Emby 入库副本误删源视频", err)
			}
		})
	}
}

func TestEmbyLocalMergedMigratesLegacySTRMAndPreservesIdentity(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	settings.BaseURL = "https://old-library.test"
	folder, _, _, err := syncEmbyLegacyMergedFilesForTest(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	settings.BaseURL = ""
	if _, _, _, err = syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(settings.OutputDir, folder)
	if links := embyTestSTRMs(t, filepath.Join(root, "Season 01")); len(links) != 0 {
		t.Fatal("迁移后仍有 HTTP STRM", links)
	}
	nfo, err := os.ReadFile(filepath.Join(root, "tvshow.nfo"))
	if err != nil || strings.Contains(string(nfo), "/api/emby/") || !strings.Contains(string(nfo), embyMetadataID(key, drama.ID)) {
		t.Fatal("迁移失去原剧集身份或继续依赖播放服务", err)
	}
	var manifest embyFolderManifest
	if err = readEmbyJSON(filepath.Join(root, ".duanju-emby.json"), &manifest, 8<<20); err != nil || manifest.Mode != embyLocalMergedMode || manifest.Media == nil || manifest.PendingMedia != nil {
		t.Fatal("本地发布身份没有持久化", err)
	}
}

func TestEmbyLocalMergedProtectsUserFilesAndCancelledMigration(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	settings.BaseURL = "https://old-library.test"
	folder, _, _, err := syncEmbyLegacyMergedFilesForTest(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(settings.OutputDir, folder)
	media := filepath.Join(root, filepath.FromSlash(embyLocalMergedName))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err = syncEmbyMergedFiles(ctx, settings, drama, merge, folder, key, "owner"); !errors.Is(err, context.Canceled) {
		t.Fatal("取消后仍执行迁移", err)
	}
	if len(embyTestSTRMs(t, filepath.Dir(media))) != 1 {
		t.Fatal("失败迁移撤掉了旧入口")
	}
	if err = os.WriteFile(media, []byte("user video"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err == nil {
		t.Fatal("同名用户视频没有阻止迁移")
	}
	if body, err := os.ReadFile(media); err != nil || string(body) != "user video" || len(embyTestSTRMs(t, filepath.Dir(media))) != 1 {
		t.Fatal("冲突迁移改动了用户视频或旧 STRM", err)
	}
}

func TestEmbyLocalMergedProtectsChangedPublishedFile(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(settings.OutputDir, folder, filepath.FromSlash(embyLocalMergedName))
	if err = os.WriteFile(media, []byte("user replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err == nil {
		t.Fatal("用户替换的 MP4 被覆盖")
	}
	if _, _, err = retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key); err == nil {
		t.Fatal("用户替换的 MP4 被清理")
	}
	if body, err := os.ReadFile(media); err != nil || string(body) != "user replacement" {
		t.Fatal("用户视频没有保留", err)
	}
}

func TestEmbyLocalMergedRecoversPendingPublication(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{false: "发布前中断", true: "发布后中断"}[published], func(t *testing.T) {
			settings, drama, merge, key := localEmbyFixture(t, 0600)
			folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
			if err != nil {
				t.Fatal(err)
			}
			root := filepath.Join(settings.OutputDir, folder)
			marker := filepath.Join(root, ".duanju-emby.json")
			var manifest embyFolderManifest
			if err = readEmbyJSON(marker, &manifest, 8<<20); err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(merge.OutputPath, []byte("recovered new video"), 0600); err != nil {
				t.Fatal(err)
			}
			manifest.PendingMedia, _, err = hashEmbyLocalFile(context.Background(), merge.OutputPath, embyLocalMergedName)
			if err != nil {
				t.Fatal(err)
			}
			if err = writeEmbyJSON(marker, manifest, 0600); err != nil {
				t.Fatal(err)
			}
			media := filepath.Join(root, filepath.FromSlash(embyLocalMergedName))
			if published {
				if err = os.WriteFile(media, []byte("recovered new video"), 0640); err != nil {
					t.Fatal(err)
				}
			}
			if _, written, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err != nil || written < 1 {
				t.Fatal("中断发布没有恢复并安排重新扫描", written, err)
			}
			var restored embyFolderManifest
			if err = readEmbyJSON(marker, &restored, 8<<20); err != nil || restored.PendingMedia != nil || restored.Media == nil {
				t.Fatal("恢复后仍残留待发布状态", err)
			}
		})
	}
}

func TestEmbyLocalPostersRetryWithoutServerAndPreserveCustomArtwork(t *testing.T) {
	app, _ := lanFixture(t)
	settings, drama, merge, _ := localEmbyFixture(t, 0600)
	app.cfg.OutputDir = filepath.Dir(merge.OutputPath)
	app.merges = map[string]*UIMergeState{drama.ID: &merge}
	app.dramas[0].CoverURL = embyFixtureCover
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	manager.document.Settings = settings
	manager.document.Settings.Enabled = true
	manager.document.ViewerID = newLANTestBrowser(t, app).id
	data, failing, requests := embySyntheticPoster(t), true, 0
	app.downloader.client.Transport = embyPosterTestTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		if failing {
			return nil, errors.New("synthetic image failure")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: request}, nil
	})
	manager.runOnce(context.Background())
	entry := manager.document.Entries[drama.ID]
	if entry.Episodes != 1 || entry.Poster.Status != "failed" || entry.Poster.RetryAt.IsZero() {
		t.Fatal("海报失败影响视频或没有安排重试", entry, manager.document.Error)
	}
	failing = false
	entry.Poster.CheckedAt, entry.Poster.RetryAt = time.Time{}, time.Time{}
	manager.document.Entries[drama.ID] = entry
	manager.runOnce(context.Background())
	entry = manager.document.Entries[drama.ID]
	root := filepath.Join(settings.OutputDir, entry.Folder)
	poster := filepath.Join(root, "poster.png")
	if body, err := os.ReadFile(poster); err != nil || !bytes.Equal(body, data) || entry.Poster.Status != "synced" || manager.document.Error != "" {
		t.Fatal("没有连接 Emby 时无法生成本地海报", entry.Poster.Status, err, manager.document.Error)
	}
	var restored embySyncDocument
	if err := readEmbyJSON(manager.path, &restored, 8<<20); err != nil {
		t.Fatal(err)
	}
	before := requests
	entry.Poster.CheckedAt = time.Time{}
	restored.Entries[drama.ID] = entry
	manager.document = restored
	manager.runOnce(context.Background())
	if requests != before {
		t.Fatal("重启后重复抓取已有完整海报")
	}
	if err := os.WriteFile(poster, []byte("custom image"), 0600); err != nil {
		t.Fatal(err)
	}
	entry = manager.document.Entries[drama.ID]
	entry.Poster.CheckedAt = time.Time{}
	manager.document.Entries[drama.ID] = entry
	manager.runOnce(context.Background())
	if manager.document.Entries[drama.ID].Poster.Status != "existing" {
		t.Fatal("用户替换海报没有被识别")
	}
	key, _ := app.embySigningKey(false)
	if _, retired, err := retireEmbyInactiveFiles(context.Background(), settings, drama.ID, entry.Folder, key); err != nil || !retired {
		t.Fatal("本地全集无法撤下", err)
	}
	if body, err := os.ReadFile(poster); err != nil || string(body) != "custom image" {
		t.Fatal("撤下全集误删自定义海报", err)
	}
}

func TestEmbyLocalMergedVideoSurvivesServiceStop(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	mergeFixtureCommand(t, ffmpeg, "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=10", "-t", "0.5", "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", merge.OutputPath)
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	app, _ := lanFixture(t)
	server := httptest.NewServer(app.routes())
	server.Close()
	// long: 服务关闭且原下载路径消失后仍实际解析入库 MP4，验证播放源确实是独立文件而不是 HTTP 地址或软链接。
	if err = os.Remove(merge.OutputPath); err != nil {
		t.Fatal(err)
	}
	info, err := probeMergeMedia(context.Background(), ffmpeg, filepath.Join(settings.OutputDir, folder, filepath.FromSlash(embyLocalMergedName)))
	if err != nil || info.duration <= 0 {
		t.Fatal("服务停止后入库视频不能独立读取", err)
	}
}

func TestEmbyLocalSettingsDoNotRequirePlaybackAddress(t *testing.T) {
	settings := embySyncSettings{Enabled: true, OutputDir: t.TempDir(), MergedOnly: true, IntervalMinutes: 60}
	if err := settings.normalize(); err != nil {
		t.Fatal("本地全集仍要求 HTTP 地址", err)
	}
	settings.MergedOnly, settings.Downloads = false, true
	if err := settings.normalize(); err == nil {
		t.Fatal("在线分集模式缺少播放地址仍被接受")
	}
}

func TestEmbyLocalScanFailureRetainsFilesAndRetries(t *testing.T) {
	app, _ := lanFixture(t)
	settings, drama, merge, _ := localEmbyFixture(t, 0600)
	app.cfg.OutputDir = filepath.Dir(merge.OutputPath)
	app.merges = map[string]*UIMergeState{drama.ID: &merge}
	app.dramas[0].CoverURL = embyFixtureCover
	data := embySyntheticPoster(t)
	var images, scans atomic.Int32
	var fail atomic.Bool
	fail.Store(true)
	app.downloader.client.Transport = embyPosterTestTransport(func(request *http.Request) (*http.Response, error) {
		images.Add(1)
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(data)), Request: request}, nil
	})
	folder := embyFolderName(app.dramas[0])
	root := filepath.Join(settings.OutputDir, folder)
	emby := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		scans.Add(1)
		if request.URL.Path != "/Library/Refresh" || request.Header.Get("X-Emby-Token") != "synthetic-api-key" {
			t.Error("扫描请求无效")
		}
		for _, name := range []string{embyLocalMergedName, "poster.png"} {
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(name))); err != nil {
				t.Error("扫描早于完整媒体或海报发布", name, err)
			}
		}
		if fail.Load() {
			http.Error(writer, "synthetic unavailable", http.StatusServiceUnavailable)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer emby.Close()
	manager := app.embySyncer()
	t.Cleanup(manager.stop)
	settings.Enabled, settings.ServerURL, settings.APIKey = true, emby.URL, "synthetic-api-key"
	manager.document.Settings = settings
	manager.document.ViewerID = newLANTestBrowser(t, app).id
	manager.runOnce(context.Background())
	if !manager.document.RefreshPending || manager.document.Entries[drama.ID].Episodes != 1 || scans.Load() != 1 || images.Load() != 1 {
		t.Fatal("扫描失败丢失完整媒体或重试待办", manager.document.Error)
	}
	fail.Store(false)
	manager.runOnce(context.Background())
	if manager.document.RefreshPending || manager.document.Error != "" || scans.Load() != 2 || images.Load() != 1 {
		t.Fatal("扫描重试重新下载海报或未清掉待办", manager.document.Error)
	}
}

func TestEmbyLocalDirectoryPreservesPrivateSharedPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permissions")
	}
	root := t.TempDir()
	if err := os.Chmod(root, os.ModeSetgid|0770); err != nil {
		t.Fatal(err)
	}
	if err := ensureSharedMediaDirectory(root); err != nil {
		t.Fatal(err)
	}
	if err := ensureEmbyDirectory(root); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(root)
	if err != nil || info.Mode().Perm() != 0770 || info.Mode()&os.ModeSetgid == 0 {
		t.Fatal("共享目录的 2770 权限被放宽或丢失 SGID", err)
	}
}

func TestEmbyLocalMergedSwitchBackWithdrawsExportOnly(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0640)
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	settings.MergedOnly, settings.BaseURL = false, "http://library.test"
	if _, _, _, err = syncEmbyDramaFiles(context.Background(), settings, drama, embySyncTestChapters(), folder, key, "owner"); err != nil {
		t.Fatal(err)
	}
	season := filepath.Join(settings.OutputDir, folder, "Season 01")
	if len(embyTestSTRMs(t, season)) != 2 {
		t.Fatal("切回在线分集没有生成对应入口")
	}
	if _, err := os.Stat(filepath.Join(season, "S01E001.mp4")); !os.IsNotExist(err) {
		t.Fatal("切回分集后仍保留重复 MP4", err)
	}
	if _, err := os.Stat(merge.OutputPath); err != nil {
		t.Fatal("切换模式误删原始全集", err)
	}
}

func TestEmbyLocalMigrationProtectsModifiedSTRM(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	settings.BaseURL = "http://old-library.test"
	folder, _, _, err := syncEmbyLegacyMergedFilesForTest(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(settings.OutputDir, folder)
	link := filepath.Join(root, "Season 01", "S01E001.strm")
	if err = os.WriteFile(link, []byte("https://user.example/video.mp4\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err == nil {
		t.Fatal("用户修改的 STRM 没有阻止迁移")
	}
	if body, err := os.ReadFile(link); err != nil || string(body) != "https://user.example/video.mp4\n" {
		t.Fatal("用户 STRM 被覆盖", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(embyLocalMergedName))); !os.IsNotExist(err) {
		t.Fatal("冲突迁移生成了重复视频", err)
	}
}

func TestEmbyLocalPostersRecoverPublishedImageWithoutNetwork(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(settings.OutputDir, folder)
	marker := filepath.Join(root, ".duanju-emby.json")
	var manifest embyFolderManifest
	if err = readEmbyJSON(marker, &manifest, 8<<20); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "poster.png"), embySyntheticPoster(t), 0660); err != nil {
		t.Fatal(err)
	}
	manifest.PendingPoster, _, err = hashEmbyLocalFile(context.Background(), filepath.Join(root, "poster.png"), "poster.png")
	if err != nil {
		t.Fatal(err)
	}
	if err = writeEmbyJSON(marker, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	app, _ := lanFixture(t)
	app.downloader.client.Transport = embyPosterTestTransport(func(request *http.Request) (*http.Response, error) {
		t.Error("恢复已发布海报不应访问网络")
		return nil, errors.New("network forbidden")
	})
	poster := embyPosterState{SourceURL: embyFixtureCover, SyncedSource: embyPosterSourceHash(embyFixtureCover)}
	if written, custom, err := app.embySyncer().syncLocalPoster(context.Background(), settings, drama.ID, folder, &poster); err != nil || custom || written < 1 {
		t.Fatal("无法恢复本地海报并重新安排扫描", written, custom, err)
	}
	var restored embyFolderManifest
	if err = readEmbyJSON(marker, &restored, 8<<20); err != nil || restored.PendingPoster != nil || restored.Poster == nil {
		t.Fatal("海报恢复没有提交清单", err)
	}
}

func TestEmbyLocalPosterConvertsGIFToPortableJPEG(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	app, _ := lanFixture(t)
	// long: 通用账号夹具使用测试程序模拟 FFmpeg，这项转换验收必须调用实际二进制。
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: ffmpeg}}
	var buffer bytes.Buffer
	if err := gif.Encode(&buffer, image.NewPaletted(image.Rect(0, 0, 16, 16), color.Palette{color.Black, color.White}), nil); err != nil {
		t.Fatal(err)
	}
	data, name, err := app.localEmbyPosterImage(context.Background(), buffer.Bytes())
	if err != nil || name != "poster.jpg" || imageContentType(data) != "image/jpeg" {
		t.Fatal("GIF 海报没有转换为本地 JPEG", name, err)
	}
}

func TestEmbyLocalRetirePreservesEmptyUserPoster(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(settings.OutputDir, folder)
	marker := filepath.Join(root, ".duanju-emby.json")
	var manifest embyFolderManifest
	if err = readEmbyJSON(marker, &manifest, 8<<20); err != nil {
		t.Fatal(err)
	}
	poster := filepath.Join(root, "poster.png")
	if err = os.WriteFile(poster, embySyntheticPoster(t), 0660); err != nil {
		t.Fatal(err)
	}
	manifest.Poster, _, err = hashEmbyLocalFile(context.Background(), poster, "poster.png")
	if err != nil {
		t.Fatal(err)
	}
	if err = writeEmbyJSON(marker, manifest, 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.Truncate(poster, 0); err != nil {
		t.Fatal(err)
	}
	if _, retired, err := retireEmbyInactiveFiles(context.Background(), settings, drama.ID, folder, key); err != nil || !retired {
		t.Fatal("用户清空的海报阻止全集正常撤下", err)
	}
	if info, err := os.Stat(poster); err != nil || info.Size() != 0 {
		t.Fatal("用户清空的海报没有保留", err)
	}
}

func TestEmbyLocalMergedRejectsUnsafeMediaAndReceipts(t *testing.T) {
	for _, kind := range []string{"source-symlink", "target-symlink", "invalid-receipt"} {
		t.Run(kind, func(t *testing.T) {
			settings, drama, merge, key := localEmbyFixture(t, 0600)
			original := merge.OutputPath
			folder := ""
			if kind == "source-symlink" {
				merge.OutputPath = filepath.Join(filepath.Dir(original), "link.mp4")
				if err := os.Symlink(original, merge.OutputPath); err != nil {
					t.Skip("无法创建符号链接", err)
				}
			} else {
				var err error
				folder, _, _, err = syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
				if err != nil {
					t.Fatal(err)
				}
				root := filepath.Join(settings.OutputDir, folder)
				if kind == "target-symlink" {
					media := filepath.Join(root, filepath.FromSlash(embyLocalMergedName))
					if err = os.Remove(media); err != nil {
						t.Fatal(err)
					}
					if err = os.Symlink(original, media); err != nil {
						t.Skip("无法创建符号链接", err)
					}
				} else {
					marker := filepath.Join(root, ".duanju-emby.json")
					var manifest embyFolderManifest
					if err = readEmbyJSON(marker, &manifest, 8<<20); err != nil {
						t.Fatal(err)
					}
					manifest.Media.Name = "../user.mp4"
					if err = writeEmbyJSON(marker, manifest, 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			if _, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err == nil {
				t.Fatal("不安全的媒体路径或清单未阻止发布")
			}
			if body, err := os.ReadFile(original); err != nil || string(body) != "complete video bytes" {
				t.Fatal("拒绝发布时改动了源视频", err)
			}
		})
	}
}
