package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type embyFolderManifest struct {
	Version     int           `json:"version"`
	DramaID     string        `json:"dramaId"`
	Mode        string        `json:"mode,omitempty"`
	RetiredMode string        `json:"retiredMode,omitempty"`
	Chapters    []embyChapter `json:"chapters"`
}

func validEmbyFolder(folder string) bool {
	return folder == "" || len(folder) <= 240 && folder != "." && folder != ".." && !strings.ContainsAny(folder, "/\\\x00\r\n") && filepath.Base(folder) == folder
}

func ensureEmbyDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, 0o2775)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Emby 生成目录不是普通文件夹，已停止写入")
	}
	// long: Emby 删除媒体时需要对父目录有写权限；保留普通文件夹并开放共享组写入，实际用户组由部署配置决定。
	if err := os.Chmod(path, 0o2775); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	return nil
}

func syncEmbyDramaFiles(ctx context.Context, settings embySyncSettings, drama Drama, chapters []Chapter, folder string, key []byte, owner string) (string, int, int, error) {
	return syncEmbyManagedFiles(ctx, settings, drama, chapters, nil, folder, key, owner)
}

func syncEmbyMergedFiles(ctx context.Context, settings embySyncSettings, drama Drama, merge UIMergeState, folder string, key []byte, owner string) (string, int, int, error) {
	if merge.DramaID != drama.ID || merge.Status != "success" || merge.DeleteRequested || completedPlaybackPath(&UITask{Status: uiStatusSuccess, Path: merge.OutputPath}) == "" {
		return folder, 0, 0, errors.New("本地合并文件不存在或已移动")
	}
	return syncEmbyManagedFiles(ctx, settings, drama, nil, &merge, folder, key, owner)
}

func syncEmbyManagedFiles(ctx context.Context, settings embySyncSettings, drama Drama, chapters []Chapter, merge *UIMergeState, folder string, key []byte, owner string) (string, int, int, error) {
	if !validEmbyFolder(folder) {
		return folder, 0, 0, errors.New("Emby 剧集目录无效")
	}
	if folder == "" {
		folder = embyFolderName(drama)
	}
	if err := ensureSharedMediaDirectory(settings.OutputDir); err != nil {
		return folder, 0, 0, err
	}
	root, err := filepath.EvalSymlinks(settings.OutputDir)
	if err != nil {
		return folder, 0, 0, err
	}
	// long: 输出根目录也必须允许 Emby 删除已退休的剧集目录，否则只清掉文件仍会留下空目录。
	target := filepath.Join(root, folder)
	work := target
	var previous embyFolderManifest
	staged := false
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		work, err = os.MkdirTemp(root, ".duanju-emby-")
		if err != nil {
			return folder, 0, 0, err
		}
		staged = true
		defer os.RemoveAll(work)
		if err = os.Chmod(work, 0o2775); err != nil {
			return folder, 0, 0, err
		}
	} else {
		if err != nil {
			return folder, 0, 0, err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return folder, 0, 0, errors.New("Emby 剧集目录不是普通文件夹")
		}
		manifestPath := filepath.Join(work, ".duanju-emby.json")
		info, err := os.Lstat(manifestPath)
		if err != nil || !info.Mode().IsRegular() {
			return folder, 0, 0, errors.New("同名目录不属于自动同步，原内容已保留；请使用单独的 Emby 输出目录")
		}
		if err = readEmbyJSON(manifestPath, &previous, 8<<20); err != nil {
			return folder, 0, 0, err
		}
		if previous.Version != 1 || previous.DramaID != drama.ID || !validEmbyManifestMode(previous.Mode) || !validEmbyRetiredMode(previous.RetiredMode) {
			return folder, 0, 0, errors.New("Emby 目录所属剧集不匹配，原内容已保留")
		}
	}
	if err = ensureEmbyDirectory(work); err != nil {
		return folder, 0, 0, err
	}
	var records []embyChapter
	if merge == nil {
		records, err = embyChapterRecords(drama.ID, chapters, previous.Chapters)
		if err != nil {
			return folder, 0, 0, err
		}
	}
	if err = ensureEmbyDirectory(filepath.Join(work, "Season 01")); err != nil {
		return folder, 0, 0, err
	}
	written := 0
	if merge != nil && previous.Version == 1 && previous.Mode != "merged" {
		// long: 从分集切到全集时只撤下签名匹配的旧 STRM，原 MP4 和用户自建文件始终留在原处。
		retired, retireErr := retireEmbySTRMs(ctx, work, previous, key)
		written += retired
		if retireErr != nil {
			return folder, written, 0, retireErr
		}
	}
	// long: 模式切换时退休清理可能移除了空的 Season 目录，写入新的播放入口前重新建立它。
	if err = ensureEmbyDirectory(filepath.Join(work, "Season 01")); err != nil {
		return folder, written, 0, err
	}
	write := func(name string, body []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		changed, err := writeEmbyFile(filepath.Join(work, filepath.FromSlash(name)), body, 0664)
		if changed {
			written++
		}
		return err
	}
	if merge == nil {
		err = writeEmbyFiles(drama, records, settings.BaseURL, key, owner, write)
	} else {
		err = writeEmbyMergedFiles(drama, *merge, settings.BaseURL, key, owner, write)
	}
	if err == nil {
		mode := "episodes"
		if merge != nil {
			mode, records = "merged", previous.Chapters
		}
		body, marshalErr := json.MarshalIndent(embyFolderManifest{Version: 1, DramaID: drama.ID, Mode: mode, Chapters: records}, "", "  ")
		err = marshalErr
		if err == nil {
			_, err = writeEmbyFile(filepath.Join(work, ".duanju-emby.json"), append(body, '\n'), 0600)
		}
	}
	if err == nil {
		err = ctx.Err()
	}
	if staged {
		if err == nil {
			err = os.Rename(work, target)
		}
		if err != nil {
			written = 0
		}
	}
	if merge != nil {
		return folder, written, 1, err
	}
	return folder, written, len(records), err
}

func validEmbyManifestMode(mode string) bool {
	return mode == "" || mode == "episodes" || mode == "merged" || mode == "retired"
}

func validEmbyRetiredMode(mode string) bool {
	return mode == "" || mode == "episodes" || mode == "merged"
}

func writeEmbyFile(path string, body []byte, mode os.FileMode) (bool, error) {
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() || info.Size() > 8<<20 {
			return false, errors.New("Emby 目标文件不是普通文件或文件过大，已停止写入")
		}
		old, err := os.ReadFile(path)
		if err != nil {
			return false, err
		}
		if bytes.Equal(old, body) {
			return false, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".duanju-emby-write-")
	if err != nil {
		return false, err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(mode); err == nil {
		_, err = file.Write(body)
	}
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return false, err
	}
	if err = os.Rename(file.Name(), path); err != nil {
		return false, fmt.Errorf("Emby 文件替换失败：%w", err)
	}
	return true, nil
}
