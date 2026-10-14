package app

import (
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
)

func TestTaskPriorityReordersOnlyQueuedAndPersists(t *testing.T) {
	app, admin, _, _, _ := deleteFixture(t)
	labels := []string{"running", "queued-a", "paused", "queued-b", "queued-c", "completed"}
	ids := map[string]string{}
	order := make([]string, 0, len(labels))
	app.tasks = map[string]*UITask{}
	for index, label := range labels {
		status := uiStatusQueued
		switch label {
		case "running":
			status = uiStatusRunning
		case "paused":
			status = uiStatusPaused
		case "completed":
			status = uiStatusSuccess
		}
		path := filepath.Join(app.cfg.OutputDir, "优先级测试", label+".mp4")
		id := uiTaskID(path)
		ids[label] = id
		order = append(order, id)
		app.tasks[id] = &UITask{ID: id, DramaID: historyFixtureDramaID, Status: status, Path: path,
			Source: Task{DramaID: historyFixtureDramaID, OutPath: path, Chapter: Chapter{ID: id, Source: sourceHongguo}, Index: index + 1}}
	}
	app.taskOrder = append([]string(nil), order...)
	// long: 运行中和暂停的槽位保持不动，只改变 worker 下一次领取排队任务的先后。
	response := admin.request(t, http.MethodPost, "/api/ui/tasks/priority", map[string]any{"ids": []string{ids["queued-c"], ids["queued-b"]}, "position": "front"})
	if response.Code != http.StatusOK || !reflect.DeepEqual(app.taskOrder, []string{ids["running"], ids["queued-b"], ids["paused"], ids["queued-c"], ids["queued-a"], ids["completed"]}) {
		t.Fatal("front priority did not preserve queued selection order", response.Code, app.taskOrder, response.Body.String())
	}
	response = admin.request(t, http.MethodPost, "/api/ui/tasks/priority", map[string]any{"ids": []string{ids["queued-b"]}, "position": "back"})
	if response.Code != http.StatusOK || !reflect.DeepEqual(app.taskOrder, []string{ids["running"], ids["queued-c"], ids["paused"], ids["queued-a"], ids["queued-b"], ids["completed"]}) {
		t.Fatal("back priority changed unrelated task slots", response.Code, app.taskOrder)
	}
	restored := &UIApp{cfg: app.cfg, statePath: app.statePath, tasks: map[string]*UITask{}}
	restored.loadState()
	if !reflect.DeepEqual(restored.taskOrder, app.taskOrder) {
		t.Fatal("queue order was lost after restart", restored.taskOrder, app.taskOrder)
	}
	beforeFailure := append([]string(nil), app.taskOrder...)
	app.statePath = app.cfg.OutputDir
	response = admin.request(t, http.MethodPost, "/api/ui/tasks/priority", map[string]any{"ids": []string{ids["queued-b"]}, "position": "front"})
	if response.Code != http.StatusInternalServerError || !reflect.DeepEqual(app.taskOrder, beforeFailure) {
		t.Fatal("failed persistence changed the live queue order", response.Code, app.taskOrder)
	}
}
