package app

import (
	"context"
	"duanju/internal/embycheck"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func confirmationFixture(t *testing.T) (*UIApp, *embySyncManager, string) {
	t.Helper()
	app := viewerTestApp(t)
	manager := app.embySyncer()
	settings, drama, merge, key := localEmbyFixture(t, 0640)
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	settings.Enabled = true
	manager.document.Settings = settings
	manager.document.Entries = map[string]embySyncEntry{drama.ID: {Folder: folder, Episodes: 1, SyncedAt: time.Now()}}
	return app, manager, filepath.Join(settings.OutputDir, folder, filepath.FromSlash(embyLocalMergedName))
}

func TestEmbyMediaRecognitionPersistedSeparately(t *testing.T) {
	_, manager, media := confirmationFixture(t)
	var ready atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := []embycheck.Item{}
		if ready.Load() {
			items = append(items, embycheck.Item{ID: "item", Type: "Episode", Path: media, MediaSources: []embycheck.Source{{Path: media, Protocol: "File"}}})
		}
		json.NewEncoder(w).Encode(map[string]any{"Items": items, "TotalRecordCount": len(items)})
	}))
	defer server.Close()
	manager.document.Settings.ServerURL, manager.document.Settings.APIKey = server.URL, "test-key"
	manager.confirmMedia(context.Background())
	entry := manager.document.Entries[historyFixtureDramaID]
	if entry.Episodes != 1 || entry.Media.State != "waiting" {
		t.Fatal("scan delay changed published result", entry)
	}
	ready.Store(true)
	entry.Media.NextCheckAt = time.Time{}
	manager.document.Entries[historyFixtureDramaID] = entry
	manager.confirmMedia(context.Background())
	var stored embySyncDocument
	if err := readEmbyJSON(manager.path, &stored, 8<<20); err != nil {
		t.Fatal(err)
	}
	entry = stored.Entries[historyFixtureDramaID]
	if entry.Episodes != 1 || entry.Media.State != "confirmed" || entry.Media.ItemID != "item" {
		t.Fatal(entry)
	}
	if err := os.Remove(media); err != nil {
		t.Fatal(err)
	}
	entry.Media.NextCheckAt = time.Time{}
	manager.document.Entries[historyFixtureDramaID] = entry
	manager.confirmMedia(context.Background())
	if manager.document.Entries[historyFixtureDramaID].Media.State != "error" {
		t.Fatal("cached Emby entry masked deleted media")
	}
}

func TestEmbyOldConfirmationCannotOverwriteMerge(t *testing.T) {
	for _, cancelQuery := range []bool{false, true} {
		t.Run(map[bool]string{false: "changed-snapshot", true: "new-merge-cancels-query"}[cancelQuery], func(t *testing.T) {
			app, manager, media := confirmationFixture(t)
			started, release := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				close(started)
				select {
				case <-r.Context().Done():
					return
				case <-release:
				}
				json.NewEncoder(w).Encode(map[string]any{"Items": []embycheck.Item{{ID: "old-item", Type: "Episode", Path: media, MediaSources: []embycheck.Source{{Path: media, Protocol: "File"}}}}, "TotalRecordCount": 1})
			}))
			defer server.Close()
			manager.document.Settings.ServerURL, manager.document.Settings.APIKey = server.URL, "test-key"
			done := make(chan struct{})
			go func() { manager.confirmMedia(context.Background()); close(done) }()
			<-started
			if cancelQuery {
				app.notifyEmbyMerge(historyFixtureDramaID)
			} else {
				manager.mu.Lock()
				entry := manager.document.Entries[historyFixtureDramaID]
				entry.SyncedAt = entry.SyncedAt.Add(time.Second)
				manager.document.Entries[historyFixtureDramaID] = entry
				manager.mu.Unlock()
				close(release)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				close(release)
				t.Fatal("new merge remained blocked behind the confirmation query")
			}
			if manager.document.Entries[historyFixtureDramaID].Media.State == "confirmed" {
				t.Fatal("old query overwrote changed media")
			}
		})
	}
}
