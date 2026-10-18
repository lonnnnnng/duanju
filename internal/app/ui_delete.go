package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type deleteTasksRequest struct {
	IDs            []string `json:"ids"`
	MergedDramaIDs []string `json:"mergedDramaIds"`
}

func readDeleteTasksRequest(writer http.ResponseWriter, request *http.Request) (deleteTasksRequest, bool) {
	var input deleteTasksRequest
	decoder := json.NewDecoder(http.MaxBytesReader(writer, request.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "无效的删除选择"})
		return input, false
	}
	if len(input.IDs)+len(input.MergedDramaIDs) == 0 || len(input.IDs)+len(input.MergedDramaIDs) > 1000 {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "请选择 1–1000 个分集或全集"})
		return input, false
	}
	for _, id := range append(append([]string{}, input.IDs...), input.MergedDramaIDs...) {
		if id == "" || strings.TrimSpace(id) != id {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "删除目标 ID 无效"})
			return input, false
		}
	}
	return input, true
}

func (a *UIApp) handleDeleteTasks(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		writeJSON(writer, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	input, ok := readDeleteTasksRequest(writer, request)
	if !ok {
		return
	}
	a.mu.Lock()
	ids := (taskSelection{IDs: input.IDs}).idsLocked(a)
	// long: 合并进程持有分集路径快照；先拒绝同剧删除，避免 FFmpeg 读写途中移走文件。
	for _, id := range ids {
		if task := a.tasks[id]; task != nil && a.mergeJobs[task.DramaID] != nil {
			a.mu.Unlock()
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "该剧正在合并，请先取消合并并等待结束后再删除"})
			return
		}
	}
	for _, dramaID := range input.MergedDramaIDs {
		if a.mergeJobs[dramaID] != nil {
			a.mu.Unlock()
			writeJSON(writer, http.StatusConflict, map[string]string{"error": "该剧正在合并，请先取消合并并等待结束后再删除"})
			return
		}
	}
	// long: 批量删除先检查全部路径，防止部分资源已删后才发现目录越界或软链接。
	for _, id := range ids {
		if task := a.tasks[id]; task != nil {
			if err := a.validateTaskFilesForDelete(task); err != nil {
				a.mu.Unlock()
				writeJSON(writer, http.StatusConflict, map[string]string{"error": "分集文件不在受管理的下载目录内或不是普通文件：" + a.redactError(err)})
				return
			}
		}
	}
	for _, dramaID := range input.MergedDramaIDs {
		if merge := a.merges[dramaID]; merge != nil && merge.OutputPath != "" {
			if err := a.validateDeletePath(merge.OutputPath, dramaID); err != nil {
				a.mu.Unlock()
				writeJSON(writer, http.StatusConflict, map[string]string{"error": "全集文件不在受管理的下载目录内或不是普通文件：" + a.redactError(err)})
				return
			}
		}
	}
	if err := a.validateDeleteReferencesLocked(ids, input.MergedDramaIDs); err != nil {
		a.mu.Unlock()
		writeJSON(writer, http.StatusConflict, map[string]string{"error": a.redactError(err)})
		return
	}
	// long: 文件删除无法回滚；先持久化整批删除意图，崩溃或收尾保存失败时由启动恢复继续清理。
	snapshot := a.snapshotTaskMutationLocked()
	var markedMerges []string
	for _, dramaID := range input.MergedDramaIDs {
		if merge := a.merges[dramaID]; merge != nil {
			a.disableAutoMergeLocked(dramaID)
			marked := *merge
			marked.DeleteRequested = true
			a.merges[dramaID] = &marked
			markedMerges = append(markedMerges, dramaID)
		}
	}
	for _, id := range ids {
		if task := a.tasks[id]; task != nil {
			task.RemoveRequested, task.DeleteFilesRequested = true, true
			a.disableAutoMergeLocked(task.DramaID)
			if task.Status == uiStatusRunning || task.Status == uiStatusParsing {
				task.CancelRequested = true
			}
		}
	}
	if err := a.saveStateLockedWithDiagnostic("ui.task_delete_intent_save_failed"); err != nil {
		a.restoreTaskMutationLocked(snapshot)
		a.mu.Unlock()
		a.writeStatePersistenceError(writer, err)
		return
	}
	removed, mergedRemoved, pending, deletedFiles, failed := 0, 0, 0, 0, 0
	var problems []string
	var cancels []context.CancelFunc
	for _, dramaID := range input.MergedDramaIDs {
		merge := a.merges[dramaID]
		if merge == nil {
			continue
		}
		if merge.OutputPath != "" {
			deleted, err := a.deleteManagedPath(merge.OutputPath, dramaID)
			if err != nil {
				failed++
				problems = append(problems, merge.DramaTitle+"："+a.redactError(err))
				continue
			}
			if deleted {
				deletedFiles++
			}
		}
		delete(a.merges, dramaID)
		if merge.OutputPath != "" {
			// long: 全集记录移除后才检查目录，避免把仍被分集任务使用的下载目录提前回收。
			a.cleanupDownloadDirectoryLocked(filepath.Dir(merge.OutputPath), dramaID)
		}
		mergedRemoved++
	}
	for _, id := range ids {
		task := a.tasks[id]
		if task == nil {
			continue
		}
		if task.Status == uiStatusRunning || task.Status == uiStatusParsing {
			// long: 进行中的文件仍由 worker 在退出后删除，避免与下载完成的原子重命名竞争。
			if cancel := a.runningCancels[id]; cancel != nil {
				cancels = append(cancels, cancel)
			}
			if task.Status == uiStatusParsing {
				if cancel := a.parsingCancels[task.DramaID]; cancel != nil {
					cancels = append(cancels, cancel)
				}
			}
			pending++
			continue
		}
		deleted, err := a.deleteTaskFiles(task)
		deletedFiles += deleted
		if err != nil {
			failed++
			task.RemoveRequested, task.DeleteFilesRequested = false, false
			task.Status, task.Phase = uiStatusFailed, "failed"
			task.Error = "删除本地文件失败：" + a.redactError(err)
			problems = append(problems, firstNonEmpty(task.Title, task.Episode, task.ID)+"："+a.redactError(err))
			continue
		}
		a.removeTaskLocked(id)
		// long: 任务记录移除后再回收剧集目录；未选中的任务或全集仍存在时会阻止回收。
		a.cleanupDownloadDirectoryLocked(filepath.Dir(firstNonEmpty(task.Path, task.Source.OutPath)), task.DramaID)
		removed++
	}
	saveErr := a.saveStateLockedWithDiagnostic("ui.task_delete_save_failed")
	views := a.taskViewsLocked()
	a.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	// long: 分集删除会改变自动同步的目标集合；无论是完成删除、失败重试还是等待运行任务收尾，都要立即唤醒同步清理旧入口。
	if len(ids)+len(markedMerges) > 0 {
		a.notifyEmbySync()
	}
	// long: 删除意图已保存后全集不可再播放；即使磁盘删除或最终保存失败，也要让 Emby 及时撤下旧入口。
	for _, id := range markedMerges {
		a.notifyEmbyMerge(id)
	}
	if saveErr != nil {
		a.writeStatePersistenceError(writer, saveErr)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"data": views, "removed": removed, "mergedRemoved": mergedRemoved, "pending": pending, "deletedFiles": deletedFiles, "failed": failed, "errors": problems})
}

func (a *UIApp) validateTaskFilesForDelete(task *UITask) error {
	if isChapterPlaceholderTask(task) {
		return nil
	}
	path := firstNonEmpty(task.Path, task.Source.OutPath)
	if path == "" {
		return errors.New("任务没有下载文件路径")
	}
	if task.Path != "" && task.Source.OutPath != "" && filepath.Clean(task.Path) != filepath.Clean(task.Source.OutPath) {
		return errors.New("任务文件路径不一致")
	}
	if strings.HasSuffix(strings.ToLower(path), ".part.mp4") {
		return errors.New("任务最终文件不能是下载临时文件")
	}
	if err := a.validateDeletePath(path, task.DramaID); err != nil {
		return err
	}
	return a.validateDeletePath(strings.TrimSuffix(path, filepath.Ext(path))+".part.mp4", task.DramaID)
}

func (a *UIApp) validateDeleteReferencesLocked(ids, mergedIDs []string) error {
	selectedTasks := map[string]bool{}
	selectedMerges := map[string]bool{}
	deleting := map[string]bool{}
	for _, id := range ids {
		selectedTasks[id] = true
		if task := a.tasks[id]; task != nil && !isChapterPlaceholderTask(task) {
			deleting[filepath.Clean(firstNonEmpty(task.Path, task.Source.OutPath))] = true
		}
	}
	for _, id := range mergedIDs {
		selectedMerges[id] = true
		if merge := a.merges[id]; merge != nil && merge.OutputPath != "" {
			deleting[filepath.Clean(merge.OutputPath)] = true
		}
	}
	for id, task := range a.tasks {
		if selectedTasks[id] || task == nil || isChapterPlaceholderTask(task) {
			continue
		}
		if path := firstNonEmpty(task.Path, task.Source.OutPath); path != "" && deleting[filepath.Clean(path)] {
			return errors.New("所选文件仍被未选中的分集任务使用，请一并选择后再删除")
		}
	}
	for id, merge := range a.merges {
		if selectedMerges[id] || merge == nil || merge.OutputPath == "" {
			continue
		}
		if deleting[filepath.Clean(merge.OutputPath)] {
			return errors.New("所选文件仍被未选中的全集使用，请一并选择后再删除")
		}
	}
	return nil
}

func (a *UIApp) deleteTaskFiles(task *UITask) (int, error) {
	if err := a.validateTaskFilesForDelete(task); err != nil {
		return 0, err
	}
	if isChapterPlaceholderTask(task) {
		return 0, nil
	}
	path := firstNonEmpty(task.Path, task.Source.OutPath)
	part := strings.TrimSuffix(path, filepath.Ext(path)) + ".part.mp4"
	removed := 0
	for _, candidate := range []string{path, part} {
		deleted, err := a.deleteManagedPath(candidate, task.DramaID)
		if err != nil {
			return removed, err
		}
		if deleted {
			removed++
		}
	}
	return removed, nil
}

func (a *UIApp) cleanupDownloadDirectoryLocked(directory, dramaID string) {
	if directory == "" || dramaID == "" || filepath.Base(directory) == "." || filepath.Base(directory) == string(filepath.Separator) {
		return
	}
	root, rootErr := filepath.Abs(a.cfg.OutputDir)
	dir, dirErr := filepath.Abs(directory)
	if rootErr != nil || dirErr != nil || dir == root {
		return
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	marker := filepath.Join(directory, ".drama-id")
	markerInfo, err := os.Lstat(marker)
	if err != nil || !markerInfo.Mode().IsRegular() || markerInfo.Size() > 512 {
		return
	}
	body, err := os.ReadFile(marker)
	if err != nil || strings.TrimSpace(string(body)) != dramaID {
		return
	}
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 || entries[0].Name() != filepath.Base(marker) {
		return
	}
	for _, task := range a.tasks {
		if task == nil {
			continue
		}
		for _, path := range []string{task.Path, task.Source.OutPath} {
			if path != "" && filepath.Dir(filepath.Clean(path)) == filepath.Clean(directory) {
				return
			}
		}
	}
	for _, merge := range a.merges {
		if merge == nil {
			continue
		}
		for _, path := range append([]string{merge.OutputPath}, merge.CleanupPending...) {
			if path != "" && filepath.Dir(filepath.Clean(path)) == filepath.Clean(directory) {
				return
			}
		}
	}
	if err := os.Remove(marker); err != nil {
		return
	}
	if err := os.Remove(directory); err != nil {
		// long: 并发下载或其他写入使目录不再为空时恢复标记，保留后续识别剧集归属的能力。
		if _, statErr := os.Lstat(marker); errors.Is(statErr, os.ErrNotExist) {
			if file, createErr := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, markerInfo.Mode().Perm()); createErr == nil {
				_, _ = file.Write([]byte(dramaID))
				_ = file.Close()
			}
		}
	}
}

func (a *UIApp) deleteManagedPath(path, dramaID string) (bool, error) {
	if err := a.validateDeletePath(path, dramaID); err != nil {
		return false, err
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, removeAutoMergedReceipt(path, dramaID)
	}
	if err != nil {
		return false, err
	}
	return true, removeAutoMergedReceipt(path, dramaID)
}

func (a *UIApp) validateDeletePath(path, dramaID string) error {
	if path == "" || !strings.EqualFold(filepath.Ext(path), ".mp4") {
		return errors.New("仅允许删除已登记的 MP4 文件")
	}
	root, err := filepath.Abs(a.cfg.OutputDir)
	if err != nil {
		return err
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, target)
	currentRoot := err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	if !currentRoot {
		// long: 更改保存目录后旧任务仍指向原目录；原目录的剧集标记必须与任务归属一致。
		parentInfo, parentErr := os.Lstat(filepath.Dir(target))
		if parentErr != nil || !parentInfo.IsDir() || parentInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("原下载目录不是普通文件夹：%s", path)
		}
		marker := filepath.Join(filepath.Dir(target), ".drama-id")
		info, markerErr := os.Lstat(marker)
		if markerErr != nil || !info.Mode().IsRegular() || info.Size() > 512 {
			return fmt.Errorf("文件超出下载目录且没有有效剧集标记：%s", path)
		}
		body, markerErr := os.ReadFile(marker)
		if markerErr != nil || strings.TrimSpace(string(body)) != dramaID {
			return fmt.Errorf("原下载目录的剧集标记不匹配：%s", path)
		}
	}
	if currentRoot {
		rootReal, err := filepath.EvalSymlinks(root)
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		parentReal, err := filepath.EvalSymlinks(filepath.Dir(target))
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if err != nil {
			return err
		}
		rel, err = filepath.Rel(rootReal, parentReal)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("文件所在目录超出下载目录：%s", path)
		}
	}
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("文件不是普通文件：%s", path)
	}
	return nil
}
