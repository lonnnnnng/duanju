package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func warmEmbyMediaCache(t *testing.T) (string, string, string, embyFolderManifest) {
	t.Helper()
	settings, drama, merge, key := localEmbyFixture(t, 0600)
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, folder, key, "owner"); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(settings.OutputDir, folder)
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	var manifest embyFolderManifest
	if err := readEmbyJSON(filepath.Join(root, ".duanju-emby.json"), &manifest, 8<<20); err != nil {
		t.Fatal(err)
	}
	return root, merge.OutputPath, filepath.Join(root, filepath.FromSlash(embyLocalMergedName)), manifest
}

func BenchmarkEmbyMediaPeriodicSync(b *testing.B) {
	for _, cached := range []bool{false, true} {
		b.Run(map[bool]string{false: "full-32MiB-copy", true: "cached-32MiB-copy"}[cached], func(b *testing.B) {
			root := b.TempDir()
			source := filepath.Join(root, "source.mp4")
			folder := filepath.Join(root, "library")
			if err := os.MkdirAll(filepath.Join(folder, "Season 01"), 0755); err != nil {
				b.Fatal(err)
			}
			if err := os.WriteFile(source, bytes.Repeat([]byte{7}, 32<<20), 0600); err != nil {
				b.Fatal(err)
			}
			manifest := embyFolderManifest{Version: 1, DramaID: historyFixtureDramaID, Mode: embyLocalMergedMode}
			for warmup := 0; warmup < 2; warmup++ {
				if _, err := syncEmbyLocalMedia(context.Background(), folder, source, &manifest); err != nil {
					b.Fatal(err)
				}
			}
			info, _ := os.Lstat(source)
			if cached && embyMediaIdentity(info) == "" {
				b.Skip("无可靠变更时间的平台不启用缓存")
			}
			b.ResetTimer()
			for index := 0; index < b.N; index++ {
				if !cached {
					manifest.SourceCache = nil
				}
				if _, err := syncEmbyLocalMedia(context.Background(), folder, source, &manifest); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestEmbyMediaCachePersistenceExpiryAndPending(t *testing.T) {
	root, source, target, manifest := warmEmbyMediaCache(t)
	info, _ := os.Lstat(source)
	if embyMediaIdentity(info) == "" {
		if manifest.SourceCache != nil || manifest.TargetCache != nil {
			t.Fatal("无可靠变更时间的平台仍写入缓存")
		}
		return
	}
	now := time.Now()
	if !reuseEmbyMediaCache(context.Background(), source, target, &manifest, now) {
		t.Fatal("已持久化的完整校验不能被后续同步复用")
	}
	checked := manifest.SourceCache.CheckedAt
	if changed, err := syncEmbyLocalMedia(context.Background(), root, source, &manifest); changed || err != nil || !manifest.SourceCache.CheckedAt.Equal(checked) {
		t.Fatal("缓存命中改写媒体或顺延完整巡检", changed, err)
	}
	for _, checkTime := range []time.Time{checked.Add(embyMediaFullCheckInterval), checked.Add(-time.Second)} {
		if reuseEmbyMediaCache(context.Background(), source, target, &manifest, checkTime) {
			t.Fatal("过期或未来时间缓存被复用")
		}
	}
	manifest.PendingMedia = manifest.Media
	if reuseEmbyMediaCache(context.Background(), source, target, &manifest, now) {
		t.Fatal("待发布事务跳过完整校验")
	}
	manifest.PendingMedia = nil
	manifest.SourceCache.CheckedAt = now.Add(-25 * time.Hour)
	if changed, err := syncEmbyLocalMedia(context.Background(), root, source, &manifest); changed || err != nil || !manifest.SourceCache.CheckedAt.After(checked) {
		t.Fatal("巡检到期未重新校验", changed, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if reuseEmbyMediaCache(ctx, source, target, &manifest, now) {
		t.Fatal("已取消的同步复用了缓存")
	}
}

func TestEmbyMediaCacheDetectsRestoredMtimeAndReplacement(t *testing.T) {
	for _, replace := range []bool{false, true} {
		t.Run(map[bool]string{false: "in-place", true: "new-inode"}[replace], func(t *testing.T) {
			root, source, target, manifest := warmEmbyMediaCache(t)
			info, _ := os.Stat(target)
			body, _ := os.ReadFile(target)
			body[0] ^= 1
			path := target
			if replace {
				path += ".replacement"
			}
			if err := os.WriteFile(path, body, 0640); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if replace {
				if err := os.Rename(path, target); err != nil {
					t.Fatal(err)
				}
			}
			if reuseEmbyMediaCache(context.Background(), source, target, &manifest, time.Now()) {
				t.Fatal("同大小同 mtime 的用户修改被缓存掩盖")
			}
			if _, err := syncEmbyLocalMedia(context.Background(), root, source, &manifest); err == nil {
				t.Fatal("改写后的入库文件被覆盖")
			}
			if _, _, err := checkEmbyLocalMedia(context.Background(), root, manifest); err == nil {
				t.Fatal("退休删除校验复用了缓存")
			}
		})
	}
}

func TestEmbyMediaPublishRechecksTargetAfterCopyStarts(t *testing.T) {
	root, source, target, manifest := warmEmbyMediaCache(t)
	if err := os.WriteFile(source, []byte("new longer complete video"), 0600); err != nil {
		t.Fatal(err)
	}
	original, _ := os.Stat(target)
	body, _ := os.ReadFile(target)
	body[0] ^= 1
	guard := func(string, uint64) error {
		if err := os.WriteFile(target, body, 0640); err != nil {
			return err
		}
		return os.Chtimes(target, original.ModTime(), original.ModTime())
	}
	if _, err := syncEmbyLocalMedia(context.Background(), root, source, &manifest, guard); err == nil {
		t.Fatal("复制期间用户改写的目标被覆盖")
	}
	current, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(current, body) {
		t.Fatal("用户改写没有被保留", err)
	}
}

type retireMutationContext struct {
	context.Context
	reads  int
	mutate func()
}

func (ctx *retireMutationContext) Err() error {
	ctx.reads++
	// long: 小文件的两次读取完成完整哈希，第三次取消检查位于退休删除前；在两阶段之间模拟用户改写。
	if ctx.reads == 3 {
		ctx.mutate()
	}
	return nil
}

func TestEmbyMediaRetirementRechecksValidatedFile(t *testing.T) {
	root, _, target, manifest := warmEmbyMediaCache(t)
	before, _ := os.Stat(target)
	body, _ := os.ReadFile(target)
	body[0] ^= 1
	ctx := &retireMutationContext{Context: context.Background(), mutate: func() {
		if err := os.WriteFile(target, body, 0640); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(target, before.ModTime(), before.ModTime()); err != nil {
			t.Fatal(err)
		}
	}}
	if _, err := retireEmbySTRMs(ctx, root, manifest, make([]byte, 32)); err == nil {
		t.Fatal("完整校验后被改写的用户媒体仍被退休删除")
	}
	current, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(current, body) {
		t.Fatal("退休没有保留并发修改", err)
	}
}
