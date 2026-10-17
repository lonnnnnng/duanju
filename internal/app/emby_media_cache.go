package app

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

const embyMediaFullCheckInterval = 24 * time.Hour

func sameEmbyFileSnapshot(first, second os.FileInfo) bool {
	return first != nil && second != nil && first.Mode().IsRegular() && second.Mode().IsRegular() && os.SameFile(first, second) &&
		first.Size() == second.Size() && first.Mode() == second.Mode() && first.ModTime().Equal(second.ModTime()) && embyMediaIdentity(first) == embyMediaIdentity(second)
}

type embyMediaCache struct {
	Path      string         `json:"path"`
	Identity  string         `json:"identity"`
	Size      int64          `json:"size"`
	Modified  time.Time      `json:"modified"`
	Mode      os.FileMode    `json:"mode"`
	CheckedAt time.Time      `json:"checkedAt"`
	File      *embyLocalFile `json:"file"`
}

func (cache *embyMediaCache) matches(path string, info os.FileInfo, now time.Time) bool {
	if cache == nil || info == nil || !info.Mode().IsRegular() || info.Size() <= 0 || cache.File == nil ||
		!validEmbyLocalFile(cache.File) || cache.File.Name != embyLocalMergedName || cache.File.Size != info.Size() ||
		cache.CheckedAt.IsZero() || now.Before(cache.CheckedAt) || now.Sub(cache.CheckedAt) >= embyMediaFullCheckInterval {
		return false
	}
	identity := embyMediaIdentity(info)
	return identity != "" && cache.Path == filepath.Clean(path) && cache.Identity == identity &&
		cache.Size == info.Size() && cache.Mode == info.Mode() && cache.Modified.Equal(info.ModTime())
}

func newEmbyMediaCache(path string, file *embyLocalFile, info os.FileInfo, now time.Time) *embyMediaCache {
	if info == nil || file == nil || embyMediaIdentity(info) == "" {
		return nil
	}
	return &embyMediaCache{Path: filepath.Clean(path), Identity: embyMediaIdentity(info), Size: info.Size(),
		Modified: info.ModTime(), Mode: info.Mode(), CheckedAt: now, File: file}
}

func reuseEmbyMediaCache(ctx context.Context, source, target string, manifest *embyFolderManifest, now time.Time) bool {
	// long: 仅复用没有待发布事务且两端身份、修改时间和变更时间都未变的全集；每日完整校验时间不会被缓存命中顺延。
	if ctx.Err() != nil || manifest.PendingMedia != nil || manifest.Media == nil {
		return false
	}
	sourceInfo, sourceErr := os.Lstat(source)
	targetInfo, targetErr := os.Lstat(target)
	return sourceErr == nil && targetErr == nil && manifest.SourceCache.matches(source, sourceInfo, now) &&
		manifest.TargetCache.matches(target, targetInfo, now) && sameEmbyLocalFile(manifest.SourceCache.File, manifest.Media) &&
		sameEmbyLocalFile(manifest.TargetCache.File, manifest.Media) && ctx.Err() == nil
}
