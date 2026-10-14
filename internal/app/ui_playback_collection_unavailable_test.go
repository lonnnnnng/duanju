package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCompletedEpisodeUnavailableDoesNotQueueUntilExplicitRetry(t *testing.T) {
	app, admin, first, _, _ := deleteFixture(t)
	app.cond = sync.NewCond(&app.mu)
	task := app.tasks["episode-1"]
	task.Source.Chapter.VideoURL = "https://example.com/episode.m3u8"
	task.Progress = 100
	task.Phase = "completed"
	before := *task
	offline := first + ".offline"
	if err := os.Rename(first, offline); err != nil {
		t.Fatal(err)
	}
	// long: 把测试文件临时移走模拟共享存储不可用；播放检查不能因此改写成功任务或自动发起新下载。
	if _, err := app.prepareCollectionDownload(context.Background(), task.ID); err == nil || !strings.Contains(err.Error(), "存储连接") || !reflect.DeepEqual(*task, before) {
		t.Fatal("暂不可访问的成功任务被自动重排队", err, task.Status)
	}
	views := app.taskViewsLocked()
	if !views[0].LocalFileUnavailable || !views[0].LocalFileRetryable || task.Status != uiStatusSuccess {
		t.Fatal("下载管理未只读提示本地文件不可访问", views[0])
	}
	response := admin.request(t, http.MethodGet, "/api/ui/tasks", nil)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"localFileUnavailable":true`) {
		t.Fatal("任务接口未返回本地可用性", response.Code, response.Body.String())
	}
	if err := os.Rename(offline, first); err != nil {
		t.Fatal(err)
	}
	preparation, err := app.prepareCollectionDownload(context.Background(), task.ID)
	if err != nil || preparation.Source != "local" || task.Status != uiStatusSuccess || app.taskViewsLocked()[0].LocalFileUnavailable {
		t.Fatal("共享存储恢复后未直接使用原本地文件", preparation, err, task.Status)
	}

	if err := os.Rename(first, offline); err != nil {
		t.Fatal(err)
	}
	app.statePath = app.cfg.OutputDir
	response = admin.request(t, http.MethodPost, "/api/ui/tasks/retry", map[string]any{"ids": []string{task.ID}})
	if response.Code != http.StatusInternalServerError || !reflect.DeepEqual(*task, before) {
		t.Fatal("重试意图保存失败却改变了成功任务", response.Code, task.Status, response.Body.String())
	}
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	response = admin.request(t, http.MethodPost, "/api/ui/tasks/retry", map[string]any{"ids": []string{task.ID}})
	if response.Code != http.StatusOK || task.Status != uiStatusQueued || task.Progress != 0 || task.DownloadedBytes != 0 {
		t.Fatal("手动重试未重新排队", response.Code, task.Status, response.Body.String())
	}
}

func TestUnavailableCompletedEpisodeCannotRetryToUnmanagedPath(t *testing.T) {
	app, admin, _, _, _ := deleteFixture(t)
	task := app.tasks["episode-1"]
	task.Source.Chapter.VideoURL = "https://example.com/episode.m3u8"
	task.Source.OutPath = filepath.Join(t.TempDir(), "outside.mp4")
	task.Path = task.Source.OutPath
	before := *task
	view := app.taskViewsLocked()[0]
	if !view.LocalFileUnavailable || view.LocalFileRetryable {
		t.Fatal("受管理目录外的旧路径仍允许重新下载", view)
	}
	response := admin.request(t, http.MethodPost, "/api/ui/tasks/retry", map[string]any{"ids": []string{task.ID}})
	if response.Code != http.StatusOK || !reflect.DeepEqual(*task, before) {
		t.Fatal("受管理目录外的重试改动了任务", response.Code, task.Status)
	}
}
