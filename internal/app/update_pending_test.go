package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparePendingUpdateRecordsFirstAttempt(t *testing.T) {
	dataDir := t.TempDir()
	backup := filepath.Join(dataDir, "old-duanju")
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	update := pendingUpdate{Version: 1, TargetPath: filepath.Join(dataDir, "duanju"), BackupPath: backup}
	if err := writePendingUpdate(dataDir, update); err != nil {
		t.Fatal(err)
	}
	if err := preparePendingUpdate(dataDir); err != nil {
		t.Fatal(err)
	}
	got, err := readPendingUpdate(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Attempts != 1 {
		t.Fatalf("first startup attempt = %d, want 1", got.Attempts)
	}
}

func TestPreparePendingUpdateRestoresBackupOnSecondAttempt(t *testing.T) {
	dataDir := t.TempDir()
	target := filepath.Join(dataDir, "duanju")
	backup := filepath.Join(dataDir, "duanju.bak")
	if err := os.WriteFile(target, []byte("broken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePendingUpdate(dataDir, pendingUpdate{Version: 1, TargetPath: target, BackupPath: backup, Attempts: 1}); err != nil {
		t.Fatal(err)
	}
	if err := preparePendingUpdate(dataDir); !errors.Is(err, errPendingUpdateRestored) {
		t.Fatalf("rollback must request process restart: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old" {
		t.Fatalf("restored content = %q, want old", content)
	}
	if _, err := os.Stat(pendingUpdatePath(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("pending update marker still exists: %v", err)
	}
}

func TestMarkPendingUpdateReadyRemovesMarker(t *testing.T) {
	dataDir := t.TempDir()
	backup := filepath.Join(dataDir, "duanju.bak")
	if err := os.WriteFile(backup, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writePendingUpdate(dataDir, pendingUpdate{Version: 1, TargetPath: "target", BackupPath: backup}); err != nil {
		t.Fatal(err)
	}
	if err := markPendingUpdateReady(dataDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pendingUpdatePath(dataDir)); !os.IsNotExist(err) {
		t.Fatalf("pending update marker still exists: %v", err)
	}
	if _, err := os.Stat(backup); !os.IsNotExist(err) {
		t.Fatalf("confirmed update backup still exists: %v", err)
	}
}
