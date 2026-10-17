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
	Version       int             `json:"version"`
	DramaID       string          `json:"dramaId"`
	Mode          string          `json:"mode,omitempty"`
	RetiredMode   string          `json:"retiredMode,omitempty"`
	Chapters      []embyChapter   `json:"chapters"`
	Media         *embyLocalFile  `json:"media,omitempty"`
	PendingMedia  *embyLocalFile  `json:"pendingMedia,omitempty"`
	Poster        *embyLocalFile  `json:"localPoster,omitempty"`
	PendingPoster *embyLocalFile  `json:"pendingPoster,omitempty"`
	SourceCache   *embyMediaCache `json:"sourceCache,omitempty"`
	TargetCache   *embyMediaCache `json:"targetCache,omitempty"`
}

func validEmbyFolder(folder string) bool {
	return folder == "" || len(folder) <= 240 && folder != "." && folder != ".." && !strings.ContainsAny(folder, "/\\\x00\r\n") && filepath.Base(folder) == folder
}

func ensureEmbyDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(path, os.ModeSetgid|0775)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("Emby 生成目录不是普通文件夹，已停止写入")
	}
	// long: Emby 删除媒体时需要对父目录有写权限；保留普通文件夹并开放共享组写入，实际用户组由部署配置决定。
	if err := os.Chmod(path, info.Mode().Perm()|os.ModeSetgid|0070); err != nil && !errors.Is(err, os.ErrPermission) {
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
		if err = os.Chmod(work, os.ModeSetgid|0775); err != nil {
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
		if previous.Version != 1 || previous.DramaID != drama.ID || !validEmbyManifestMode(previous.Mode) || !validEmbyRetiredMode(previous.RetiredMode) || !validEmbyLocalManifest(previous) {
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
	transition := previous.Version == 1 && (merge != nil && previous.Mode != embyLocalMergedMode ||
		merge == nil && (previous.Media != nil || previous.PendingMedia != nil))
	manifest := previous
	manifest.Version, manifest.DramaID = 1, drama.ID
	if merge != nil {
		if err = validateEmbySTRMTransition(work, previous, key); err != nil {
			return folder, written, 0, err
		}
		changed, mediaErr := syncEmbyLocalMedia(ctx, work, merge.OutputPath, &manifest)
		if changed {
			written++
		}
		if mediaErr != nil {
			return folder, written, 0, mediaErr
		}
	}
	if transition {
		// long: 完整 MP4 发布成功后才撤下旧 STRM；切回分集时只回收清单校验匹配的入库副本，下载原文件始终保留。
		retiring := previous
		if merge != nil {
			// long: 中断迁移的清单可能已经登记新 MP4；撤旧入口时不能连同刚发布的视频或本地海报一起回收。
			retiring.Media, retiring.PendingMedia, retiring.Poster, retiring.PendingPoster = nil, nil, nil, nil
		}
		retired, retireErr := retireEmbySTRMs(ctx, work, retiring, key)
		written += retired
		if retireErr != nil {
			return folder, written, 0, retireErr
		}
		if merge == nil {
			manifest.Media, manifest.PendingMedia, manifest.Poster, manifest.PendingPoster = nil, nil, nil, nil
			manifest.SourceCache, manifest.TargetCache = nil, nil
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
		err = writeEmbyLocalMergedFiles(drama, *merge, key, write)
	}
	if err == nil {
		mode := "episodes"
		if merge != nil {
			mode, records = embyLocalMergedMode, previous.Chapters
		}
		manifest.Mode, manifest.RetiredMode, manifest.Chapters = mode, "", records
		body, marshalErr := json.MarshalIndent(manifest, "", "  ")
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
	return mode == "" || mode == "episodes" || mode == "merged" || mode == embyLocalMergedMode || mode == "retired"
}

func validEmbyRetiredMode(mode string) bool {
	return mode == "" || mode == "episodes" || mode == "merged" || mode == embyLocalMergedMode
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
