package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

type UIAutoMergeState struct {
	Enabled             bool     `json:"enabled"`
	RequiredTaskIDs     []string `json:"requiredTaskIds,omitempty"`
	Generation          uint64   `json:"generation"`
	SubmittedGeneration uint64   `json:"submittedGeneration,omitempty"`
	Status              string   `json:"status,omitempty"`
	Error               string   `json:"error,omitempty"`
}

func cloneAutoMergeStates(states map[string]*UIAutoMergeState) map[string]*UIAutoMergeState {
	result := make(map[string]*UIAutoMergeState, len(states))
	for id, state := range states {
		if state != nil {
			copyState := *state
			copyState.RequiredTaskIDs = append([]string(nil), state.RequiredTaskIDs...)
			result[id] = &copyState
		}
	}
	return result
}

func (a *UIApp) recordAutoMergeTasksLocked(id string, built []Task) {
	if a.autoMerges == nil {
		a.autoMerges = make(map[string]*UIAutoMergeState)
	}
	state := a.autoMerges[id]
	if state == nil {
		state = &UIAutoMergeState{}
		a.autoMerges[id] = state
	}
	ids := make([]string, 0, len(built))
	built = append([]Task(nil), built...)
	sort.SliceStable(built, func(i, j int) bool { return built[i].Index < built[j].Index })
	for _, task := range built {
		ids = append(ids, uiTaskID(task.OutPath))
	}
	// long: 需要的分集来自成功解析的完整章节表，删除任务不能缩小它；新增章节形成新一轮合并意图。
	if strings.Join(ids, "\x00") != strings.Join(state.RequiredTaskIDs, "\x00") || state.Generation == 0 {
		state.RequiredTaskIDs = ids
		state.Generation++
		state.Status, state.Error = "waiting", ""
	}
}

func (a *UIApp) disableAutoMergeLocked(id string) {
	if state := a.autoMerges[id]; state != nil {
		state.Enabled, state.Status, state.Error = false, "disabled", ""
	}
}

func (a *UIApp) autoMergeDownloadFinishedLocked(task *UITask) {
	state := a.autoMerges[task.DramaID]
	if state == nil || !state.Enabled || state.Status != "error" || a.mergeJobs[task.DramaID] != nil {
		return
	}
	for _, id := range state.RequiredTaskIDs {
		if id == task.ID {
			// long: 自动合并因坏分集失败后，重新下载成功即可恢复原意图；普通轮询不会不断重试同一坏文件。
			state.SubmittedGeneration, state.Status, state.Error = 0, "waiting", ""
			return
		}
	}
}

func (a *UIApp) startAutoMerge() {
	a.mu.Lock()
	if a.closed || a.autoMergeCancel != nil {
		a.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.autoMergeCancel = cancel
	a.autoMergeWG.Add(1)
	a.mu.Unlock()
	go func() {
		defer a.autoMergeWG.Done()
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()
		for {
			a.checkAutoMerges()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

func (a *UIApp) autoMergeTasksLocked(id string, state *UIAutoMergeState) ([]*UITask, string) {
	if len(state.RequiredTaskIDs) == 0 || state.Generation == 0 || a.parsingInFlight[id] {
		return nil, "等待完整章节解析"
	}
	for _, task := range a.tasks {
		if task != nil && task.DramaID == id && isChapterPlaceholderTask(task) {
			return nil, "章节尚未完整解析，请更新本剧或重试解析"
		}
	}
	indexes := make(map[int]bool)
	tasks := make([]*UITask, 0, len(state.RequiredTaskIDs))
	for _, taskID := range state.RequiredTaskIDs {
		task := a.tasks[taskID]
		if task == nil || task.DramaID != id || task.RemoveRequested || task.CancelRequested || task.PauseRequested || task.Status != uiStatusSuccess {
			return nil, "等待所需分集全部下载完成；缺失记录请更新本剧"
		}
		index := task.Source.Index
		if index < 1 || index > len(state.RequiredTaskIDs) || indexes[index] || task.Source.Total > len(state.RequiredTaskIDs) {
			return nil, "分集序号或总数不完整，请更新本剧核对章节"
		}
		indexes[index] = true
		tasks = append(tasks, cloneUITask(task))
	}
	return tasks, ""
}

func (a *UIApp) checkAutoMerges() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.mergeStopped {
		return
	}
	for id, state := range a.autoMerges {
		if state == nil || !state.Enabled || a.mergeJobs[id] != nil || state.SubmittedGeneration == state.Generation && state.Generation != 0 {
			continue
		}
		tasks, reason := a.autoMergeTasksLocked(id, state)
		if reason != "" {
			state.Status, state.Error = "waiting", reason
			continue
		}
		previous := *state
		state.SubmittedGeneration, state.Status, state.Error = state.Generation, "queued", ""
		// long: 自动合并始终保留分集，提交标记与后台合并在同一次状态保存中落盘，重启不会反复提交同一章节集合。
		jobs, err := a.enqueueMergesLocked([]string{id}, false, true)
		if err != nil {
			*state = previous
			state.Status, state.Error = "waiting", "提交自动合并失败："+a.redactError(err)
			continue
		}
		jobs[0].tasks, jobs[0].autoGeneration = tasks, state.Generation
	}
}

func (a *UIApp) verifyAutoMergeInputs(ctx context.Context, job *uiMergeJob) error {
	ffmpeg, err := a.downloader.ensureFFmpeg(ctx)
	if err != nil {
		return err
	}
	for _, task := range job.tasks {
		if a.validateDeletePath(task.Path, task.DramaID) != nil || task.Path != task.Source.OutPath {
			return fmt.Errorf("第 %d 集不在受管理目录，未自动合并", task.Source.Index)
		}
		if _, err := probeMergeMedia(ctx, ffmpeg, task.Path); err != nil {
			return fmt.Errorf("第 %d 集未通过视频校验，文件已保留，请校验并重试该分集：%w", task.Source.Index, err)
		}
	}
	snapshot, err := collectAutoMergeInputs(ctx, job.tasks)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	state := a.autoMerges[job.dramaID]
	if state == nil || !state.Enabled || state.Generation != job.autoGeneration || a.parsingInFlight[job.dramaID] {
		return fmt.Errorf("自动合并已关闭或章节发生变化，原分集已保留")
	}
	for _, task := range job.tasks {
		current := a.tasks[task.ID]
		if current == nil || current.Status != uiStatusSuccess || current.RemoveRequested || current.Path != task.Path ||
			!current.UpdatedAt.Equal(task.UpdatedAt) {
			return fmt.Errorf("校验期间分集任务发生变化，原分集已保留")
		}
	}
	expected, reason := a.autoMergeTasksLocked(job.dramaID, state)
	if reason != "" || len(expected) != len(job.tasks) {
		return fmt.Errorf("完整分集集合已变化，原分集已保留")
	}
	for index, task := range expected {
		if task.ID != job.tasks[index].ID {
			return fmt.Errorf("合并分集与本轮章节集合不一致，原分集已保留")
		}
	}
	job.autoSnapshot = snapshot
	return nil
}

func (a *UIApp) handleAutoMerge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !requireDownload(w, r) {
		return
	}
	var input struct {
		DramaIDs []string `json:"dramaIds"`
		Enabled  bool     `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "无效的自动合并设置"})
		return
	}
	ids, ok := cleanIDList(w, input.DramaIDs)
	if !ok || !a.requireDramaSources(w, r, ids) {
		return
	}
	a.mu.Lock()
	previous := cloneAutoMergeStates(a.autoMerges)
	previousMerges := cloneMergeStates(a.merges)
	if a.autoMerges == nil {
		a.autoMerges = make(map[string]*UIAutoMergeState)
	}
	var resolve []string
	var canceledJobs []*uiMergeJob
	for _, id := range ids {
		state := a.autoMerges[id]
		if state == nil {
			state = &UIAutoMergeState{}
			a.autoMerges[id] = state
		}
		state.Enabled = input.Enabled
		state.Status, state.Error = "disabled", ""
		if input.Enabled {
			state.Status = "waiting"
			if merge := a.merges[id]; merge == nil || merge.Status != "success" {
				state.SubmittedGeneration = 0
			} else if state.SubmittedGeneration == state.Generation && state.Generation != 0 {
				state.Status = "complete"
			}
			if len(state.RequiredTaskIDs) == 0 {
				resolve = append(resolve, id)
			}
		} else if job := a.mergeJobs[id]; job != nil && job.autoGeneration != 0 {
			if merge := a.merges[id]; merge != nil {
				merge.Status, merge.WaitingForSpace, merge.ResumeAutomatic, merge.Error = "failed", false, false, "自动合并已关闭，原分集已保留"
			}
			canceledJobs = append(canceledJobs, job)
		}
	}
	err := a.saveStateLocked()
	if err != nil {
		a.autoMerges = previous
		a.merges = previousMerges
	} else {
		// long: 关闭开关先落盘再取消自动 job，空间等待也不能在重启后降级成不校验完整集合的普通合并。
		for _, job := range canceledJobs {
			job.userCanceled = true
			job.cancel()
			if !job.running {
				job.result = uiMergeResult{DramaID: job.dramaID, Error: "自动合并已关闭，原分集已保留"}
				delete(a.mergeJobs, job.dramaID)
				close(job.done)
			}
		}
	}
	a.mu.Unlock()
	if err != nil {
		a.writeStatePersistenceError(w, err)
		return
	}
	if len(resolve) > 0 {
		// long: 老版本记录没有远端完整章节集合，先重新解析并补齐缺失集，不能把现存成功记录当全集。
		if _, err := a.enqueueDramasAsync(resolve, true, nil); err != nil {
			a.writeStatePersistenceError(w, err)
			return
		}
	}
	a.startAutoMerge()
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": input.Enabled})
}
