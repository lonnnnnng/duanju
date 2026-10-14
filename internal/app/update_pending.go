package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const pendingUpdateFile = "update-pending.json"

var errPendingUpdateRestored = errors.New("已恢复更新前程序，需要重新启动服务")

type pendingUpdate struct {
	Version    int    `json:"version"`
	TargetPath string `json:"targetPath"`
	BackupPath string `json:"backupPath"`
	Attempts   int    `json:"attempts"`
}

func pendingUpdatePath(dataDir string) string {
	return filepath.Join(dataDir, pendingUpdateFile)
}

func writePendingUpdate(dataDir string, update pendingUpdate) error {
	if update.Version != 1 || update.TargetPath == "" || update.BackupPath == "" || update.Attempts < 0 {
		return errors.New("更新启动标记无效")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return err
	}
	body, err := json.MarshalIndent(update, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(dataDir, ".duanju-update-pending-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err = file.Chmod(0o600); err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporary, pendingUpdatePath(dataDir))
}

func stagePendingUpdate(dataDir, targetPath, backupPath string) error {
	return writePendingUpdate(dataDir, pendingUpdate{
		Version:    1,
		TargetPath: targetPath,
		BackupPath: backupPath,
	})
}

func readPendingUpdate(dataDir string) (pendingUpdate, error) {
	body, err := os.ReadFile(pendingUpdatePath(dataDir))
	if err != nil {
		return pendingUpdate{}, err
	}
	var update pendingUpdate
	if err := json.Unmarshal(body, &update); err != nil {
		return pendingUpdate{}, fmt.Errorf("解析更新启动标记失败：%w", err)
	}
	if update.Version != 1 || update.TargetPath == "" || update.BackupPath == "" || update.Attempts < 0 {
		return pendingUpdate{}, errors.New("更新启动标记内容无效")
	}
	return update, nil
}

func preparePendingUpdate(dataDir string) error {
	update, err := readPendingUpdate(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if update.Attempts == 0 {
		// long: 第一次启动只记录一次尝试；新程序能成功监听后会删除标记，启动中途失败则下次启动进入回滚分支。
		update.Attempts = 1
		return writePendingUpdate(dataDir, update)
	}
	if err := restorePendingUpdate(update); err != nil {
		return fmt.Errorf("恢复更新前版本失败：%w", err)
	}
	if err := os.Remove(pendingUpdatePath(dataDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("清理更新启动标记失败：%w", err)
	}
	fmt.Printf("检测到更新后的程序未能启动，已恢复备份：%s\n", update.BackupPath)
	// long: 当前进程仍是更新后的程序；必须退出，让 systemd 下次从已恢复的磁盘程序启动，不能误报新版本已就绪。
	return errPendingUpdateRestored
}

func restorePendingUpdate(update pendingUpdate) error {
	input, err := os.Open(update.BackupPath)
	if err != nil {
		return err
	}
	defer input.Close()
	temporary, err := os.CreateTemp(filepath.Dir(update.TargetPath), ".duanju-update-rollback-*")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err = temporary.Chmod(0o755); err == nil {
		_, err = io.Copy(temporary, input)
	}
	if err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(temporaryName, update.TargetPath)
}

func markPendingUpdateReady(dataDir string) error {
	update, err := readPendingUpdate(dataDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := os.Remove(pendingUpdatePath(dataDir)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("清理更新启动标记失败：%w", err)
	}
	// long: 新程序已经成功监听，旧程序只需保留到这一刻；及时删除备份避免长期累积可执行文件。
	if err := os.Remove(update.BackupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("清理更新备份失败（可手动删除）：%s：%v\n", update.BackupPath, err)
	}
	return nil
}
