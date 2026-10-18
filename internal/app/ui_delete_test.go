package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func deleteFixture(t *testing.T) (*UIApp, *lanTestBrowser, string, string, string) {
	t.Helper()
	app, admin := lanFixture(t)
	app.tasks = map[string]*UITask{}
	app.taskOrder = nil
	app.merges = map[string]*UIMergeState{}
	app.runningCancels = map[string]context.CancelFunc{}
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	dramaDir := filepath.Join(app.cfg.OutputDir, "删除测试剧")
	if err := os.MkdirAll(dramaDir, 0755); err != nil {
		t.Fatal(err)
	}
	paths := []string{filepath.Join(dramaDir, "001.mp4"), filepath.Join(dramaDir, "002.mp4"), filepath.Join(dramaDir, "全集1-2.mp4")}
	for _, path := range paths {
		if err := os.WriteFile(path, []byte("video"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for index, id := range []string{"episode-1", "episode-2"} {
		source := Task{DramaID: historyFixtureDramaID, DramaTitle: "删除测试剧", OutPath: paths[index], Chapter: Chapter{ID: id, Source: sourceHongguo}}
		app.tasks[id] = &UITask{ID: id, DramaID: historyFixtureDramaID, DramaTitle: "删除测试剧", Title: id, Path: paths[index], Status: uiStatusSuccess, Source: source}
		app.taskOrder = append(app.taskOrder, id)
	}
	app.merges[historyFixtureDramaID] = &UIMergeState{DramaID: historyFixtureDramaID, DramaTitle: "删除测试剧", Status: "success", OutputPath: paths[2]}
	return app, admin, paths[0], paths[1], paths[2]
}

func TestDeleteDownloadsSeparatesEpisodesAndMergedFile(t *testing.T) {
	app, admin, first, second, merged := deleteFixture(t)
	part := strings.TrimSuffix(first, ".mp4") + ".part.mp4"
	if err := os.WriteFile(part, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}})
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"deletedFiles":2`) {
		t.Fatal("单集与临时文件没有一起删除", result.Code, result.Body.String())
	}
	for _, path := range []string{first, part} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("删除后文件仍存在", path, err)
		}
	}
	if _, err := os.Stat(second); err != nil || app.tasks["episode-2"] == nil || app.merges[historyFixtureDramaID] == nil {
		t.Fatal("删除单集影响其他分集或全集", err)
	}
	result = admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"mergedDramaIds": []string{historyFixtureDramaID}})
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"mergedRemoved":1`) {
		t.Fatal("全集删除失败", result.Code, result.Body.String())
	}
	if _, err := os.Stat(merged); !os.IsNotExist(err) || app.merges[historyFixtureDramaID] != nil {
		t.Fatal("全集文件或记录未删除", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatal("删除全集误删分集", err)
	}
	result = admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-2"}})
	if result.Code != http.StatusOK || len(app.tasks) != 0 {
		t.Fatal("最后一集删除失败", result.Code, result.Body.String())
	}
	if _, err := os.Stat(second); !os.IsNotExist(err) {
		t.Fatal("最后一集磁盘文件仍存在", err)
	}
	restored := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if len(restored.tasks) != 0 || len(restored.merges) != 0 {
		t.Fatal("删除后的记录在重启后复活")
	}
}

func TestDeleteDownloadsCleansOwnedDirectoryAfterLastResource(t *testing.T) {
	app, admin, first, _, _ := deleteFixture(t)
	directory := filepath.Dir(first)
	if err := os.WriteFile(filepath.Join(directory, ".drama-id"), []byte(historyFixtureDramaID), 0600); err != nil {
		t.Fatal(err)
	}
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{
		"ids": []string{"episode-1", "episode-2"}, "mergedDramaIds": []string{historyFixtureDramaID},
	})
	if result.Code != http.StatusOK || len(app.tasks) != 0 || len(app.merges) != 0 {
		t.Fatal("批量删除未完成", result.Code, result.Body.String())
	}
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatal("最后一个资源删除后仍残留受管空目录", err)
	}
	if _, err := os.Stat(app.cfg.OutputDir); err != nil {
		t.Fatal("误删了下载根目录", err)
	}
}

func TestDeleteDownloadsPreservesDirectoryWithUserFileOrWrongMarker(t *testing.T) {
	for _, test := range []struct {
		name     string
		marker   string
		userFile bool
	}{
		{name: "用户文件", marker: historyFixtureDramaID, userFile: true},
		{name: "不匹配标记", marker: "hongguo:another"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, admin, first, _, _ := deleteFixture(t)
			directory := filepath.Dir(first)
			marker := filepath.Join(directory, ".drama-id")
			if err := os.WriteFile(marker, []byte(test.marker), 0600); err != nil {
				t.Fatal(err)
			}
			if test.userFile {
				if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{
				"ids": []string{"episode-1", "episode-2"}, "mergedDramaIds": []string{historyFixtureDramaID},
			})
			if result.Code != http.StatusOK {
				t.Fatal(result.Code, result.Body.String())
			}
			if body, err := os.ReadFile(marker); err != nil || string(body) != test.marker {
				t.Fatal("不应回收的目录或标记被删除", err)
			}
		})
	}
}

func TestDeleteDownloadsPreservesDirectoryWithReferencedMissingFile(t *testing.T) {
	app, admin, first, second, merged := deleteFixture(t)
	directory := filepath.Dir(first)
	marker := filepath.Join(directory, ".drama-id")
	if err := os.WriteFile(marker, []byte(historyFixtureDramaID), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{second, merged} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}})
	if result.Code != http.StatusOK || app.tasks["episode-2"] == nil {
		t.Fatal(result.Code, result.Body.String())
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("仍有任务引用的目录被回收", err)
	}
}

func TestDeleteDownloadNotifiesEmbySync(t *testing.T) {
	app, admin, first, _, _ := deleteFixture(t)
	manager := app.embySyncer()
	manager.document.Settings.Enabled = true
	manager.document.Entries[historyFixtureDramaID] = embySyncEntry{Folder: "删除测试剧", CheckedAt: time.Now()}
	select {
	case <-manager.wake:
	default:
	}
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}})
	if result.Code != http.StatusOK {
		t.Fatal("删除分集失败", result.Code, result.Body.String())
	}
	select {
	case <-manager.wake:
	case <-time.After(time.Second):
		t.Fatal("删除分集后没有立即唤醒 Emby 清理")
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Fatal("分集文件仍存在", err)
	}
}

func TestDeleteDoesNotTouchFilesWhenIntentSaveFails(t *testing.T) {
	app, admin, first, second, merged := deleteFixture(t)
	app.tasks["episode-1"].Status = uiStatusRunning
	before := *app.tasks["episode-1"]
	canceled := false
	app.runningCancels["episode-1"] = func() { canceled = true }
	app.statePath = app.cfg.OutputDir
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{
		"ids": []string{"episode-1", "episode-2"}, "mergedDramaIds": []string{historyFixtureDramaID},
	})
	if result.Code != http.StatusInternalServerError || canceled || !reflect.DeepEqual(*app.tasks["episode-1"], before) ||
		len(app.tasks) != 2 || app.merges[historyFixtureDramaID].DeleteRequested {
		t.Fatal("删除意图保存失败却改动了任务或中断下载", result.Code, canceled, result.Body.String())
	}
	for _, path := range []string{first, second, merged} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("删除意图未保存却已删除文件", path, err)
		}
	}
}

func TestDeleteIntentContinuesAfterRestart(t *testing.T) {
	app, _, first, second, merged := deleteFixture(t)
	app.tasks["episode-1"].RemoveRequested = true
	app.tasks["episode-1"].DeleteFilesRequested = true
	app.merges[historyFixtureDramaID].DeleteRequested = true
	app.mu.Lock()
	err := app.saveStateLocked()
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	restarted := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restarted.loadState()
	if len(restarted.tasks) != 1 || restarted.merges[historyFixtureDramaID] != nil {
		t.Fatal("重启未完成已落盘的删除意图", len(restarted.tasks), restarted.merges[historyFixtureDramaID])
	}
	for _, path := range []string{first, merged} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("重启后待删文件仍存在", path, err)
		}
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatal("重启误删未选中的分集", err)
	}
}

func TestDeleteDownloadsBatchAndUnsafePathAbort(t *testing.T) {
	app, admin, first, second, merged := deleteFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	app.tasks["episode-2"].Path = outside
	app.tasks["episode-2"].Source.OutPath = outside
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1", "episode-2"}, "mergedDramaIds": []string{historyFixtureDramaID}})
	if result.Code != http.StatusConflict {
		t.Fatal("越界路径没有阻止整批删除", result.Code, result.Body.String())
	}
	for _, path := range []string{first, second, merged, outside} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("预检失败却删除了文件", path, err)
		}
	}
	app.tasks["episode-2"].Path = second
	app.tasks["episode-2"].Source.OutPath = second
	result = admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1", "episode-2"}, "mergedDramaIds": []string{historyFixtureDramaID}})
	if result.Code != http.StatusOK || len(app.tasks) != 0 || len(app.merges) != 0 {
		t.Fatal("整批删除未完成", result.Code, result.Body.String())
	}
	for _, path := range []string{first, second, merged} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("整批删除后文件仍存在", path, err)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatal("整批删除影响目录外文件", err)
	}
}

func TestDeleteRunningDownloadContinuesAfterRestart(t *testing.T) {
	app, admin, first, _, _ := deleteFixture(t)
	app.tasks["episode-1"].Status = uiStatusRunning
	canceled := false
	app.runningCancels["episode-1"] = func() { canceled = true }
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}})
	if result.Code != http.StatusOK || !strings.Contains(result.Body.String(), `"pending":1`) || !canceled {
		t.Fatal("运行任务没有先取消并等待收尾", result.Code, result.Body.String())
	}
	if _, err := os.Stat(first); err != nil || !app.tasks["episode-1"].DeleteFilesRequested {
		t.Fatal("协程退出前错误删除文件或丢失删除标记", err)
	}
	restored := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if _, err := os.Stat(first); !os.IsNotExist(err) || restored.tasks["episode-1"] != nil {
		t.Fatal("重启后未完成磁盘删除", err)
	}
}

func TestMergedDeleteFailureStillNotifiesEmby(t *testing.T) {
	app, admin, _, _, merged := deleteFixture(t)
	manager := app.embySyncer()
	manager.document.Settings.Enabled = true
	manager.document.Settings.MergedOnly = true
	manager.document.Entries[historyFixtureDramaID] = embySyncEntry{CheckedAt: time.Now()}
	dir := filepath.Dir(merged)
	if err := os.Chmod(dir, 0555); err != nil {
		t.Skipf("cannot make media directory read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"mergedDramaIds": []string{historyFixtureDramaID}})
	state := app.merges[historyFixtureDramaID]
	if state == nil {
		t.Skip("filesystem permits removing a file from a read-only directory")
	}
	if result.Code != http.StatusOK || !state.DeleteRequested || !strings.Contains(result.Body.String(), `"failed":1`) {
		t.Fatal("failed file deletion lost its saved intent", result.Code, result.Body.String(), state)
	}
	if !manager.document.Entries[historyFixtureDramaID].CheckedAt.IsZero() {
		t.Fatal("Emby was not notified after merged deletion intent was saved")
	}
	if _, err := os.Stat(merged); err != nil {
		t.Fatal("failed deletion unexpectedly removed the media file", err)
	}
}

func TestDeleteDownloadsRejectsActiveMerge(t *testing.T) {
	app, admin, first, _, merged := deleteFixture(t)
	app.mergeJobs = map[string]*uiMergeJob{historyFixtureDramaID: {dramaID: historyFixtureDramaID}}
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}, "mergedDramaIds": []string{historyFixtureDramaID}})
	if result.Code != http.StatusConflict || app.tasks["episode-1"] == nil || app.merges[historyFixtureDramaID] == nil {
		t.Fatal("合并进行中仍允许删除资源", result.Code, result.Body.String())
	}
	for _, path := range []string{first, merged} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("合并冲突时删除了文件", path, err)
		}
	}
}

func TestDeleteDownloadsPreservesSharedAndSymlinkTargets(t *testing.T) {
	app, admin, first, _, merged := deleteFixture(t)
	app.merges[historyFixtureDramaID].OutputPath = first
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}})
	if result.Code != http.StatusConflict {
		t.Fatal("全集仍引用的文件被单集删除", result.Code, result.Body.String())
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal("共享文件被删除", err)
	}
	app.merges[historyFixtureDramaID].OutputPath = merged
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, first); err != nil {
		t.Fatal(err)
	}
	result = admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}})
	if result.Code != http.StatusConflict {
		t.Fatal("软链接文件被当成下载资源删除", result.Code, result.Body.String())
	}
	if _, err := os.Lstat(first); err != nil {
		t.Fatal("软链接本身被删除", err)
	}
	if body, err := os.ReadFile(outside); err != nil || string(body) != "keep" {
		t.Fatal("软链接外部文件被修改", err)
	}
}

func TestDeleteDownloadsAfterOutputDirectoryChanged(t *testing.T) {
	app, admin, first, _, merged := deleteFixture(t)
	marker := filepath.Join(filepath.Dir(first), ".drama-id")
	if err := os.WriteFile(marker, []byte(historyFixtureDramaID), 0600); err != nil {
		t.Fatal(err)
	}
	app.cfg.OutputDir = t.TempDir()
	if err := os.WriteFile(marker, []byte("hongguo:wrong-drama"), 0600); err != nil {
		t.Fatal(err)
	}
	rejected := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}})
	if rejected.Code != http.StatusConflict {
		t.Fatal("原目录标记不匹配仍允许删除", rejected.Code, rejected.Body.String())
	}
	if err := os.WriteFile(marker, []byte(historyFixtureDramaID), 0600); err != nil {
		t.Fatal(err)
	}
	result := admin.request(t, http.MethodPost, "/api/ui/tasks/delete", map[string]any{"ids": []string{"episode-1"}, "mergedDramaIds": []string{historyFixtureDramaID}})
	if result.Code != http.StatusOK {
		t.Fatal("原保存目录的任务无法删除", result.Code, result.Body.String())
	}
	for _, path := range []string{first, merged} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("原目录文件仍存在", path, err)
		}
	}
}
