package app

import (
	"context"
	"duanju/internal/diskspace"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func spaceFailure() error {
	return &diskspace.WaitingError{Available: 1, Required: diskspace.MinFreeBytes}
}

func TestWorkerSpaceWaitAndTimedRecovery(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "2", "320x180")
	app.cfg.Concurrency = 1
	app.cfg.SkipBytes = 1
	app.downloader.cfg = app.cfg
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: app.cfg.FFmpeg}}
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.runningCancels = map[string]context.CancelFunc{}
	app.cond = sync.NewCond(&app.mu)
	var low atomic.Bool
	low.Store(true)
	app.downloader.spaceCheck = func(string, uint64) error {
		if low.Load() {
			return spaceFailure()
		}
		return nil
	}
	source := fixture.tasks[0]
	data, err := os.ReadFile(source.OutPath)
	if err != nil {
		t.Fatal(err)
	}
	source.OutPath = filepath.Join(filepath.Dir(source.OutPath), "recovery.mp4")
	task := newUITask(source, time.Now())
	app.tasks = map[string]*UITask{task.ID: task}
	app.taskOrder = []string{task.ID}
	app.startWorkers()
	defer app.stopWorkers()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		waiting := task.Phase == "waiting-space"
		app.mu.Unlock()
		if waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}
	app.mu.Lock()
	if task.Phase != "waiting-space" || task.Status != uiStatusQueued || task.Attempt != 0 || app.activeDownloads != 0 {
		app.mu.Unlock()
		t.Fatal("low space consumed a worker or attempt")
	}
	app.mu.Unlock()
	if err := os.WriteFile(source.OutPath, data, 0644); err != nil {
		t.Fatal(err)
	}
	low.Store(false)
	app.mu.Lock()
	task.NextSpaceCheck = time.Now().Add(30 * time.Millisecond)
	app.cond.Broadcast()
	app.mu.Unlock()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		done := task.Status == uiStatusSuccess
		app.mu.Unlock()
		if done {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("space retry timer failed to resume the queued task")
}

func TestSpaceWaitingMergeSurvivesStopAndRestartsAutomatically(t *testing.T) {
	app, fixture := nativePlaybackFixture(t, "2", "320x180")
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.cfg.SkipBytes = 1
	app.downloader.cfg = app.cfg
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: app.cfg.FFmpeg}}
	var low atomic.Bool
	low.Store(true)
	app.downloader.spaceCheck = func(string, uint64) error {
		if low.Load() {
			return spaceFailure()
		}
		return nil
	}
	source := fixture.tasks[0]
	task := newUITask(source, time.Now())
	task.Status = uiStatusSuccess
	app.tasks = map[string]*UITask{task.ID: task}
	app.taskOrder = []string{task.ID}
	app.mu.Lock()
	jobs, err := app.enqueueMergesLocked([]string{source.DramaID}, true)
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		app.mu.Lock()
		state := app.merges[source.DramaID]
		waiting := state != nil && state.WaitingForSpace
		app.mu.Unlock()
		if waiting {
			break
		}
		time.Sleep(time.Millisecond)
	}
	app.stopMergeJobs()
	state := app.merges[source.DramaID]
	if state == nil || state.Status != "queued" || !state.WaitingForSpace || !state.DeleteEpisodes || !jobs[0].result.WaitingForSpace {
		t.Fatal("shutdown lost waiting merge intent", state, jobs[0].result)
	}
	low.Store(false)
	restored := NewUIApp(app.downloader)
	defer restored.stopWorkers()
	defer restored.stopMergeJobs()
	defer restored.closePlaybacks()
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		restored.mu.Lock()
		state = restored.merges[source.DramaID]
		complete := state != nil && state.Status == "success"
		restored.mu.Unlock()
		if complete {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("restart did not finish waiting merge", state)
}

func TestEmbyCopySpaceWaitKeepsOldAndSourceMedia(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(settings.OutputDir, folder)
	media := filepath.Join(root, filepath.FromSlash(embyLocalMergedName))
	old, err := os.ReadFile(media)
	if err != nil {
		t.Fatal(err)
	}
	var manifest embyFolderManifest
	if err := readEmbyJSON(filepath.Join(root, ".duanju-emby.json"), &manifest, 8<<20); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(merge.OutputPath, []byte("replacement video bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := syncEmbyLocalMedia(context.Background(), root, merge.OutputPath, &manifest, func(_ string, reserve uint64) error {
		if reserve != uint64(len("replacement video bytes")) {
			t.Error("copy reserve excluded full source size")
		}
		return spaceFailure()
	})
	if changed || !diskspace.IsWaiting(err) || manifest.PendingMedia != nil {
		t.Fatal("copy space failure changed publication", err)
	}
	current, err := os.ReadFile(media)
	if err != nil || string(current) != string(old) {
		t.Fatal("old published video changed", err)
	}
	if _, err := os.Stat(merge.OutputPath); err != nil {
		t.Fatal("source media removed", err)
	}
}

func TestMergeFFmpegDirectFullDiskWaits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	program := filepath.Join(t.TempDir(), "fake-ffmpeg")
	if err := os.WriteFile(program, []byte("#!/bin/sh\nprintf 'No space left on device' >&2\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := runMergeFFmpeg(context.Background(), program, nil, nil); !diskspace.IsWaiting(err) {
		t.Fatal("immediate disk-full counted as media failure", err)
	}
}

func TestUserCanceledWaitingMergeCannotResumeAfterShutdown(t *testing.T) {
	app := viewerTestApp(t)
	app.statePath = filepath.Join(app.cfg.dataDirectory(), "ui-state.json")
	app.mergeMu.Lock()
	var once sync.Once
	unlock := func() { once.Do(app.mergeMu.Unlock) }
	defer func() { unlock(); app.stopMergeJobs() }()
	app.mu.Lock()
	jobs, err := app.enqueueMergesLocked([]string{historyFixtureDramaID}, false)
	if err == nil {
		jobs[0].running = true
		app.merges[historyFixtureDramaID].WaitingForSpace = true
	}
	app.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	writer := historyJSONRequest(t, app, app.handleMergeCancel, "/api/ui/merge/cancel", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if writer.Code != http.StatusOK {
		t.Fatal(writer.Code, writer.Body.String())
	}
	app.mu.Lock()
	app.mergeStopped = true
	app.mu.Unlock()
	res := uiMergeResult{DramaID: historyFixtureDramaID, WaitingForSpace: true}
	app.recordMergeError(&res, context.Canceled, false)
	if res.WaitingForSpace || res.Error == "" || app.merges[historyFixtureDramaID].Status != "failed" {
		t.Fatal("shutdown revived explicitly canceled merge", res)
	}
	restored := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if state := restored.merges[historyFixtureDramaID]; state.WaitingForSpace || state.Status != "failed" {
		t.Fatal("persisted cancel intent was lost", state)
	}
}
