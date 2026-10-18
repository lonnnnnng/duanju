package app

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var finishedEpisodeRemark = regexp.MustCompile(`(?:全\s*\d+\s*集|\d+\s*集全|已完结|大结局)`)

func releaseStatusFromRemark(remark string) string {
	if strings.Contains(remark, "未完结") || strings.Contains(remark, "更新至") || strings.Contains(remark, "连载") {
		return "ongoing"
	}
	if finishedEpisodeRemark.MatchString(remark) || strings.Contains(remark, "完结") {
		return "finished"
	}
	return "unknown"
}

func dramaReleaseStatus(drama Drama) string {
	switch drama.ReleaseStatus {
	case "finished", "ongoing", "unknown":
		return drama.ReleaseStatus
	default:
		return releaseStatusFromRemark(drama.Remark)
	}
}

func (a *UIApp) removeTaskLocked(id string) {
	task := a.tasks[id]
	delete(a.tasks, id)
	order := a.taskOrder[:0]
	groupRemains := false
	for _, taskID := range a.taskOrder {
		if current := a.tasks[taskID]; current != nil {
			order = append(order, taskID)
			if task != nil && current.DramaID == task.DramaID {
				groupRemains = true
			}
		}
	}
	a.taskOrder = order
	if task != nil && !groupRemains {
		if _, _, err := a.mergedPlaybackTaskLocked(mergedPlaybackPrefix + task.DramaID); err != nil && a.mergeJobs[task.DramaID] == nil {
			delete(a.merges, task.DramaID)
		}
	}
}

func (a *UIApp) startDramaParse(drama Drama, updateOnly bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	a.mu.Lock()
	if a.closed {
		// long: 关停时不能再给解析等待组新增任务，否则服务可能在后台仍写状态时返回。
		a.mu.Unlock()
		cancel()
		return
	}
	if a.parsingCancels == nil {
		a.parsingCancels = map[string]context.CancelFunc{}
	}
	a.parsingCancels[placeholderDramaKey(drama)] = cancel
	if a.finishCanceledParseLocked(drama) {
		a.mu.Unlock()
		cancel()
		a.markParsingFinished([]Drama{drama})
		return
	}
	quality := 0
	if task := a.tasks[uiTaskID(a.placeholderTask(drama, "parsing").OutPath)]; task != nil {
		quality = task.Source.DownloadQuality
	}
	a.parsingWG.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.parsingWG.Done()
		defer cancel()
		defer a.markParsingFinished([]Drama{drama})
		_, _, _ = a.enqueueDramas(ctx, []Drama{drama}, updateOnly, quality)
	}()
}

func (a *UIApp) finishCanceledParseLocked(drama Drama) bool {
	id := uiTaskID(a.placeholderTask(drama, "parsing").OutPath)
	task := a.tasks[id]
	if task == nil || !task.CancelRequested && !task.RemoveRequested && !task.PauseRequested {
		return false
	}
	if task.RemoveRequested {
		a.removeTaskLocked(id)
	} else if task.PauseRequested {
		task.Status = uiStatusPaused
		task.Phase = "paused"
		task.PauseRequested = false
		task.CancelRequested = false
		task.Error = ""
	} else {
		task.Status = uiStatusCanceled
		task.Phase = "canceled"
		task.CancelRequested = false
		task.Error = "章节解析已取消"
		task.UpdatedAt = time.Now()
	}
	_ = a.saveStateLockedWithDiagnostic("ui.parse_cancel_save_failed")
	return true
}

func isChapterPlaceholderTask(task *UITask) bool {
	return task != nil && (strings.Contains(task.Source.Chapter.ID, ":fetch-failed") || strings.Contains(task.Source.Chapter.ID, ":parsing"))
}

type taskSelection struct {
	IDs      []string `json:"ids"`
	DramaIDs []string `json:"dramaIds"`
}

func readTaskSelection(writer http.ResponseWriter, request *http.Request) (taskSelection, bool) {
	var selection taskSelection
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&selection); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "无效的任务选择"})
		return selection, false
	}
	if _, valid := cleanIDList(writer, append(append([]string{}, selection.IDs...), selection.DramaIDs...)); !valid {
		return selection, false
	}
	return selection, true
}

func (selection taskSelection) idsLocked(app *UIApp) []string {
	groups := map[string]bool{}
	selected := map[string]bool{}
	for _, id := range selection.DramaIDs {
		groups[strings.TrimSpace(id)] = true
	}
	for _, id := range selection.IDs {
		selected[strings.TrimSpace(id)] = true
	}
	var ids []string
	for _, id := range app.taskOrder {
		task := app.tasks[id]
		if task != nil && (selected[id] || groups[task.DramaID]) {
			ids = append(ids, id)
		}
	}
	return ids
}

func (a *UIApp) handleTaskPriority(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var input struct {
		IDs      []string `json:"ids"`
		DramaIDs []string `json:"dramaIds"`
		Position string   `json:"position"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Position != "front" && input.Position != "back" {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请选择优先下载或稍后下载"})
		return
	}
	if _, valid := cleanIDList(writer, append(append([]string{}, input.IDs...), input.DramaIDs...)); !valid {
		return
	}
	a.mu.Lock()
	selection := taskSelection{IDs: input.IDs, DramaIDs: input.DramaIDs}
	selected := make(map[string]bool)
	for _, id := range selection.idsLocked(a) {
		selected[id] = true
	}
	before := append([]string(nil), a.taskOrder...)
	var chosen, other []string
	for _, id := range before {
		if task := a.tasks[id]; task != nil && task.Status == uiStatusQueued && !task.RemoveRequested {
			if selected[id] {
				chosen = append(chosen, id)
			} else {
				other = append(other, id)
			}
		}
	}
	if input.Position == "front" {
		chosen = append(chosen, other...)
	} else {
		chosen = append(other, chosen...)
	}
	// long: worker 只扫描 taskOrder；在同一把锁中替换排队槽位，运行中任务既不抢占也不改状态。
	index := 0
	for slot, id := range a.taskOrder {
		if task := a.tasks[id]; task != nil && task.Status == uiStatusQueued && !task.RemoveRequested {
			a.taskOrder[slot] = chosen[index]
			index++
		}
	}
	changed := 0
	for index, id := range before {
		if a.taskOrder[index] != id {
			changed++
		}
	}
	if changed > 0 {
		if err := a.saveStateLockedWithDiagnostic("ui.task_priority_save_failed"); err != nil {
			a.taskOrder = before
			a.mu.Unlock()
			a.writeStatePersistenceError(writer, err)
			return
		}
		if a.cond != nil {
			a.cond.Broadcast()
		}
	}
	views := a.taskViewsLocked()
	a.mu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]any{"data": views, "changed": changed})
}

func (a *UIApp) handlePause(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	selection, ok := readTaskSelection(writer, request)
	if !ok {
		return
	}
	a.mu.Lock()
	snapshot := a.snapshotTaskMutationLocked()
	var cancels []context.CancelFunc
	for _, id := range selection.idsLocked(a) {
		task := a.tasks[id]
		if task == nil || task.RemoveRequested || task.CancelRequested {
			continue
		}
		switch task.Status {
		case uiStatusQueued:
			task.Status = uiStatusPaused
			task.Phase = "paused"
		case uiStatusRunning, uiStatusParsing:
			task.PauseRequested = true
			if cancel := a.runningCancels[id]; cancel != nil {
				cancels = append(cancels, cancel)
			}
			if task.Status == uiStatusParsing {
				if cancel := a.parsingCancels[task.DramaID]; cancel != nil {
					cancels = append(cancels, cancel)
				}
			}
		}
		task.UpdatedAt = time.Now()
	}
	saveErr := a.saveStateLockedWithDiagnostic("ui.task_pause_save_failed")
	if saveErr != nil {
		a.restoreTaskMutationLocked(snapshot)
	}
	views := a.taskViewsLocked()
	a.mu.Unlock()
	if saveErr != nil {
		a.writeStatePersistenceError(writer, saveErr)
		return
	}
	// long: 暂停意图保存成功后再中断解析和下载，避免接口报保存失败却已经停止任务。
	for _, cancel := range cancels {
		cancel()
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": views})
}

func (a *UIApp) handleResume(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	selection, ok := readTaskSelection(writer, request)
	if !ok {
		return
	}
	a.mu.Lock()
	snapshot := a.snapshotTaskMutationLocked()
	if a.parsingInFlight == nil {
		a.parsingInFlight = map[string]bool{}
	}
	var reparses []Drama
	changed := false
	for _, id := range selection.idsLocked(a) {
		task := a.tasks[id]
		if task == nil || task.Status != uiStatusPaused || task.RemoveRequested {
			continue
		}
		if isChapterPlaceholderTask(task) {
			drama := dramaFromPlaceholderTask(task)
			dramaKey := placeholderDramaKey(drama)
			if a.parsingInFlight[dramaKey] {
				continue
			}
			a.removeTaskLocked(id)
			a.parsingInFlight[dramaKey] = true
			a.addParsingPlaceholderTaskLocked(drama, task.Source.DownloadQuality)
			reparses = append(reparses, drama)
			changed = true
			continue
		}
		task.Status = uiStatusQueued
		task.Phase = "queued"
		task.NextSpaceCheck = time.Time{}
		task.Error = ""
		task.Progress = 0
		task.DownloadedBytes = 0
		task.SpeedBytesPerSecond = 0
		task.PauseRequested = false
		task.CancelRequested = false
		task.UpdatedAt = time.Now()
		changed = true
	}
	var saveErr error
	if changed {
		saveErr = a.saveStateLockedWithDiagnostic("ui.task_resume_save_failed")
		if saveErr != nil {
			a.restoreTaskMutationLocked(snapshot)
		} else {
			a.cond.Broadcast()
		}
	}
	views := a.taskViewsLocked()
	a.mu.Unlock()
	if saveErr != nil {
		a.writeStatePersistenceError(writer, saveErr)
		return
	}
	for _, drama := range reparses {
		a.startDramaParse(drama, true)
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": views})
}
