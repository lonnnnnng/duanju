package app

import (
	"context"
	"duanju/internal/diskspace"
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
	if job := a.mergeJobs[res.DramaID]; job != nil {
		userCanceled = job.userCanceled
	}
	a.mu.Unlock()
	// long: 正常停止保留空间等待的合并意图；用户取消则终止任务。即使磁盘满到状态无法落盘，内存队列仍继续重试。
	if !userCanceled && (diskspace.IsWaiting(err) || stopped && res.WaitingForSpace) {
		res.WaitingForSpace = true
		res.Detail, res.Error = a.redactError(err), ""
		a.setMergeState(*res, "queued", 0, false, deleteEpisodes)
		return
	}
	res.WaitingForSpace = false
	if userCanceled || err == context.Canceled {
		res.Error = "合并已取消，原分集已保留"
	} else {
		res.Error = a.redactError(err)
	}
	a.setMergeState(*res, "failed", 0, false, deleteEpisodes)
}
