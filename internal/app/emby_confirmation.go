package app

import (
	"context"
	"duanju/internal/embycheck"
	"path/filepath"
	"time"
)

func (manager *embySyncManager) confirmMedia(ctx context.Context) {
	manager.mu.Lock()
	document := manager.document
	settings := document.Settings
	if manager.running || manager.confirming || manager.closed || manager.loadErr != nil || !settings.Enabled || !settings.MergedOnly || settings.ServerURL == "" || settings.APIKey == "" || document.RefreshPending {
		manager.mu.Unlock()
		return
	}
	targets := []embycheck.Target{}
	syncedAt := map[string]time.Time{}
	for id, entry := range document.Entries {
		if entry.Episodes <= 0 || entry.Folder == "" || entry.Media.NextCheckAt.After(time.Now()) {
			continue
		}
		suffix := filepath.Join(entry.Folder, filepath.FromSlash(embyLocalMergedName))
		targets = append(targets, embycheck.Target{ID: id, LocalPath: filepath.Join(settings.OutputDir, suffix), Suffix: suffix, Previous: entry.Media})
		syncedAt[id] = entry.SyncedAt
	}
	if len(targets) == 0 {
		manager.mu.Unlock()
		return
	}
	generation := manager.generation
	manager.confirming = true
	queryCtx, cancel := context.WithCancel(ctx)
	manager.confirmCancel = cancel
	manager.mu.Unlock()
	results, _ := embycheck.Check(queryCtx, settings.ServerURL, settings.APIKey, targets)
	queryErr := queryCtx.Err()
	cancel()
	manager.mu.Lock()
	defer manager.mu.Unlock()
	manager.confirming = false
	manager.confirmCancel = nil
	// long: 扫描接受与媒体识别分开保存；合并、删除或换配置发生在查询期间时，旧结果不能覆盖新状态。
	if queryErr != nil || manager.closed || generation != manager.generation || manager.document.RefreshPending {
		return
	}
	for id, result := range results {
		entry, exists := manager.document.Entries[id]
		if exists && entry.Episodes > 0 && entry.SyncedAt.Equal(syncedAt[id]) {
			entry.Media = result
			manager.document.Entries[id] = entry
		}
	}
	if err := writeEmbyJSON(manager.path, manager.document, 0600); err != nil {
		manager.document.Error = "入库确认状态保存失败：" + manager.safeError(err, settings.APIKey)
	}
}
