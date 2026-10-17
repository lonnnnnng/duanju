package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func localEmbyPosterName(name string) bool {
	stem, extension := strings.TrimSuffix(strings.ToLower(name), strings.ToLower(filepath.Ext(name))), strings.ToLower(filepath.Ext(name))
	return (stem == "poster" || stem == "folder") &&
		(extension == ".jpg" || extension == ".jpeg" || extension == ".png" || extension == ".webp" || extension == ".gif" || extension == ".tbn")
}

func customEmbyLocalPoster(ctx context.Context, folder string, manifest embyFolderManifest) (bool, error) {
	entries, err := os.ReadDir(folder)
	if err != nil {
		return false, err
	}
	for _, entry := range entries {
		if !localEmbyPosterName(entry.Name()) {
			continue
		}
		info, err := os.Lstat(filepath.Join(folder, entry.Name()))
		if err != nil {
			return false, err
		}
		if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCoverBytes {
			return true, nil
		}
		actual, _, err := hashEmbyLocalFile(ctx, filepath.Join(folder, entry.Name()), entry.Name())
		if err != nil {
			return false, err
		}
		if !sameEmbyLocalFile(actual, manifest.Poster) && !sameEmbyLocalFile(actual, manifest.PendingPoster) {
			return true, nil
		}
	}
	return false, nil
}

func (app *UIApp) localEmbyPosterImage(ctx context.Context, data []byte) ([]byte, string, error) {
	kind := imageContentType(data)
	if kind != "image/jpeg" && kind != "image/png" {
		// long: 部分站源提供 WebP/GIF；将单张封面转成 JPEG，保证导出的媒体目录可被独立扫描和迁移。
		ffmpeg, err := app.downloader.ensureFFmpeg(ctx)
		if err != nil {
			return nil, "", err
		}
		convertCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		command := exec.CommandContext(convertCtx, ffmpeg, "-hide_banner", "-loglevel", "error", "-nostdin",
			"-max_alloc", "67108864", "-protocol_whitelist", "pipe", "-threads", "1", "-i", "pipe:0",
			"-frames:v", "1", "-an", "-sn", "-vf", "scale=w='min(1600,iw)':h='min(1600,ih)':force_original_aspect_ratio=decrease",
			"-filter_threads", "1", "-threads", "1", "-c:v", "mjpeg", "-f", "image2pipe", "pipe:1")
		var output coverOutput
		stderr := &cappedStringWriter{limit: 4096}
		command.Stdin, command.Stdout, command.Stderr = bytes.NewReader(data), &output, stderr
		if err = command.Run(); err != nil {
			if convertCtx.Err() != nil {
				return nil, "", convertCtx.Err()
			}
			detail := strings.TrimSpace(stderr.String())
			if detail == "" {
				detail = err.Error()
			}
			return nil, "", fmt.Errorf("本地海报转换失败：%s", app.redactString(detail))
		}
		data, kind = output.Bytes(), "image/jpeg"
	}
	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 16000000 || len(data) > maxCoverBytes {
		return nil, "", errors.New("本地海报不是有效图片或尺寸过大")
	}
	name := "poster.jpg"
	if kind == "image/png" {
		name = "poster.png"
	}
	return data, name, nil
}

func recoverEmbyLocalPoster(ctx context.Context, folder string, manifest *embyFolderManifest) (int, error) {
	if manifest.PendingPoster == nil {
		return 0, nil
	}
	pending := manifest.PendingPoster
	actual, _, err := hashEmbyLocalFile(ctx, filepath.Join(folder, pending.Name), pending.Name)
	if err != nil {
		return 0, err
	}
	if !sameEmbyLocalFile(actual, pending) {
		return 0, nil
	}
	written := 1
	// long: 海报已发布但状态尚未提交时，先完成旧发布的收尾，再处理新地址，避免更换图片格式后遗留无法识别的旧海报。
	if manifest.Poster != nil && manifest.Poster.Name != pending.Name {
		old := filepath.Join(folder, manifest.Poster.Name)
		actual, _, err := hashEmbyLocalFile(ctx, old, manifest.Poster.Name)
		if err != nil {
			return written, err
		}
		if sameEmbyLocalFile(actual, manifest.Poster) {
			if err = os.Remove(old); err != nil {
				return written, err
			}
			written++
		}
	}
	manifest.Poster, manifest.PendingPoster = pending, nil
	err = writeEmbyJSON(filepath.Join(folder, ".duanju-emby.json"), manifest, 0600)
	return written, err
}

func (manager *embySyncManager) syncLocalPoster(ctx context.Context, settings embySyncSettings, id, folderName string, poster *embyPosterState) (int, bool, error) {
	if !validEmbyFolder(folderName) || folderName == "" {
		return 0, false, errors.New("Emby 海报目录无效")
	}
	folder := filepath.Join(settings.OutputDir, folderName)
	info, err := os.Lstat(folder)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return 0, false, errors.New("Emby 海报目录不是普通文件夹")
	}
	marker := filepath.Join(folder, ".duanju-emby.json")
	info, err = os.Lstat(marker)
	if err != nil || !info.Mode().IsRegular() {
		return 0, false, errors.New("Emby 海报目录缺少自动同步清单")
	}
	var manifest embyFolderManifest
	if err = readEmbyJSON(marker, &manifest, 8<<20); err != nil {
		return 0, false, err
	}
	if manifest.Version != 1 || manifest.DramaID != id || manifest.Mode != embyLocalMergedMode || !validEmbyLocalManifest(manifest) {
		return 0, false, errors.New("Emby 海报目录所属剧集不匹配")
	}
	written, err := recoverEmbyLocalPoster(ctx, folder, &manifest)
	if err != nil {
		return written, false, err
	}
	custom, err := customEmbyLocalPoster(ctx, folder, manifest)
	if err != nil || custom {
		return written, custom, err
	}
	if manifest.Poster != nil && manifest.PendingPoster == nil && poster.SyncedSource == embyPosterSourceHash(poster.SourceURL) {
		actual, _, err := hashEmbyLocalFile(ctx, filepath.Join(folder, manifest.Poster.Name), manifest.Poster.Name)
		if err != nil {
			return written, false, err
		}
		if sameEmbyLocalFile(actual, manifest.Poster) {
			return written, false, nil
		}
	}
	source, _, _ := splitProviderDramaID(id)
	requestCtx, cancel := context.WithTimeout(context.WithValue(ctx, coverSourceKey{}, source), 90*time.Second)
	defer cancel()
	data, err := manager.app.loadCoverImage(requestCtx, poster.SourceURL, nil)
	if err != nil {
		return written, false, err
	}
	data, name, err := manager.app.localEmbyPosterImage(requestCtx, data)
	if err != nil {
		return written, false, err
	}
	// long: 下载海报可能耗时；发布前再确认目录中的封面仍由程序管理，期间由用户换上的图片必须保留。
	custom, err = customEmbyLocalPoster(ctx, folder, manifest)
	if err != nil || custom {
		return written, custom, err
	}
	hash := sha256.Sum256(data)
	wanted := &embyLocalFile{Name: name, Size: int64(len(data)), SHA256: hex.EncodeToString(hash[:])}
	manifest.PendingPoster = wanted
	if err = writeEmbyJSON(marker, manifest, 0600); err != nil {
		return written, false, err
	}
	changed, err := writeEmbyFile(filepath.Join(folder, name), data, 0660)
	if changed {
		written++
	}
	if err != nil {
		return written, false, err
	}
	if manifest.Poster != nil && manifest.Poster.Name != name {
		old := filepath.Join(folder, manifest.Poster.Name)
		actual, _, err := hashEmbyLocalFile(ctx, old, manifest.Poster.Name)
		if err != nil {
			return written, false, err
		}
		if sameEmbyLocalFile(actual, manifest.Poster) {
			if err = os.Remove(old); err != nil {
				return written, false, err
			}
			written++
		}
	}
	manifest.Poster, manifest.PendingPoster = wanted, nil
	if err = writeEmbyJSON(marker, manifest, 0600); err != nil {
		return written, false, err
	}
	return written, false, nil
}

func (manager *embySyncManager) synchronizeLocalPosters(ctx context.Context, document *embySyncDocument, targets []Drama) {
	interval, checked := time.Duration(document.Settings.IntervalMinutes)*time.Minute, 0
	for _, drama := range targets {
		entry := document.Entries[drama.ID]
		poster := entry.Poster
		if entry.Episodes == 0 {
			continue
		}
		if next := embyPosterDue(poster, interval); next.IsZero() || next.After(time.Now()) {
			continue
		}
		if ctx.Err() != nil || checked >= 20 {
			break
		}
		checked++
		document.PosterChecked++
		written, custom, err := manager.syncLocalPoster(ctx, document.Settings, drama.ID, entry.Folder, &poster)
		document.WrittenFiles += written
		if written > 0 {
			document.PostersUpdated++
			document.RefreshPending = document.Settings.ServerURL != ""
		}
		if err != nil {
			manager.postponePoster(&poster, false, err, document.Settings.APIKey)
		} else {
			poster.Status, poster.Error = "synced", ""
			if custom {
				poster.Status = "existing"
			}
			poster.SyncedSource = embyPosterSourceHash(poster.SourceURL)
			poster.CheckedAt, poster.SyncedAt, poster.RetryAt, poster.Failures = time.Now(), time.Now(), time.Time{}, 0
		}
		entry.Poster = poster
		document.Entries[drama.ID] = entry
	}
}
