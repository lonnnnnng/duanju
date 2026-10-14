package app

import (
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyCompletedDownloadKeepsDamagedFileAndMarksRetryable(t *testing.T) {
	app, admin, first, _, _ := deleteFixture(t)
	response := admin.request(t, http.MethodPost, "/api/ui/tasks/verify", map[string]any{"ids": []string{"episode-1"}})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"damaged":1`) {
		t.Fatal("invalid local video was not detected", response.Code, response.Body.String())
	}
	if task := app.tasks["episode-1"]; task == nil || task.Status != uiStatusFailed || !strings.Contains(task.Error, "重试") {
		t.Fatal("damaged task was not made retryable")
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal("verification removed original file", err)
	}
	response = admin.request(t, http.MethodPost, "/api/ui/tasks/verify", map[string]any{"ids": []string{"episode-1"}})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"checked":0`) {
		t.Fatal("failed task was checked again", response.Code, response.Body.String())
	}
}

func TestVerifyCompletedDownloadAcceptsValidMP4(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg is unavailable")
	}
	app, admin, first, _, _ := deleteFixture(t)
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: ffmpeg}}
	mergeFixtureCommand(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25:duration=1", "-c:v", "libx264", "-preset", "ultrafast", "-threads:v", "1", first)
	response := admin.request(t, http.MethodPost, "/api/ui/tasks/verify", map[string]any{"ids": []string{"episode-1"}})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"damaged":0`) || app.tasks["episode-1"].Status != uiStatusSuccess {
		t.Fatal("valid local video was marked damaged", response.Code, response.Body.String())
	}
}

func TestRetryMergedEpisodeCleanup(t *testing.T) {
	app, admin, first, _, merged := deleteFixture(t)
	prepareMergedCleanupFixture(t, app, merged)
	state := app.merges[historyFixtureDramaID]
	state.OutputPath = merged
	state.CleanupPending = []string{first}
	state.Error = "全集已生成，但 1 个分集文件清理失败"
	response := admin.request(t, http.MethodPost, "/api/ui/merge/cleanup", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"pending":0`) {
		t.Fatal("merged episode cleanup did not finish", response.Code, response.Body.String())
	}
	if _, err := os.Stat(first); !os.IsNotExist(err) || !state.DeleteEpisodes || len(state.CleanupPending) != 0 {
		t.Fatal("cleaned episode still exists or status not updated", err)
	}
	if _, err := os.Stat(merged); err != nil {
		t.Fatal("cleanup removed the merged video", err)
	}
}

func TestRetryMergedCleanupPreservesSharedAndSymlinkFiles(t *testing.T) {
	app, admin, first, _, merged := deleteFixture(t)
	prepareMergedCleanupFixture(t, app, merged)
	state := app.merges[historyFixtureDramaID]
	state.CleanupPending = []string{first}
	other := *app.tasks["episode-2"]
	other.ID, other.DramaID, other.Path = "other-episode", "hongguo:other-drama", first
	other.Source.DramaID, other.Source.OutPath = other.DramaID, first
	app.tasks[other.ID] = &other
	response := admin.request(t, http.MethodPost, "/api/ui/merge/cleanup", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if response.Code != http.StatusOK || len(state.CleanupPending) != 1 {
		t.Fatal("shared file was removed during cleanup", response.Code, response.Body.String())
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal(err)
	}
	delete(app.tasks, other.ID)
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
	response = admin.request(t, http.MethodPost, "/api/ui/merge/cleanup", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if response.Code != http.StatusOK || len(state.CleanupPending) != 1 {
		t.Fatal("symlink was removed during cleanup", response.Code, response.Body.String())
	}
	if body, err := os.ReadFile(outside); err != nil || string(body) != "keep" {
		t.Fatal("outside file changed", err)
	}
}

func prepareMergedCleanupFixture(t *testing.T, app *UIApp, merged string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg is unavailable")
	}
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: ffmpeg}}
	mergeFixtureCommand(t, ffmpeg, "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=25:duration=1", "-c:v", "libx264", "-preset", "ultrafast", "-threads:v", "1", merged)
}

func TestRetryMergedCleanupKeepsEpisodesWhenWholeVideoIsDamaged(t *testing.T) {
	app, admin, first, _, merged := deleteFixture(t)
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("local FFmpeg is unavailable")
	}
	app.downloader.ffmpegInstaller = &ffmpegInstaller{state: ffmpegInstallState{Status: "ready", Path: ffmpeg}}
	state := app.merges[historyFixtureDramaID]
	state.OutputPath, state.CleanupPending = merged, []string{first}
	response := admin.request(t, http.MethodPost, "/api/ui/merge/cleanup", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if response.Code != http.StatusConflict || len(state.CleanupPending) != 1 {
		t.Fatal("damaged whole video allowed episode cleanup", response.Code, response.Body.String())
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal("original episode was removed", err)
	}
}

func TestRetryMergedCleanupKeepsActiveDownload(t *testing.T) {
	app, admin, first, _, merged := deleteFixture(t)
	prepareMergedCleanupFixture(t, app, merged)
	state := app.merges[historyFixtureDramaID]
	state.CleanupPending = []string{first}
	app.tasks["episode-1"].Status = uiStatusRunning
	response := admin.request(t, http.MethodPost, "/api/ui/merge/cleanup", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if response.Code != http.StatusOK || len(state.CleanupPending) != 1 {
		t.Fatal("active download was cleaned", response.Code, response.Body.String())
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal("active download file was removed", err)
	}
}

func TestRetryMergedCleanupClearsAlreadyDeletedEpisode(t *testing.T) {
	app, admin, first, _, merged := deleteFixture(t)
	prepareMergedCleanupFixture(t, app, merged)
	state := app.merges[historyFixtureDramaID]
	state.CleanupPending = []string{first}
	if err := os.Remove(first); err != nil {
		t.Fatal(err)
	}
	response := admin.request(t, http.MethodPost, "/api/ui/merge/cleanup", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if response.Code != http.StatusOK || len(state.CleanupPending) != 0 || !state.DeleteEpisodes {
		t.Fatal("already deleted episode kept pending", response.Code, response.Body.String())
	}
}

func TestInitialMergedCleanupKeepsChangedTask(t *testing.T) {
	app, _, first, _, merged := deleteFixture(t)
	snapshot := *app.tasks["episode-1"]
	app.tasks["episode-1"].Status = uiStatusQueued
	res := uiMergeResult{DramaID: historyFixtureDramaID, OutputPath: merged, OK: true}
	app.cleanupMergedEpisodeFiles(&res, []*UITask{&snapshot})
	if len(res.CleanupPending) != 1 || res.CleanupPending[0] != first {
		t.Fatal("changed task did not remain pending", res)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal("new download file was removed", err)
	}
}

func TestPendingMergedCleanupCannotBeOverwrittenByNewMerge(t *testing.T) {
	app, admin, first, _, _ := deleteFixture(t)
	state := app.merges[historyFixtureDramaID]
	state.CleanupPending = []string{first}
	response := admin.request(t, http.MethodPost, "/api/ui/merge", map[string]any{"dramaIds": []string{historyFixtureDramaID}, "async": true})
	if response.Code != http.StatusConflict || app.merges[historyFixtureDramaID] != state || len(state.CleanupPending) != 1 {
		t.Fatal("new merge overwrote pending cleanup record", response.Code, response.Body.String())
	}
}

func TestMergedCleanupRecordsFailedFilesWithoutLosingWholeVideo(t *testing.T) {
	app, _, first, _, merged := deleteFixture(t)
	res := uiMergeResult{DramaID: historyFixtureDramaID, OutputPath: merged, OK: true}
	other := *app.tasks["episode-2"]
	other.ID, other.DramaID, other.Path = "shared-other", "hongguo:other-drama", first
	other.Source.DramaID, other.Source.OutPath = other.DramaID, first
	app.tasks[other.ID] = &other
	app.cleanupMergedEpisodeFiles(&res, []*UITask{app.tasks["episode-1"]})
	if len(res.CleanupPending) != 1 || res.CleanupPending[0] != first || res.Error == "" {
		t.Fatal("failed cleanup was not retained for retry", res)
	}
	if _, err := os.Stat(first); err != nil {
		t.Fatal("shared episode was removed", err)
	}
	if _, err := os.Stat(merged); err != nil {
		t.Fatal("whole video was removed", err)
	}
}
