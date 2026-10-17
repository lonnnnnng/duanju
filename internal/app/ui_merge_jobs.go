package app

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type uiMergeJob struct {
	dramaID        string
	tasks          []*UITask
	remove         bool
	ctx            context.Context
	cancel         context.CancelFunc
	done           chan struct{}
	result         uiMergeResult
	running        bool
	userCanceled   bool
	autoGeneration uint64
	autoSnapshot   *autoMergeSnapshot
}

func (a *UIApp) enqueueMergesLocked(ids []string, deleteEpisodes bool, automatic ...bool) ([]*uiMergeJob, error) {
	if a.mergeStopped {
		return nil, fmt.Errorf("服务正在停止，无法提交合并")
	}
	if len(a.mergeJobs)+len(ids) > 256 {
		return nil, fmt.Errorf("合并队列已满，请等待当前任务完成后再提交")
	}
	if a.mergeJobs == nil {
		a.mergeJobs = make(map[string]*uiMergeJob)
	}
	if a.merges == nil {
		a.merges = make(map[string]*UIMergeState)
	}
	for _, id := range ids {
		// long: 未回收的分集只登记在当前合并记录中；覆盖记录会让失败文件失去安全的重试入口。
		if state := a.merges[id]; state != nil && len(state.CleanupPending) > 0 && a.mergeJobs[id] == nil {
			return nil, fmt.Errorf("本剧仍有未清理的分集文件，请先重试清理")
		}
	}
	byDrama := make(map[string][]*UITask)
	for _, taskID := range a.taskOrder {
		if task := a.tasks[taskID]; task != nil && task.Status == uiStatusSuccess && !task.RemoveRequested {
			byDrama[task.DramaID] = append(byDrama[task.DramaID], cloneUITask(task))
		}
	}
	previous := cloneMergeStates(a.merges)
	restoredOnly := true
	var jobs, added []*uiMergeJob
	for _, id := range ids {
		job := a.mergeJobs[id]
		if job == nil {
			ctx, cancel := context.WithCancel(context.Background())
			job = &uiMergeJob{dramaID: id, tasks: byDrama[id], remove: deleteEpisodes, ctx: ctx, cancel: cancel, done: make(chan struct{})}
			if len(automatic) > 0 && automatic[0] {
				if state := a.autoMerges[id]; state != nil {
					job.autoGeneration = state.Generation
				}
			}
			a.mergeJobs[id] = job
			added = append(added, job)
			title := ""
			if len(job.tasks) > 0 {
				title = job.tasks[0].DramaTitle
			}
			a.merges[id] = &UIMergeState{DramaID: id, DramaTitle: title, Status: "queued", Detail: "已提交后台合并，可关闭或刷新页面", DeleteEpisodes: deleteEpisodes, UpdatedAt: time.Now()}
			a.merges[id].Automatic = job.autoGeneration != 0
			if old := previous[id]; old != nil && old.WaitingForSpace {
				a.merges[id] = old
			} else {
				restoredOnly = false
			}
		}
		jobs = append(jobs, job)
	}
	var saveErr error
	// long: 恢复的等待意图已持久化；磁盘仍满时也先启动重试，避免必须先写状态才能等到空间恢复。
	if !restoredOnly {
		saveErr = a.saveStateLocked()
	}
	if saveErr != nil {
		for _, job := range added {
			delete(a.mergeJobs, job.dramaID)
			job.cancel()
		}
		a.merges = previous
		return nil, fmt.Errorf("保存合并任务失败：%w", saveErr)
	}
	if len(added) > 0 {
		go a.runMergeJobs(added)
	}
	return jobs, nil
}

func (a *UIApp) runMergeJobs(jobs []*uiMergeJob) {
	a.mergeMu.Lock()
	defer a.mergeMu.Unlock()
	for _, job := range jobs {
		a.mu.Lock()
		if a.mergeJobs[job.dramaID] != job {
			a.mu.Unlock()
			continue
		}
		job.running = true
		a.mu.Unlock()
		if job.ctx.Err() == nil {
			for {
				if job.autoGeneration != 0 {
					if err := a.verifyAutoMergeInputs(job.ctx, job); err != nil {
						job.result = uiMergeResult{DramaID: job.dramaID, Error: a.redactError(err)}
						a.recordMergeError(&job.result, err, false)
						break
					}
				}
				mergeCtx := job.ctx
				if job.autoSnapshot != nil {
					mergeCtx = context.WithValue(mergeCtx, autoMergeContextKey{}, job.autoSnapshot)
				}
				job.result = a.mergeDrama(mergeCtx, job.dramaID, job.tasks, job.remove)
				if !job.result.WaitingForSpace {
					break
				}
				select {
				case <-job.ctx.Done():
					a.recordMergeError(&job.result, context.Canceled, job.remove)
				case <-time.After(5 * time.Second):
					continue
				}
				break
			}
		} else {
			job.result = uiMergeResult{DramaID: job.dramaID, Error: "合并已取消，原分集已保留"}
			a.mu.Lock()
			if state := a.merges[job.dramaID]; state != nil {
				job.result.WaitingForSpace = state.WaitingForSpace
			}
			a.mu.Unlock()
			a.recordMergeError(&job.result, context.Canceled, job.remove)
		}
		job.cancel()
		a.mu.Lock()
		if state := a.autoMerges[job.dramaID]; state != nil && state.Enabled && state.Generation == job.autoGeneration {
			state.Status, state.Error = "error", job.result.Error
			if job.result.OK {
				state.Status = "complete"
			} else if job.result.ResumeAutomatic {
				state.SubmittedGeneration, state.Status, state.Error = 0, "waiting", ""
			}
			_ = a.saveStateLockedWithDiagnostic("ui.auto_merge_finish_save_failed")
		}
		delete(a.mergeJobs, job.dramaID)
		close(job.done)
		a.mu.Unlock()
	}
}

func (a *UIApp) stopMergeJobs() {
	a.mu.Lock()
	a.mergeStopped = true
	var jobs []*uiMergeJob
	for _, job := range a.mergeJobs {
		job.cancel()
		jobs = append(jobs, job)
	}
	a.mu.Unlock()
	for _, job := range jobs {
		<-job.done
	}
}

func (a *UIApp) handleMergeCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !requireDownload(w, r) {
		return
	}
	ids, ok := readDramaIDsRequest(w, r)
	if !ok || !a.requireDramaSources(w, r, ids) {
		return
	}
	a.mu.Lock()
	previous := cloneMergeStates(a.merges)
	previousAutomatic := cloneAutoMergeStates(a.autoMerges)
	var canceledJobs []*uiMergeJob
	for _, id := range ids {
		a.disableAutoMergeLocked(id)
		if job := a.mergeJobs[id]; job != nil {
			if state := a.merges[id]; state != nil {
				state.Status, state.Detail, state.Error = "failed", "", "合并已取消，原分集已保留"
				state.WaitingForSpace = false
				state.ResumeAutomatic = false
			}
			canceledJobs = append(canceledJobs, job)
		}
	}
	saveErr := a.saveStateLockedWithDiagnostic("ui.merge_cancel_save_failed")
	if saveErr != nil {
		a.merges = previous
		a.autoMerges = previousAutomatic
	} else {
		// long: 先持久化用户取消意图，再中断后台合并；即使紧接着关机，取消也不能被“恢复空间等待”覆盖。
		for _, job := range canceledJobs {
			job.userCanceled = true
			job.cancel()
			if !job.running {
				job.result = uiMergeResult{DramaID: job.dramaID, Error: "合并已取消，原分集已保留"}
				delete(a.mergeJobs, job.dramaID)
				close(job.done)
			}
		}
	}
	a.mu.Unlock()
	if saveErr != nil {
		a.writeStatePersistenceError(w, saveErr)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"canceled": len(canceledJobs)})
}
