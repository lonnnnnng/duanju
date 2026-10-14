package app

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestRetryAndResumeDoNotStartWhenStateSaveFails(t *testing.T) {
	for _, test := range []struct {
		name, route, status string
	}{
		{"retry", "/api/ui/tasks/retry", uiStatusFailed},
		{"resume", "/api/ui/tasks/resume", uiStatusPaused},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, admin, _, _, _ := deleteFixture(t)
			task := app.tasks["episode-1"]
			task.Status = test.status
			task.Phase = test.status
			before := *task
			order := append([]string(nil), app.taskOrder...)
			app.statePath = app.cfg.OutputDir
			response := admin.request(t, http.MethodPost, test.route, map[string]any{"ids": []string{task.ID}})
			if response.Code != http.StatusInternalServerError || !reflect.DeepEqual(*task, before) || !reflect.DeepEqual(app.taskOrder, order) {
				t.Fatal("failed persistence changed queued tasks", response.Code, response.Body.String())
			}
		})
	}
}

func TestPauseAndCancelDoNotStopTasksWhenStateSaveFails(t *testing.T) {
	for _, action := range []string{"pause", "cancel"} {
		for _, status := range []string{uiStatusRunning, uiStatusParsing} {
			t.Run(action+"/"+status, func(t *testing.T) {
				app, admin, _, _, _ := deleteFixture(t)
				task := app.tasks["episode-1"]
				task.Status, task.Phase = status, status
				before := *task
				canceled := false
				if status == uiStatusParsing {
					app.parsingCancels = map[string]context.CancelFunc{task.DramaID: func() { canceled = true }}
				} else {
					app.runningCancels[task.ID] = func() { canceled = true }
				}
				app.statePath = app.cfg.OutputDir
				response := admin.request(t, http.MethodPost, "/api/ui/tasks/"+action, map[string]any{"ids": []string{task.ID}})
				if response.Code != http.StatusInternalServerError || canceled || !reflect.DeepEqual(*task, before) {
					t.Fatal("状态保存失败却中断或改写了任务", response.Code, canceled, task.Status, response.Body.String())
				}
			})
		}
	}
}

func TestPauseAndCancelStopOnlyAfterStateIsSaved(t *testing.T) {
	for _, action := range []string{"pause", "cancel"} {
		t.Run(action, func(t *testing.T) {
			app, admin, _, _, _ := deleteFixture(t)
			task := app.tasks["episode-1"]
			task.Status = uiStatusRunning
			canceled := false
			app.runningCancels[task.ID] = func() { canceled = true }
			response := admin.request(t, http.MethodPost, "/api/ui/tasks/"+action, map[string]any{"ids": []string{task.ID}})
			if response.Code != http.StatusOK || !canceled || action == "pause" && !task.PauseRequested || action == "cancel" && !task.CancelRequested {
				t.Fatal("状态保存成功后没有中断运行任务", response.Code, canceled, task, response.Body.String())
			}
		})
	}
}

func TestClearDoesNotStopOrRemoveTasksWhenStateSaveFails(t *testing.T) {
	app, admin, _, _, _ := deleteFixture(t)
	running := app.tasks["episode-1"]
	running.Status = uiStatusRunning
	before := *running
	canceled := false
	app.runningCancels[running.ID] = func() { canceled = true }
	app.statePath = app.cfg.OutputDir
	response := admin.request(t, http.MethodPost, "/api/ui/tasks/clear", map[string]any{"ids": []string{"episode-1", "episode-2"}})
	if response.Code != http.StatusInternalServerError || canceled || !reflect.DeepEqual(*running, before) || len(app.tasks) != 2 {
		t.Fatal("清理状态保存失败却停止或移除了任务", response.Code, canceled, response.Body.String())
	}
}

func TestParsedEpisodesDoNotEnterQueueWhenStateSaveFails(t *testing.T) {
	app := viewerTestApp(t)
	app.cond = sync.NewCond(&app.mu)
	app.tasks = map[string]*UITask{}
	app.merges = map[string]*UIMergeState{}
	drama := app.dramas[0]
	placeholder, _ := app.addParsingPlaceholderTaskLocked(drama, 0)
	app.statePath = app.cfg.OutputDir
	views, failed, message := app.enqueueDramas(context.Background(), []Drama{drama}, false, 0)
	if len(views) != 0 || len(failed) != 1 || message == "" || len(app.tasks) != 1 || len(app.taskOrder) != 1 || app.taskOrder[0] != placeholder.ID {
		t.Fatal("分集状态未落盘却进入了下载队列", views, failed, message, app.taskOrder)
	}
	if task := app.tasks[placeholder.ID]; task == nil || task.Status != uiStatusFailed {
		t.Fatal("解析占位任务没有留下可重试状态", task)
	}
}

func TestTaskFinishRetryPersistsCompletedState(t *testing.T) {
	app, _, _, _, _ := deleteFixture(t)
	task := app.tasks["episode-1"]
	task.Status, task.Phase = uiStatusSuccess, "completed"
	app.statePath = app.cfg.OutputDir
	app.mu.Lock()
	firstErr := app.saveStateLockedWithDiagnostic("ui.task_finish_save_failed")
	app.mu.Unlock()
	if firstErr == nil {
		t.Fatal("测试夹具没有触发首次保存失败")
	}
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	if err := app.retryTaskFinishSave(); err != nil {
		t.Fatal("数据目录恢复后仍未补写完成状态", err)
	}
	restarted := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restarted.loadState()
	completed := false
	for _, saved := range restarted.tasks {
		if saved.Path == task.Path && saved.Status == uiStatusSuccess && saved.Phase == "completed" {
			completed = true
		}
	}
	if !completed {
		t.Fatal("重启后没有恢复已补写的完成状态")
	}
}

func TestCanceledIntentDoesNotRestartDownloadsAfterProcessRestart(t *testing.T) {
	for _, status := range []string{uiStatusRunning, uiStatusParsing} {
		t.Run(status, func(t *testing.T) {
			app, _, _, _, _ := deleteFixture(t)
			task := app.tasks["episode-1"]
			task.Status, task.Phase = status, status
			task.CancelRequested = true
			app.mu.Lock()
			err := app.saveStateLocked()
			app.mu.Unlock()
			if err != nil {
				t.Fatal(err)
			}
			restarted := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
			restarted.loadState()
			var restored *UITask
			for _, candidate := range restarted.tasks {
				if candidate.Path == task.Path {
					restored = candidate
					break
				}
			}
			// long: 取消请求已确认写入磁盘时，即使下载协程没来得及记录终态，重启也只能恢复为已取消。
			if restored == nil || restored.Status != uiStatusCanceled || restored.Phase != "canceled" || restored.CancelRequested || restored.Error != "已取消" {
				t.Fatal("重启后丢失取消意图", restored)
			}
		})
	}
}

func TestPausedIntentClearsProgressAfterProcessRestart(t *testing.T) {
	app, _, _, _, _ := deleteFixture(t)
	task := app.tasks["episode-1"]
	task.Status, task.Phase = uiStatusRunning, "downloading"
	task.PauseRequested = true
	task.Progress, task.DownloadedBytes = 65, 650
	task.ElapsedSeconds, task.MediaElapsedSeconds = 30, 24
	task.SpeedBytesPerSecond, task.RemainingSeconds = 25, 15
	app.mu.Lock()
	err := app.saveStateLocked()
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	restarted := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restarted.loadState()
	for _, restored := range restarted.tasks {
		if restored.Path != task.Path {
			continue
		}
		// long: 暂停后的临时文件不再保留，重启恢复时不能显示可续传的旧进度。
		if restored.Status != uiStatusPaused || restored.PauseRequested || restored.Progress != 0 || restored.DownloadedBytes != 0 ||
			restored.ElapsedSeconds != 0 || restored.MediaElapsedSeconds != 0 || restored.SpeedBytesPerSecond != 0 || restored.RemainingSeconds != 0 {
			t.Fatal("暂停意图恢复后仍显示旧进度", restored)
		}
		return
	}
	t.Fatal("重启后未找到暂停任务")
}

func TestPlaceholderRetryAndResumeRollbackOnStateSaveFailure(t *testing.T) {
	for _, test := range []struct {
		name, route string
		prepare     func(*UIApp, Drama) *UITask
	}{
		{"retry", "/api/ui/tasks/retry", func(app *UIApp, drama Drama) *UITask {
			view, _ := app.addFailedPlaceholderTaskLocked(drama, errors.New("章节获取失败"), 0)
			return app.tasks[view.ID]
		}},
		{"resume", "/api/ui/tasks/resume", func(app *UIApp, drama Drama) *UITask {
			task := newUITask(app.placeholderTask(drama, "parsing"), app.merges[drama.ID].UpdatedAt)
			task.Status, task.Phase = uiStatusPaused, "paused"
			app.tasks[task.ID] = task
			app.taskOrder = append(app.taskOrder, task.ID)
			return task
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, admin, _, _, _ := deleteFixture(t)
			app.tasks = map[string]*UITask{}
			app.taskOrder = nil
			drama := Drama{ID: historyFixtureDramaID, Title: "测试剧", Source: sourceHongguo}
			task := test.prepare(app, drama)
			before := *task
			order := append([]string(nil), app.taskOrder...)
			app.statePath = app.cfg.OutputDir
			response := admin.request(t, http.MethodPost, test.route, map[string]any{"ids": []string{task.ID}})
			if response.Code != http.StatusInternalServerError || app.tasks[task.ID] != task || !reflect.DeepEqual(*task, before) ||
				!reflect.DeepEqual(app.taskOrder, order) || len(app.parsingInFlight) != 0 || len(app.parsingCancels) != 0 {
				t.Fatal("failed persistence replaced placeholder or started parsing", response.Code, response.Body.String())
			}
		})
	}
}

func TestWorkerKeepsTaskQueuedWhenRunningStateCannotBeSaved(t *testing.T) {
	app := viewerTestApp(t)
	app.cfg.Concurrency = 1
	app.statePath = app.cfg.OutputDir
	app.cond = sync.NewCond(&app.mu)
	app.tasks = map[string]*UITask{}
	app.runningCancels = map[string]context.CancelFunc{}
	path := filepath.Join(app.cfg.OutputDir, "worker-save-failure.mp4")
	task := newUITask(Task{DramaID: historyFixtureDramaID, DramaTitle: "测试剧", OutPath: path,
		Chapter: Chapter{ID: "hongguo:test:1", Source: sourceHongguo}}, time.Now())
	app.tasks[task.ID] = task
	app.taskOrder = []string{task.ID}
	app.startWorkers()
	defer app.stopWorkers()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		failed := app.lastError != ""
		app.mu.Unlock()
		if failed {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	app.mu.Lock()
	defer app.mu.Unlock()
	if app.lastError == "" || task.Status != uiStatusQueued || task.Attempt != 0 || app.activeDownloads != 0 || len(app.runningCancels) != 0 {
		t.Fatal("worker started a download without persisting its running state", task.Status, task.Attempt, app.lastError)
	}
}
