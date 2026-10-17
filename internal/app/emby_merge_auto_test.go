package app

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmbyLocalNewBackgroundMergeAutomaticallyPublishes(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg unavailable")
	}
	for _, remove := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep-episodes", true: "clean-episodes"}[remove], func(t *testing.T) {
			app, admin := administratorFixture(t)
			app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
			app.cfg.SkipBytes = 1
			app.downloader.cfg.FFmpeg = ffmpeg
			app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: ffmpeg}}
			app.tasks = make(map[string]*UITask)
			app.dramas[0].CoverURL = embyFixtureCover
			drama := app.dramas[0]
			directory := filepath.Join(app.cfg.OutputDir, "new-drama")
			if err := ensureSharedMediaDirectory(directory); err != nil {
				t.Fatal(err)
			}
			poster := embySyntheticPoster(t)
			app.downloader.client.Transport = embyPosterTestTransport(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(poster)), Request: request}, nil
			})
			var paths []string
			for index, id := range []string{"episode-1", "episode-2"} {
				path := filepath.Join(directory, id+".mp4")
				mergeFixtureCommand(t, ffmpeg, "-f", "lavfi", "-i", "color=c=blue:s=160x90:r=10", "-t", "0.5", "-an", "-c:v", "libx264", "-pix_fmt", "yuv420p", path)
				task := Task{DramaID: drama.ID, DramaTitle: drama.Title, Index: index + 1, Total: 2, OutPath: path, Chapter: Chapter{ID: id, Source: sourceHongguo}}
				app.tasks[id] = &UITask{ID: id, DramaID: drama.ID, DramaTitle: drama.Title, Path: path, Status: uiStatusSuccess, Source: task}
				app.taskOrder = append(app.taskOrder, id)
				paths = append(paths, path)
			}
			output := filepath.Join(app.cfg.OutputDir, "Emby")
			root := filepath.Join(output, embyFolderName(drama))
			var scans atomic.Int32
			emby := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.Method != http.MethodPost || request.URL.Path != "/Library/Refresh" || request.Header.Get("X-Emby-Token") != "synthetic-api-key" {
					t.Error("合并后的自动扫描请求无效")
				}
				for _, name := range []string{embyLocalMergedName, "tvshow.nfo", "Season 01/S01E001.nfo", "poster.png"} {
					info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
					if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || runtime.GOOS != "windows" && info.Mode().Perm()&0040 == 0 {
						t.Error("扫描早于完整媒体发布，或文件不允许共享组读取", name, err)
					}
				}
				scans.Add(1)
				writer.WriteHeader(http.StatusNoContent)
			}))
			defer emby.Close()
			manager := app.embySyncer()
			manager.document.Settings = embySyncSettings{Enabled: true, MergedOnly: true, OutputDir: output, IntervalMinutes: 60, ServerURL: emby.URL, APIKey: "synthetic-api-key"}
			manager.document.OwnerID = app.browserViewers().accountStore().state.Accounts["admin"].ID
			manager.start()
			t.Cleanup(manager.stop)
			t.Cleanup(app.stopMergeJobs)
			// long: 从没有合并记录和入库条目的新剧开始，只提交后台合并；测试不能手动调用同步来掩盖通知链路断开。
			started := time.Now()
			response := admin.request(t, http.MethodPost, "/api/ui/merge", map[string]any{"dramaIds": []string{drama.ID}, "async": true, "deleteEpisodes": remove})
			if response.Code != http.StatusAccepted {
				t.Fatal("后台合并提交失败", response.Code, response.Body.String())
			}
			result := waitEmbySync(t, manager, started)
			entry := result.Entries[drama.ID]
			if result.Error != "" || result.Failed != 0 || result.RefreshPending || entry.Episodes != 1 || entry.Poster.Status != "synced" || scans.Load() < 1 {
				t.Fatal("新剧合并后没有自动发布视频、海报并通知扫描", result.Error, entry.Error, entry.Poster.Status, scans.Load())
			}
			app.stopMergeJobs()
			app.mu.Lock()
			merge := *app.merges[drama.ID]
			app.mu.Unlock()
			if merge.Status != "success" || len(merge.CleanupPending) != 0 || merge.DeleteEpisodes != remove {
				t.Fatal("合并或分集清理未完成", merge.Status, merge.Error)
			}
			for _, path := range paths {
				_, err := os.Stat(path)
				if remove && !os.IsNotExist(err) || !remove && err != nil {
					t.Fatal("分集清理状态不符合选择", remove, err)
				}
			}
			info, err := probeMergeMedia(context.Background(), ffmpeg, filepath.Join(root, filepath.FromSlash(embyLocalMergedName)))
			if err != nil || info.duration < 900*time.Millisecond {
				t.Fatal("自动入库的全集不是可播放的完整合并结果", err, info.duration)
			}
			if runtime.GOOS != "windows" {
				parent, _ := os.Stat(output)
				for _, path := range []string{root, filepath.Join(root, "Season 01")} {
					info, err := os.Stat(path)
					if err != nil || info.Mode()&os.ModeSetgid == 0 || info.Mode().Perm()&0070 != 0070 || !sameEmbyLocalGroup(parent, info) {
						t.Fatal("全新入库目录没有保留可读写共享组和 SGID", path, err)
					}
				}
			}
		})
	}
}

func TestEmbyLocalMergeNotificationRetriesInterruptedScan(t *testing.T) {
	app, _ := administratorFixture(t)
	settings, drama, merge, _ := localEmbyFixture(t, 0600)
	app.cfg.OutputDir = filepath.Dir(merge.OutputPath)
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.merges = map[string]*UIMergeState{drama.ID: &merge}
	started := make(chan struct{})
	var scans atomic.Int32
	emby := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if scans.Add(1) == 1 {
			close(started)
			select {
			case <-request.Context().Done():
			case <-time.After(8 * time.Second):
				t.Error("合并状态更新没有取消上一轮扫描")
			}
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer emby.Close()
	manager := app.embySyncer()
	settings.Enabled, settings.ServerURL, settings.APIKey = true, emby.URL, "synthetic-api-key"
	manager.document.Settings = settings
	manager.document.OwnerID = app.browserViewers().accountStore().state.Accounts["admin"].ID
	manager.start()
	t.Cleanup(manager.stop)
	select {
	case <-started:
	case <-time.After(8 * time.Second):
		t.Fatal("第一轮同步没有自动进入扫描")
	}
	// long: 清理分集会再次保存合并成功状态；即使首轮文件已完整发布，取消的扫描也必须交给下一轮继续完成。
	after := time.Now()
	if err := app.setMergeState(uiMergeResult{DramaID: drama.ID, DramaTitle: drama.Title, OutputPath: merge.OutputPath, StartEpisode: 1, EndEpisode: 3, Merged: 3}, "success", 100, false, true); err != nil {
		t.Fatal(err)
	}
	result := waitEmbySync(t, manager, after)
	if result.Error != "" || result.RefreshPending || result.Entries[drama.ID].Episodes != 1 || scans.Load() != 2 {
		t.Fatal("上一轮已发布文件，但合并通知取消后漏掉了扫描重试", result.Error, result.RefreshPending, scans.Load())
	}
}

func TestEmbyLocalUncommittedPublicationScansAfterRestart(t *testing.T) {
	app, _ := administratorFixture(t)
	settings, drama, merge, _ := localEmbyFixture(t, 0600)
	app.dramas = []Drama{drama}
	app.cfg.OutputDir = filepath.Dir(merge.OutputPath)
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	if err := app.setMergeState(uiMergeResult{DramaID: drama.ID, DramaTitle: drama.Title, OutputPath: merge.OutputPath, StartEpisode: 1, EndEpisode: 3, Merged: 3}, "success", 100, false, false); err != nil {
		t.Fatal(err)
	}
	var scans atomic.Int32
	emby := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		scans.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer emby.Close()
	manager := app.embySyncer()
	settings.Enabled, settings.ServerURL, settings.APIKey = true, emby.URL, "synthetic-api-key"
	manager.document.Settings = settings
	manager.document.OwnerID = app.browserViewers().accountStore().state.Accounts["admin"].ID
	if err := writeEmbyJSON(manager.path, manager.document, 0600); err != nil {
		t.Fatal(err)
	}
	key, err := app.embySigningKey(true)
	if err != nil {
		t.Fatal(err)
	}
	// long: 模拟 MP4 和 NFO 已发布、条目收据尚未落盘就退出；重启必须补扫已有文件，不能只等待下一次文件变化。
	if _, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, manager.document.OwnerID); err != nil {
		t.Fatal(err)
	}
	restarted := &UIApp{cfg: app.cfg, downloader: app.downloader, dramas: app.dramas, statePath: app.statePath, tasks: map[string]*UITask{}}
	restarted.loadState()
	next := restarted.embySyncer()
	started := time.Now()
	next.start()
	t.Cleanup(next.stop)
	result := waitEmbySync(t, next, started)
	if result.Error != "" || result.WrittenFiles != 0 || result.Entries[drama.ID].Episodes != 1 || result.RefreshPending || scans.Load() != 1 {
		t.Fatal("重启后没有为已发布但未确认的新剧补扫", result.Error, result.WrittenFiles, scans.Load())
	}
}
