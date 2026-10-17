package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const embyLocalMergedMode = "merged-local"
const embyLocalMergedName = "Season 01/S01E001.mp4"

type embyLocalFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type embyContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader embyContextReader) Read(body []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(body)
}

func validEmbyLocalFile(file *embyLocalFile) bool {
	if file == nil {
		return true
	}
	hash, err := hex.DecodeString(file.SHA256)
	return err == nil && len(hash) == sha256.Size && file.Size > 0 &&
		(file.Name == embyLocalMergedName || (file.Name == "poster.jpg" || file.Name == "poster.png") && file.Size <= maxCoverBytes)
}

func validEmbyLocalManifest(manifest embyFolderManifest) bool {
	return validEmbyLocalFile(manifest.Media) && (manifest.Media == nil || manifest.Media.Name == embyLocalMergedName) &&
		validEmbyLocalFile(manifest.PendingMedia) && (manifest.PendingMedia == nil || manifest.PendingMedia.Name == embyLocalMergedName) &&
		validEmbyLocalFile(manifest.Poster) && (manifest.Poster == nil || manifest.Poster.Name != embyLocalMergedName) &&
		validEmbyLocalFile(manifest.PendingPoster) && (manifest.PendingPoster == nil || manifest.PendingPoster.Name != embyLocalMergedName)
}

func sameEmbyLocalFile(first, second *embyLocalFile) bool {
	return first != nil && second != nil && *first == *second
}

func hashEmbyLocalFile(ctx context.Context, path, name string) (*embyLocalFile, os.FileInfo, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return nil, nil, errors.New("Emby 本地文件不是非空普通文件，原内容已保留")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, nil, errors.New("Emby 本地文件在打开时发生变化，原内容已保留")
	}
	hash := sha256.New()
	size, err := io.CopyBuffer(hash, embyContextReader{ctx: ctx, reader: file}, make([]byte, 128*1024))
	if err != nil {
		return nil, nil, err
	}
	after, err := file.Stat()
	current, pathErr := os.Lstat(path)
	if err != nil || pathErr != nil || !os.SameFile(info, current) || size != info.Size() ||
		after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return nil, nil, errors.New("Emby 本地文件在读取时发生变化，请稍后重试")
	}
	return &embyLocalFile{Name: name, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}, info, nil
}

func checkEmbyLocalMedia(ctx context.Context, folder string, manifest embyFolderManifest) (*embyLocalFile, os.FileInfo, error) {
	actual, info, err := hashEmbyLocalFile(ctx, filepath.Join(folder, filepath.FromSlash(embyLocalMergedName)), embyLocalMergedName)
	if err != nil || actual == nil {
		return actual, info, err
	}
	// long: 清单保存发布前后的校验值；中断重试可识别已发布文件，同名用户视频则绝不能被覆盖或删除。
	if !sameEmbyLocalFile(actual, manifest.Media) && !sameEmbyLocalFile(actual, manifest.PendingMedia) {
		return nil, nil, errors.New("Emby 同名 MP4 不属于本次同步或已被修改，原视频已保留")
	}
	return actual, info, nil
}

func syncEmbyLocalMedia(ctx context.Context, folder, source string, manifest *embyFolderManifest) (bool, error) {
	wanted, sourceInfo, err := hashEmbyLocalFile(ctx, source, embyLocalMergedName)
	if err != nil {
		return false, err
	}
	if wanted == nil {
		return false, errors.New("本地合并文件不存在或已移动")
	}
	target := filepath.Join(folder, filepath.FromSlash(embyLocalMergedName))
	var actual *embyLocalFile
	targetInfo, statErr := os.Lstat(target)
	if statErr == nil && targetInfo.Mode().IsRegular() && os.SameFile(sourceInfo, targetInfo) {
		// long: 硬链接与源文件共用内容，刚完成的源校验已经覆盖入库文件，避免每轮重复读取整部视频。
		actual = wanted
		if !sameEmbyLocalFile(actual, manifest.Media) && !sameEmbyLocalFile(actual, manifest.PendingMedia) {
			return false, errors.New("Emby 同名 MP4 不属于本次同步或已被修改，原视频已保留")
		}
	} else {
		actual, targetInfo, err = checkEmbyLocalMedia(ctx, folder, *manifest)
	}
	if err != nil {
		return false, err
	}
	if sameEmbyLocalFile(actual, wanted) {
		recovered := manifest.PendingMedia != nil || !sameEmbyLocalFile(actual, manifest.Media)
		manifest.Media, manifest.PendingMedia = wanted, nil
		return recovered, nil
	}
	stageDir, err := os.MkdirTemp(filepath.Dir(target), ".duanju-media-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(stageDir)
	stage := filepath.Join(stageDir, "complete")
	stageInfo, err := os.Stat(stageDir)
	if err != nil {
		return false, err
	}
	// long: 硬链接保留源属组，只有源文件允许入库目录的共享组读取时才复用；否则复制继承目标属组且不修改源权限。
	linked := sourceInfo.Mode().Perm()&0040 != 0 && sameEmbyLocalGroup(sourceInfo, stageInfo) && os.Link(source, stage) == nil
	staged := wanted
	if !linked {
		input, err := os.Open(source)
		if err != nil {
			return false, err
		}
		defer input.Close()
		opened, err := input.Stat()
		if err != nil || !os.SameFile(opened, sourceInfo) {
			return false, errors.New("全集在同步期间发生变化，请稍后重试")
		}
		output, err := os.OpenFile(stage, os.O_WRONLY|os.O_CREATE|os.O_EXCL, sourceInfo.Mode().Perm()|0040)
		if err != nil {
			return false, err
		}
		hash := sha256.New()
		size, copyErr := io.CopyBuffer(io.MultiWriter(output, hash), embyContextReader{ctx: ctx, reader: input}, make([]byte, 128*1024))
		if copyErr == nil {
			copyErr = output.Sync()
		}
		closeErr := output.Close()
		if copyErr != nil {
			return false, copyErr
		}
		if closeErr != nil {
			return false, closeErr
		}
		if err = os.Chmod(stage, sourceInfo.Mode().Perm()|0040); err != nil {
			return false, err
		}
		staged = &embyLocalFile{Name: embyLocalMergedName, Size: size, SHA256: hex.EncodeToString(hash.Sum(nil))}
	}
	currentSource, sourceErr := os.Lstat(source)
	if sourceErr != nil || !os.SameFile(currentSource, sourceInfo) || currentSource.Size() != sourceInfo.Size() ||
		!currentSource.ModTime().Equal(sourceInfo.ModTime()) || !sameEmbyLocalFile(staged, wanted) {
		return false, errors.New("全集在同步期间发生变化，旧入库视频已保留")
	}
	// long: 先保存待发布身份再替换 MP4；即使发布后进程中断，下一轮也能辨认视频并完成 STRM 迁移。
	manifest.PendingMedia = wanted
	if err = writeEmbyJSON(filepath.Join(folder, ".duanju-emby.json"), manifest, 0600); err != nil {
		return false, err
	}
	current, err := os.Lstat(target)
	if targetInfo == nil {
		if !errors.Is(err, os.ErrNotExist) {
			return false, errors.New("同步期间出现同名 MP4，原文件已保留")
		}
	} else if err != nil || !os.SameFile(current, targetInfo) || current.Size() != targetInfo.Size() || !current.ModTime().Equal(targetInfo.ModTime()) {
		return false, errors.New("Emby 视频在同步期间发生变化，原文件已保留")
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	if err = os.Rename(stage, target); err != nil {
		return false, err
	}
	manifest.Media, manifest.PendingMedia = wanted, nil
	return true, nil
}

func validateEmbySTRMTransition(folder string, manifest embyFolderManifest, key []byte) error {
	chapters := manifest.Chapters
	merged := manifest.Mode == "merged" || manifest.Mode == embyLocalMergedMode ||
		manifest.Mode == "retired" && (manifest.RetiredMode == "merged" || manifest.RetiredMode == embyLocalMergedMode)
	if merged {
		chapters = []embyChapter{{ID: "merged", Number: 1}}
	}
	for _, chapter := range chapters {
		if !validEmbyIdentity(manifest.DramaID, chapter.ID) || chapter.Number < 1 || chapter.Number > 2000 {
			return errors.New("Emby 分集清单无效，原文件已保留")
		}
		path := filepath.Join(folder, "Season 01", fmt.Sprintf("S01E%03d.strm", chapter.Number))
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode().IsRegular() || info.Size() > 4096 {
			return errors.New("Emby 播放入口已改变，原文件已保留")
		}
		body, err := os.ReadFile(path)
		if err != nil || !isManagedEmbyLink(strings.TrimSpace(string(body)), manifest.DramaID, chapter.ID, key, merged) {
			return errors.New("Emby 播放入口不是本程序生成，原文件已保留")
		}
	}
	return nil
}
