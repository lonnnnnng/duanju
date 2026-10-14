package app

import (
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestMergedEpisodesAreKeptWhenSuccessRecordCannotBeSaved(t *testing.T) {
	app, _, first, second, merged := deleteFixture(t)
	app.statePath = app.cfg.OutputDir
	res := uiMergeResult{DramaID: historyFixtureDramaID, DramaTitle: "删除测试剧", OutputPath: merged, StartEpisode: 1, EndEpisode: 2, Merged: 2}
	valid := []*UITask{cloneUITask(app.tasks["episode-1"]), cloneUITask(app.tasks["episode-2"])}
	app.finishMergedDrama(&res, valid, false, true)
	if res.OK || !res.PersistFailed || !strings.Contains(res.Error, "保存失败") || len(app.merges[historyFixtureDramaID].CleanupPending) != 2 {
		t.Fatal("failed success record did not leave cleanup pending", res)
	}
	for _, path := range []string{first, second, merged} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("a media file was lost after persistence failure", path, err)
		}
	}
}

func TestMergedFileIsReportedAsUnpersistedWhenSuccessRecordCannotBeSaved(t *testing.T) {
	app, _, first, second, merged := deleteFixture(t)
	app.statePath = app.cfg.OutputDir
	res := uiMergeResult{DramaID: historyFixtureDramaID, DramaTitle: "删除测试剧", OutputPath: merged, StartEpisode: 1, EndEpisode: 2, Merged: 2}
	app.finishMergedDrama(&res, nil, true, false)
	if res.OK || !res.PersistFailed || !strings.Contains(res.Error, "保存失败") {
		t.Fatal("未保存的合并记录被当成成功", res)
	}
	for _, path := range []string{first, second, merged} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("保存失败后应保留全集和分集", path, err)
		}
	}
}

func TestSynchronousMergeReportsPersistenceFailureAsServerError(t *testing.T) {
	app, admin, _, _, merged := deleteFixture(t)
	job := &uiMergeJob{dramaID: historyFixtureDramaID, done: make(chan struct{}), result: uiMergeResult{
		DramaID: historyFixtureDramaID, OutputPath: merged, Error: "全集已生成，但合并记录保存失败", PersistFailed: true,
	}}
	close(job.done)
	app.mergeJobs = map[string]*uiMergeJob{historyFixtureDramaID: job}
	response := admin.request(t, http.MethodPost, "/api/ui/merge", map[string]any{"dramaIds": []string{historyFixtureDramaID}})
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"code":"state_persist_failed"`) ||
		!strings.Contains(response.Body.String(), `"ok":false`) || !strings.Contains(response.Body.String(), merged) {
		t.Fatal("同步合并掩盖了状态保存失败", response.Code, response.Body.String())
	}
}

func TestMergedSuccessRecordSurvivesRestartAfterEpisodeCleanup(t *testing.T) {
	app, _, first, second, merged := deleteFixture(t)
	res := uiMergeResult{DramaID: historyFixtureDramaID, DramaTitle: "删除测试剧", OutputPath: merged, StartEpisode: 1, EndEpisode: 2, Merged: 2}
	valid := []*UITask{cloneUITask(app.tasks["episode-1"]), cloneUITask(app.tasks["episode-2"])}
	app.finishMergedDrama(&res, valid, false, true)
	if !res.OK || res.Error != "" {
		t.Fatal("merged cleanup failed", res)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("original episode remains after cleanup", path, err)
		}
	}
	restarted := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restarted.loadState()
	if state := restarted.merges[historyFixtureDramaID]; state == nil || state.Status != "success" || state.OutputPath != merged || len(state.CleanupPending) != 0 {
		t.Fatal("saved whole-video record was not restored", state)
	}
}
