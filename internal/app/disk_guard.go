package app

import (
	"context"
	"duanju/internal/diskspace"
	"errors"
)

func (d *Downloader) requireSpace(path string, reserve uint64) error {
	if d.spaceCheck != nil {
		return d.spaceCheck(path, reserve)
	}
	return diskspace.Require(path, reserve)
}

func (a *UIApp) recordMergeError(res *uiMergeResult, err error, deleteEpisodes bool) {
	a.mu.Lock()
	stopped := a.mergeStopped
	userCanceled := false
	automatic := false
	if job := a.mergeJobs[res.DramaID]; job != nil {
		userCanceled = job.userCanceled
		automatic = job.autoGeneration != 0
	}
	a.mu.Unlock()
	if !userCanceled && stopped && automatic {
		// long: 正常停服与用户取消不同，自动合并保留可恢复意图；下一次启动仍从完整章节集合重新校验。
		res.ResumeAutomatic = true
		res.Detail, res.Error = "服务停止，启动后继续自动合并", ""
		a.setMergeState(*res, "queued", 0, false, false)
		return
	}
	// long: 正常停止保留空间等待的合并意图；用户取消则终止任务。即使磁盘满到状态无法落盘，内存队列仍继续重试。
	if !userCanceled && (diskspace.IsWaiting(err) || stopped && res.WaitingForSpace) {
		res.WaitingForSpace = true
		res.Detail, res.Error = a.redactError(err), ""
		a.setMergeState(*res, "queued", 0, false, deleteEpisodes)
		return
	}
	res.WaitingForSpace = false
	if userCanceled || errors.Is(err, context.Canceled) {
		res.Error = "合并已取消，原分集已保留"
	} else {
		res.Error = a.redactError(err)
	}
	a.setMergeState(*res, "failed", 0, false, deleteEpisodes)
}
