package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

func autoMergeFixture(t *testing.T) *UIApp {
	t.Helper()
	app, session := nativePlaybackFixture(t, "0.5", "160x90")
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.taskOrder = append([]string(nil), session.downloadIDs...)
	app.cfg.SkipBytes = 1
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: app.downloader.cfg.FFmpeg}}
	app.autoMerges = map[string]*UIAutoMergeState{historyFixtureDramaID: {Enabled: true, RequiredTaskIDs: append([]string(nil), app.taskOrder...), Generation: 1}}
	return app
}

func TestAutoMergeInvalidVideoStopsUntilRepair(t *testing.T) {
	app := autoMergeFixture(t)
	path := app.tasks["one"].Path
	if err := os.WriteFile(path, []byte("broken video"), 0600); err != nil {
		t.Fatal(err)
	}
	app.mergeMu.Lock()
	app.checkAutoMerges()
	app.mu.Lock()
	job := app.mergeJobs[historyFixtureDramaID]
	app.mu.Unlock()
	app.mergeMu.Unlock()
	if job == nil {
		t.Fatal("未提交自动校验")
	}
	select {
	case <-job.done:
	case <-time.After(30 * time.Second):
		t.Fatal("自动校验未结束")
	}
	app.mu.Lock()
	if state := app.autoMerges[historyFixtureDramaID]; state.Status != "error" || state.Error == "" {
		t.Fatal("坏分集没有显示异常")
	}
	app.mu.Unlock()
	app.checkAutoMerges()
	app.mu.Lock()
	if len(app.mergeJobs) != 0 {
		t.Fatal("重复轮询重试同一坏分集")
	}
	app.autoMergeDownloadFinishedLocked(app.tasks["one"])
	if state := app.autoMerges[historyFixtureDramaID]; state.SubmittedGeneration != 0 || state.Status != "waiting" {
		t.Fatal("修复后没有恢复自动意图")
	}
	app.mu.Unlock()
	body, err := os.ReadFile(path)
	if err != nil || string(body) != "broken video" {
		t.Fatal("校验失败删除了分集", err)
	}
}

func TestAutoMergeRequiresWholeCollectionAndSuccessfulSave(t *testing.T) {
	for _, kind := range []string{"missing-first", "missing-last", "paused", "parsing", "total-mismatch", "save-failure"} {
		t.Run(kind, func(t *testing.T) {
			app := autoMergeFixture(t)
			original := append([]string(nil), app.autoMerges[historyFixtureDramaID].RequiredTaskIDs...)
			switch kind {
			case "missing-first":
				delete(app.tasks, "one")
			case "missing-last":
				delete(app.tasks, "three")
			case "paused":
				app.tasks["three"].Status = uiStatusPaused
			case "parsing":
				app.parsingInFlight = map[string]bool{historyFixtureDramaID: true}
			case "total-mismatch":
				app.tasks["three"].Source.Total = 4
			case "save-failure":
				app.statePath = app.cfg.OutputDir
			}
			app.checkAutoMerges()
			if len(app.mergeJobs) != 0 || app.autoMerges[historyFixtureDramaID].SubmittedGeneration != 0 || !reflect.DeepEqual(original, app.autoMerges[historyFixtureDramaID].RequiredTaskIDs) {
				t.Fatal("缺集或保存失败仍提交自动合并")
			}
		})
	}
}

func TestAutoMergeRestartAndCancelPreserveIntent(t *testing.T) {
	app := autoMergeFixture(t)
	app.mergeMu.Lock()
	defer app.mergeMu.Unlock()
	app.checkAutoMerges()
	job := app.mergeJobs[historyFixtureDramaID]
	if job == nil || job.autoGeneration != 1 || len(job.tasks) != 3 || job.remove {
		t.Fatal("完整集合没有提交保留分集的自动合并")
	}
	app.checkAutoMerges()
	if app.mergeJobs[historyFixtureDramaID] != job {
		t.Fatal("重复检查生成多个合并任务")
	}
	restored := &UIApp{cfg: app.cfg, downloader: app.downloader, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	state := restored.autoMerges[historyFixtureDramaID]
	if state == nil || !state.Enabled || state.SubmittedGeneration != 0 || len(state.RequiredTaskIDs) != 3 {
		t.Fatal("重启没有恢复未完成的自动合并意图", state)
	}
	w := historyJSONRequest(t, app, app.handleMergeCancel, "/api/ui/merge/cancel", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	if app.autoMerges[historyFixtureDramaID].Enabled {
		t.Fatal("用户取消后仍开启自动合并")
	}
	select {
	case <-job.done:
	case <-time.After(30 * time.Second):
		t.Fatal("用户取消未结束排队合并")
	}
	restored.loadState()
	if restored.autoMerges[historyFixtureDramaID].Enabled {
		t.Fatal("重启丢失用户取消意图")
	}
	app.checkAutoMerges()
	if len(app.mergeJobs) != 0 {
		t.Fatal("取消后自动重新提交")
	}
}

func TestAutoMergeNormalStopRestoresAndWaitingUsesRequiredCollection(t *testing.T) {
	app := autoMergeFixture(t)
	app.autoMerges[historyFixtureDramaID].SubmittedGeneration = 1
	app.mergeJobs = map[string]*uiMergeJob{historyFixtureDramaID: {autoGeneration: 1}}
	app.mergeStopped = true
	res := uiMergeResult{DramaID: historyFixtureDramaID}
	app.recordMergeError(&res, context.Canceled, false)
	if !res.ResumeAutomatic || app.merges[historyFixtureDramaID].Status != "queued" {
		t.Fatal("正常停服丢失自动合并")
	}
	restored := &UIApp{cfg: app.cfg, downloader: app.downloader, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if restored.autoMerges[historyFixtureDramaID].SubmittedGeneration != 0 {
		t.Fatal("重启没有恢复自动意图")
	}
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "ignore-extra", true: "missing-edge"}[missing], func(t *testing.T) {
			restored := autoMergeFixture(t)
			restored.merges = map[string]*UIMergeState{historyFixtureDramaID: {DramaID: historyFixtureDramaID, Status: "queued", WaitingForSpace: true, Automatic: true}}
			extra := cloneUITask(restored.tasks["three"])
			extra.ID = "old-four"
			extra.Source.Index = 4
			extra.Source.Total = 4
			restored.tasks[extra.ID] = extra
			restored.taskOrder = append(restored.taskOrder, extra.ID)
			if missing {
				delete(restored.tasks, "three")
			}
			restored.mergeMu.Lock()
			restored.restoreWaitingMergesLocked()
			job := restored.mergeJobs[historyFixtureDramaID]
			if missing {
				if job != nil {
					t.Fatal("恢复把缺集的连续子集当全集")
				}
			} else {
				if job == nil || len(job.tasks) != 3 || job.tasks[2].ID != "three" || job.autoGeneration != 1 {
					t.Fatal("恢复合入额外旧分集")
				}
				job.cancel()
			}
			restored.mergeMu.Unlock()
			if job != nil {
				select {
				case <-job.done:
				case <-time.After(30 * time.Second):
					t.Fatal("恢复任务未退出")
				}
			}
		})
	}
}

func TestAutoMergeExistingReceiptProbeNormalStopResumes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	app := autoMergeFixture(t)
	tasks := []*UITask{app.tasks["one"], app.tasks["two"], app.tasks["three"]}
	snapshot, err := collectAutoMergeInputs(context.Background(), tasks)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(filepath.Dir(tasks[0].Path), mergeOutputName(tasks[0].DramaTitle, 1, 3))
	stage := output + ".part.mp4"
	body, err := os.ReadFile(tasks[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stage, body, 0664); err != nil {
		t.Fatal(err)
	}
	if err := prepareAutoMergedReceipt(context.Background(), historyFixtureDramaID, output, stage, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := publishMergedFile(stage, output); err != nil {
		t.Fatal(err)
	}
	// long: 用进入校验的标记控制停服时机，避免只验证提前取消而漏掉已有全集的 probe 分支。
	program := filepath.Join(t.TempDir(), "ffmpeg-probe")
	if err := os.WriteFile(program, []byte("#!/bin/sh\n: > \"$0.started\"\nexec sleep 30\n"), 0700); err != nil {
		t.Fatal(err)
	}
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: program}}
	app.autoMerges[historyFixtureDramaID].SubmittedGeneration = 1
	app.mergeJobs = map[string]*uiMergeJob{historyFixtureDramaID: {autoGeneration: 1}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan uiMergeResult, 1)
	go func() {
		finished <- app.mergeDrama(context.WithValue(ctx, autoMergeContextKey{}, snapshot), historyFixtureDramaID, tasks, false)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(program + ".started"); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("已有全集校验未启动")
		}
		time.Sleep(time.Millisecond)
	}
	app.mu.Lock()
	app.mergeStopped = true
	app.mu.Unlock()
	cancel()
	select {
	case result := <-finished:
		state := app.merges[historyFixtureDramaID]
		if !result.ResumeAutomatic || state == nil || state.Status != "queued" || !state.Automatic {
			t.Fatal("已有全集校验停服后丢失自动恢复意图", result, state)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("停服未结束已有全集校验")
	}
	restored := &UIApp{cfg: app.cfg, downloader: app.downloader, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if state := restored.autoMerges[historyFixtureDramaID]; state == nil || !state.Enabled || state.SubmittedGeneration != 0 {
		t.Fatal("重启没有恢复已有全集校验的自动意图", state)
	}
	if err := verifyAutoMergedReceipt(context.Background(), historyFixtureDramaID, output, snapshot); err != nil {
		t.Fatal("停服损坏了可恢复的全集收据", err)
	}
}

func TestAutoMergeDisableWaitingPersistsBeforeCancel(t *testing.T) {
	for _, saveFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved", true: "save-failed"}[saveFailure], func(t *testing.T) {
			app, admin, _, _, _ := deleteFixture(t)
			t.Cleanup(app.stopWorkers)
			app.autoMerges = map[string]*UIAutoMergeState{historyFixtureDramaID: {Enabled: true, Generation: 1, SubmittedGeneration: 1, RequiredTaskIDs: append([]string(nil), app.taskOrder...)}}
			app.merges[historyFixtureDramaID] = &UIMergeState{DramaID: historyFixtureDramaID, Status: "queued", WaitingForSpace: true, Automatic: true}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			job := &uiMergeJob{dramaID: historyFixtureDramaID, ctx: ctx, cancel: cancel, done: make(chan struct{}), autoGeneration: 1}
			app.mergeJobs = map[string]*uiMergeJob{historyFixtureDramaID: job}
			beforeAuto, beforeMerges := cloneAutoMergeStates(app.autoMerges), cloneMergeStates(app.merges)
			if saveFailure {
				app.statePath = app.cfg.OutputDir
			}
			response := admin.request(t, http.MethodPost, "/api/ui/merge/auto", map[string]any{"dramaIds": []string{historyFixtureDramaID}, "enabled": false})
			app.mu.Lock()
			if saveFailure {
				unchanged := response.Code == http.StatusInternalServerError && reflect.DeepEqual(beforeAuto, app.autoMerges) && reflect.DeepEqual(beforeMerges, app.merges) && app.mergeJobs[historyFixtureDramaID] == job && !job.userCanceled && ctx.Err() == nil
				app.mu.Unlock()
				if !unchanged {
					t.Fatal("关闭设置保存失败仍取消了等待合并或丢失状态", response.Code)
				}
				select {
				case <-job.done:
					t.Fatal("保存失败仍结束了排队合并")
				default:
				}
				return
			}
			state := app.merges[historyFixtureDramaID]
			closed := response.Code == http.StatusOK && !app.autoMerges[historyFixtureDramaID].Enabled && state.Status == "failed" && !state.WaitingForSpace && !state.ResumeAutomatic && app.mergeJobs[historyFixtureDramaID] == nil && job.userCanceled && ctx.Err() != nil
			app.mu.Unlock()
			if !closed {
				t.Fatal("关闭设置没有取消等待合并", response.Code)
			}
			select {
			case <-job.done:
			default:
				t.Fatal("关闭后排队合并未结束")
			}
			restored := &UIApp{cfg: app.cfg, downloader: app.downloader, statePath: app.statePath, tasks: map[string]*UITask{}}
			restored.loadState()
			restored.restoreWaitingMergesLocked()
			if restored.autoMerges[historyFixtureDramaID].Enabled || len(restored.mergeJobs) != 0 || restored.merges[historyFixtureDramaID].WaitingForSpace {
				t.Fatal("重启恢复了已经关闭的等待合并")
			}
		})
	}
}

func TestAutoMergeRestoreWaitingRejectsDisabledOrMissingIntent(t *testing.T) {
	for _, missing := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "missing"}[missing], func(t *testing.T) {
			app := autoMergeFixture(t)
			if missing {
				delete(app.autoMerges, historyFixtureDramaID)
			} else {
				app.autoMerges[historyFixtureDramaID].Enabled = false
			}
			// long: 即使旧空间等待记录仍在，也不能把已关闭或丢失意图的自动任务当普通合并恢复。
			app.merges = map[string]*UIMergeState{historyFixtureDramaID: {DramaID: historyFixtureDramaID, Status: "queued", WaitingForSpace: true, ResumeAutomatic: true, Automatic: true}}
			app.restoreWaitingMergesLocked()
			state := app.merges[historyFixtureDramaID]
			if len(app.mergeJobs) != 0 || state.Status != "failed" || state.WaitingForSpace || state.ResumeAutomatic {
				t.Fatal("无自动意图的空间等待降级成普通合并", state)
			}
		})
	}
}

func TestAutoMergeDisableRunningSpaceWaitCannotResume(t *testing.T) {
	app := autoMergeFixture(t)
	t.Cleanup(app.stopWorkers)
	t.Cleanup(app.stopMergeJobs)
	app.downloader.spaceCheck = func(string, uint64) error { return spaceFailure() }
	app.checkAutoMerges()
	app.mu.Lock()
	job := app.mergeJobs[historyFixtureDramaID]
	app.mu.Unlock()
	if job == nil {
		t.Fatal("完整分集未提交自动合并")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		app.mu.Lock()
		state := app.merges[historyFixtureDramaID]
		waiting := job.running && state != nil && state.WaitingForSpace
		app.mu.Unlock()
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("自动合并未进入运行中的空间等待")
		}
		time.Sleep(time.Millisecond)
	}
	response := historyJSONRequest(t, app, app.handleAutoMerge, "/api/ui/merge/auto", map[string]any{"dramaIds": []string{historyFixtureDramaID}, "enabled": false})
	if response.Code != http.StatusOK {
		t.Fatal(response.Code, response.Body.String())
	}
	select {
	case <-job.done:
	case <-time.After(5 * time.Second):
		t.Fatal("关闭自动合并后运行中的空间等待没有退出")
	}
	app.mu.Lock()
	state := app.merges[historyFixtureDramaID]
	closed := !app.autoMerges[historyFixtureDramaID].Enabled && state.Status == "failed" && !state.WaitingForSpace && !state.ResumeAutomatic && len(app.mergeJobs) == 0
	app.mu.Unlock()
	if !closed {
		t.Fatal("运行中的空间等待回调覆盖了关闭意图")
	}
	restored := &UIApp{cfg: app.cfg, downloader: app.downloader, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	restored.restoreWaitingMergesLocked()
	if restored.autoMerges[historyFixtureDramaID].Enabled || len(restored.mergeJobs) != 0 {
		t.Fatal("重启继续运行已关闭的空间等待")
	}
}

func TestAutoMergeCanceledStateRejectsStaleRecoveryCallback(t *testing.T) {
	app := autoMergeFixture(t)
	app.autoMerges[historyFixtureDramaID].Enabled = false
	app.mergeJobs = map[string]*uiMergeJob{historyFixtureDramaID: {autoGeneration: 1, userCanceled: true}}
	// long: 模拟错误回调已读取停服快照、但用户关闭先落盘的交错；旧快照不能重新写出恢复意图。
	stale := uiMergeResult{DramaID: historyFixtureDramaID, ResumeAutomatic: true, WaitingForSpace: true, Detail: "服务停止，启动后继续自动合并"}
	if err := app.setMergeState(stale, "queued", 0, false, true); err != nil {
		t.Fatal(err)
	}
	state := app.merges[historyFixtureDramaID]
	if state.Status != "failed" || state.ResumeAutomatic || state.WaitingForSpace || state.DeleteEpisodes || !state.Automatic {
		t.Fatal("旧错误快照覆盖了用户取消意图", state)
	}
	restored := &UIApp{cfg: app.cfg, downloader: app.downloader, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if state := restored.merges[historyFixtureDramaID]; state.Status != "failed" || state.ResumeAutomatic || state.WaitingForSpace {
		t.Fatal("取消后的状态落盘仍保留恢复意图", state)
	}
}

func TestAutoMergeReceiptRejectsForeignAndChangedInputs(t *testing.T) {
	app := autoMergeFixture(t)
	output := filepath.Join(filepath.Dir(app.tasks["one"].Path), mergeOutputName(app.tasks["one"].DramaTitle, 1, 3))
	body, err := os.ReadFile(app.tasks["one"].Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, body, 0664); err != nil {
		t.Fatal(err)
	}
	snapshot, err := collectAutoMergeInputs(context.Background(), []*UITask{app.tasks["one"], app.tasks["two"], app.tasks["three"]})
	if err != nil {
		t.Fatal(err)
	}
	res := app.mergeDrama(context.WithValue(context.Background(), autoMergeContextKey{}, snapshot), historyFixtureDramaID, []*UITask{app.tasks["one"], app.tasks["two"], app.tasks["three"]}, false)
	if res.OK || res.Error == "" {
		t.Fatal("可播放的单集被当作自动全集")
	}
	stage := filepath.Join(filepath.Dir(output), "new-complete.part.mp4")
	if err := os.WriteFile(stage, body, 0664); err != nil {
		t.Fatal(err)
	}
	ownedOutput := output + ".owned.mp4"
	if err := prepareAutoMergedReceipt(context.Background(), historyFixtureDramaID, ownedOutput, stage, snapshot); err != nil {
		t.Fatal(err)
	}
	if err := publishMergedFile(stage, ownedOutput); err != nil {
		t.Fatal(err)
	}
	if err := verifyAutoMergedReceipt(context.Background(), historyFixtureDramaID, ownedOutput, snapshot); err != nil {
		t.Fatal("已记录的发布不能恢复", err)
	}
	changed := *snapshot
	changed.Digest = "different-inputs"
	if err := verifyAutoMergedReceipt(context.Background(), historyFixtureDramaID, ownedOutput, &changed); err == nil {
		t.Fatal("新分集复用旧全集")
	}
	current, err := os.ReadFile(output)
	if err != nil || !reflect.DeepEqual(current, body) {
		t.Fatal("拒绝复用却覆盖旧文件", err)
	}
	if _, err := app.deleteManagedPath(ownedOutput, historyFixtureDramaID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(ownedOutput + ".duanju-merge.json"); !os.IsNotExist(err) {
		t.Fatal("删除全集遗留自己的合并收据", err)
	}
}

func TestAutoMergeSettingAndTaskRemovalRollback(t *testing.T) {
	for _, operation := range []string{"setting", "clear", "delete", "cancel"} {
		t.Run(operation, func(t *testing.T) {
			app, admin, _, _, _ := deleteFixture(t)
			app.autoMerges = map[string]*UIAutoMergeState{historyFixtureDramaID: {Enabled: true, Generation: 1, RequiredTaskIDs: []string{"episode-1", "episode-2"}}}
			if operation == "cancel" {
				app.tasks["episode-1"].Status = uiStatusQueued
			}
			before := cloneAutoMergeStates(app.autoMerges)
			app.statePath = app.cfg.OutputDir
			path := "/api/ui/tasks/" + operation
			body := map[string]any{"ids": []string{"episode-1"}}
			if operation == "setting" {
				path = "/api/ui/merge/auto"
				body = map[string]any{"dramaIds": []string{historyFixtureDramaID}, "enabled": false}
			}
			response := admin.request(t, http.MethodPost, path, body)
			if response.Code != http.StatusInternalServerError || !reflect.DeepEqual(before, app.autoMerges) {
				t.Fatal("持久化失败丢失自动合并意图", response.Code)
			}
		})
	}
}
