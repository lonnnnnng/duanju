package app

import (
	"context"
	"errors"
	"net/http"
	"time"
)

func (a *UIApp) handleVerifyTasks(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	selection, ok := readTaskSelection(writer, request)
	if !ok || !requireDownload(writer, request) {
		return
	}
	if len(selection.IDs) > 10 || len(selection.DramaIDs) != 0 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "每次最多校验 10 个分集"})
		return
	}
	a.mu.Lock()
	if !a.requireTaskSourcesLocked(writer, request, selection) {
		a.mu.Unlock()
		return
	}
	type candidate struct {
		original *UITask
		id, path string
	}
	var candidates []candidate
	for _, id := range selection.idsLocked(a) {
		if task := a.tasks[id]; task != nil && task.Status == uiStatusSuccess && !task.RemoveRequested {
			candidates = append(candidates, candidate{original: task, id: id, path: a.managedCompletedPlaybackPath(task)})
		}
	}
	a.mu.Unlock()
	if len(candidates) == 0 {
		writeJSON(writer, http.StatusOK, map[string]int{"checked": 0, "damaged": 0})
		return
	}
	ffmpeg, err := a.downloader.ensureFFmpeg(request.Context())
	if err != nil {
		writeJSON(writer, http.StatusServiceUnavailable, map[string]string{"error": "FFmpeg 尚不可用，无法校验本地视频"})
		return
	}
	checked, damaged := 0, 0
	for _, item := range candidates {
		if request.Context().Err() != nil {
			return
		}
		var probeErr error
		if item.path == "" {
			probeErr = errors.New("本地文件不存在")
		} else {
			_, probeErr = probeMergeMedia(request.Context(), ffmpeg, item.path)
		}
		if request.Context().Err() != nil {
			return
		}
		if errors.Is(probeErr, context.DeadlineExceeded) {
			writeJSON(writer, http.StatusGatewayTimeout, map[string]string{"error": "本地文件校验超时，请稍后重试"})
			return
		}
		a.mu.Lock()
		current := a.tasks[item.id]
		if current == item.original && current.Status == uiStatusSuccess && a.managedCompletedPlaybackPath(current) == item.path {
			checked++
			if probeErr != nil {
				// long: 保留损坏文件供排查；重试下载会再次校验并在成功后原子替换它。
				previous := *current
				current.Status, current.Phase = uiStatusFailed, "failed"
				current.Error = "本地文件缺失或视频校验失败，请点击重试重新下载"
				current.UpdatedAt = time.Now()
				if saveErr := a.saveStateLockedWithDiagnostic("ui.task_verify_save_failed"); saveErr != nil {
					*current = previous
					a.mu.Unlock()
					a.writeStatePersistenceError(writer, saveErr)
					return
				}
				damaged++
			}
		}
		a.mu.Unlock()
	}
	writeJSON(writer, http.StatusOK, map[string]int{"checked": checked, "damaged": damaged})
}
