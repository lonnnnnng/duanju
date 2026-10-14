package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestDownloadEnqueueRollsBackWhenStateSaveFails(t *testing.T) {
	app, _, _, _, _ := deleteFixture(t)
	app.cond = sync.NewCond(&app.mu)
	before := app.snapshotTaskMutationLocked()
	app.statePath = app.cfg.OutputDir
	views, err := app.enqueueDramasAsync([]string{historyFixtureDramaID}, false, nil)
	if err == nil || len(views) != 0 || len(app.parsingCancels) != 0 || !reflect.DeepEqual(app.taskOrder, before.taskOrder) ||
		!reflect.DeepEqual(app.parsingInFlight, before.parsingInFlight) || len(app.tasks) != len(before.tasks) {
		t.Fatal("保存失败后仍然入队或启动解析", err, views)
	}
}

func TestUnmanagedMediaCannotBePlayedOrMerged(t *testing.T) {
	app, _, _, _, _ := deleteFixture(t)
	outside := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(outside, []byte("video"), 0600); err != nil {
		t.Fatal(err)
	}
	task := cloneUITask(app.tasks["episode-1"])
	task.Path, task.Source.OutPath = outside, outside
	if app.managedCompletedPlaybackPath(task) != "" {
		t.Fatal("下载目录外的文件被当作本地视频播放")
	}
	app.merges[historyFixtureDramaID].OutputPath = outside
	if _, _, err := app.mergedPlaybackTaskLocked(mergedPlaybackPrefix + historyFixtureDramaID); err == nil {
		t.Fatal("下载目录外的文件被当作合并全集播放")
	}
	result := app.mergeDrama(context.Background(), historyFixtureDramaID, []*UITask{task}, false)
	if !strings.Contains(result.Error, "受管理") {
		t.Fatal("下载目录外的文件被用作合并输入", result)
	}
}

func TestManagedPlaybackKeepsEarlierDownloadDirectory(t *testing.T) {
	app, _, first, _, merged := deleteFixture(t)
	marker := filepath.Join(filepath.Dir(first), ".drama-id")
	if err := os.WriteFile(marker, []byte(historyFixtureDramaID), 0600); err != nil {
		t.Fatal(err)
	}
	app.cfg.OutputDir = t.TempDir()
	if got := app.managedCompletedPlaybackPath(app.tasks["episode-1"]); got != first {
		t.Fatal("更改下载目录后旧分集无法播放", got)
	}
	if _, got, err := app.mergedPlaybackTaskLocked(mergedPlaybackPrefix + historyFixtureDramaID); err != nil || got != merged {
		t.Fatal("更改下载目录后旧全集无法播放", got, err)
	}
}

func TestMergeRejectsSymlinkOutput(t *testing.T) {
	app, _, _, _, _ := deleteFixture(t)
	first := app.tasks["episode-1"]
	input := cloneUITask(first)
	input.Source.Index = 1
	target := filepath.Join(t.TempDir(), "outside.mp4")
	if err := os.WriteFile(target, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(filepath.Dir(first.Path), mergeOutputName(first.DramaTitle, 1, 1))
	if err := os.Symlink(target, output); err != nil {
		t.Fatal(err)
	}
	result := app.mergeDrama(context.Background(), historyFixtureDramaID, []*UITask{input}, false)
	if !strings.Contains(result.Error, "受管理") {
		t.Fatal("软链接输出仍被允许合并", result)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "keep" {
		t.Fatal("合并改写了软链接目标", err)
	}
}

func TestMergePreservesExistingRegularOutput(t *testing.T) {
	app, _, first, _, _ := deleteFixture(t)
	app.cfg.SkipBytes = 64
	input := cloneUITask(app.tasks["episode-1"])
	input.Source.Index = 1
	output := filepath.Join(filepath.Dir(first), mergeOutputName(input.DramaTitle, 1, 1))
	legacyPart := filepath.Join(filepath.Dir(output), "."+strings.TrimSuffix(filepath.Base(output), ".mp4")+".part.mp4")
	for _, path := range []string{output, legacyPart} {
		if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	result := app.mergeDrama(context.Background(), historyFixtureDramaID, []*UITask{input}, false)
	if result.OK || !strings.Contains(result.Error, "同名") {
		t.Fatal("同名全集文件未被保护", result)
	}
	for path, expected := range map[string]string{first: "video", output: "keep", legacyPart: "keep"} {
		body, err := os.ReadFile(path)
		if err != nil || string(body) != expected {
			t.Fatal("合并碰撞改写了已有文件", path, err)
		}
	}
}

func TestMergeRejectsLargeInvalidExistingOutput(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg is unavailable")
	}
	app, _, first, _, _ := deleteFixture(t)
	app.cfg.SkipBytes = 1
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: ffmpeg}}
	input := cloneUITask(app.tasks["episode-1"])
	input.Source.Index = 1
	output := filepath.Join(filepath.Dir(first), mergeOutputName(input.DramaTitle, 1, 1))
	if err := os.WriteFile(output, []byte("large but invalid media"), 0600); err != nil {
		t.Fatal(err)
	}
	result := app.mergeDrama(context.Background(), historyFixtureDramaID, []*UITask{input}, false)
	if result.OK || !strings.Contains(result.Error, "无法确认可播放") {
		t.Fatal("只按大小把损坏文件登记为全集", result)
	}
	if body, err := os.ReadFile(output); err != nil || string(body) != "large but invalid media" {
		t.Fatal("损坏的同名文件被静默覆盖", err)
	}
}

func TestPublishMergedFileDoesNotReplaceExistingTarget(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "complete.part.mp4")
	target := filepath.Join(dir, "全集.mp4")
	if err := os.WriteFile(source, []byte("new video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("user video"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := publishMergedFile(source, target); !os.IsExist(err) {
		t.Fatal("同名全集没有阻止成品发布", err)
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "user video" {
		t.Fatal("发布覆盖了已有全集", err)
	}
	if body, err := os.ReadFile(source); err != nil || string(body) != "new video" {
		t.Fatal("发布碰撞后临时成品丢失", err)
	}
}
