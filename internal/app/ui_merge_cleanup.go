package app

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"
)

type mergeCleanupTarget struct {
	id     string
	state  *UIMergeState
	paths  []string
	output string
	info   os.FileInfo
}

func (a *UIApp) handleMergeCleanup(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ids, ok := readDramaIDsRequest(writer, request)
	if !ok {
		return
	}
	a.mu.Lock()
	var targets []mergeCleanupTarget
	for _, id := range ids {
		state := a.merges[id]
		if state == nil || state.Status != "success" || a.mergeJobs[id] != nil || len(state.CleanupPending) == 0 {
			continue
		}
		targets = append(targets, mergeCleanupTarget{id: id, state: state, paths: append([]string(nil), state.CleanupPending...), output: state.OutputPath})
	}
	a.mu.Unlock()
	if len(targets) == 0 {
		writeJSON(writer, http.StatusOK, map[string]int{"cleaned": 0, "pending": 0})
		return
	}

	// long: 清理请求可能晚于合并数小时；重新完整解码全集，避免成品已损坏却继续删除唯一可用的分集。
	verifyCtx, cancel := context.WithTimeout(request.Context(), 2*time.Hour)
	defer cancel()
	ffmpeg, err := a.downloader.ensureFFmpeg(verifyCtx)
	if err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "FFmpeg 尚不可用，无法确认全集可播放"})
		return
	}
	for index := range targets {
		target := &targets[index]
		if a.managedCompletedPlaybackPath(&UITask{Status: uiStatusSuccess, DramaID: target.id, Path: target.output}) == "" {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "全集文件不存在或已移动，原分集已保留"})
			return
		}
		target.info, err = os.Stat(target.output)
		if err != nil {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "全集文件不存在或已移动，原分集已保留"})
			return
		}
		if _, err = probeMergeMedia(verifyCtx, ffmpeg, target.output); err == nil {
			err = decodeMergedMedia(verifyCtx, ffmpeg, target.output, nil)
		}
		if err != nil {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "全集解码校验失败，原分集已保留：" + a.redactError(err)})
			return
		}
		checkedInfo, statErr := os.Stat(target.output)
		if statErr != nil || !os.SameFile(checkedInfo, target.info) || checkedInfo.Size() != target.info.Size() || !checkedInfo.ModTime().Equal(target.info.ModTime()) {
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "全集文件在校验后发生变化，原分集已保留"})
			return
		}
	}

	a.mu.Lock()
	for _, target := range targets {
		current := a.merges[target.id]
		info, statErr := os.Stat(target.output)
		if current != target.state || current.Status != "success" || a.mergeJobs[target.id] != nil ||
			current.OutputPath != target.output || !slices.Equal(current.CleanupPending, target.paths) ||
			statErr != nil || !os.SameFile(info, target.info) || info.Size() != target.info.Size() || !info.ModTime().Equal(target.info.ModTime()) {
			a.mu.Unlock()
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "全集或清理任务在校验期间发生变化，请重新尝试"})
			return
		}
	}
	cleaned, pending := 0, 0
	for _, target := range targets {
		state := a.merges[target.id]
		var remaining []string
		for _, path := range state.CleanupPending {
			// long: 用户也可能在下载管理中单独删除分集；文件已消失时只清除待办记录，不再要求旧任务存在。
			if _, statErr := os.Lstat(path); errors.Is(statErr, os.ErrNotExist) {
				cleaned++
				continue
			} else if statErr != nil {
				remaining = append(remaining, path)
				continue
			}
			// long: 仅回收仍由本剧成功分集登记的路径，重试或新下载已占用的文件必须保留。
			owned, shared := false, filepath.Clean(path) == filepath.Clean(state.OutputPath)
			for _, task := range a.tasks {
				if task == nil || filepath.Clean(firstNonEmpty(task.Path, task.Source.OutPath)) != filepath.Clean(path) {
					continue
				}
				if task.DramaID == target.id && task.Status == uiStatusSuccess && !task.RemoveRequested &&
					(task.Source.OutPath == "" || filepath.Clean(task.Source.OutPath) == filepath.Clean(path)) {
					owned = true
				} else {
					shared = true
				}
			}
			for _, merge := range a.merges {
				if merge != nil && merge.OutputPath != "" && filepath.Clean(merge.OutputPath) == filepath.Clean(path) {
					shared = true
					break
				}
			}
			if !owned || shared {
				remaining = append(remaining, path)
				continue
			}
			if _, err := a.deleteManagedPath(path, target.id); err != nil {
				remaining = append(remaining, path)
				continue
			}
			cleaned++
		}
		state.CleanupPending = remaining
		state.DeleteEpisodes = len(remaining) == 0
		state.Error = ""
		if len(remaining) > 0 {
			state.Error = "仍有分集文件未能清理，请检查文件权限或共享引用后重试"
		}
		state.UpdatedAt = time.Now()
		pending += len(remaining)
	}
	if err := a.saveStateLockedWithDiagnostic("ui.merge_cleanup_save_failed"); err != nil {
		a.mu.Unlock()
		a.writeStatePersistenceError(writer, err)
		return
	}
	a.mu.Unlock()
	writeJSON(writer, http.StatusOK, map[string]int{"cleaned": cleaned, "pending": pending})
}
