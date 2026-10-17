//go:build unix

package app

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestEmbyLocalMergedCopiesSourceWithDifferentGroup(t *testing.T) {
	settings, drama, merge, key := localEmbyFixture(t, 0640)
	destination, err := os.Stat(settings.OutputDir)
	if err != nil {
		t.Fatal(err)
	}
	destinationGroup := int(destination.Sys().(*syscall.Stat_t).Gid)
	groups, err := os.Getgroups()
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	for _, group := range groups {
		if group != destinationGroup && os.Chown(merge.OutputPath, -1, group) == nil {
			changed = true
			break
		}
	}
	if !changed {
		t.Skip("当前用户无法创建不同属组的测试源文件")
	}
	before, err := os.Stat(merge.OutputPath)
	if err != nil {
		t.Fatal(err)
	}
	folder, _, _, err := syncEmbyMergedFiles(context.Background(), settings, drama, merge, "", key, "owner")
	if err != nil {
		t.Fatal(err)
	}
	media, err := os.Stat(filepath.Join(settings.OutputDir, folder, filepath.FromSlash(embyLocalMergedName)))
	if err != nil || os.SameFile(before, media) || int(media.Sys().(*syscall.Stat_t).Gid) != destinationGroup || media.Mode().Perm()&0040 == 0 {
		t.Fatal("入库副本没有继承可读的目标共享组", err)
	}
	after, err := os.Stat(merge.OutputPath)
	if err != nil || before.Sys().(*syscall.Stat_t).Gid != after.Sys().(*syscall.Stat_t).Gid || before.Mode() != after.Mode() {
		t.Fatal("发布副本改变了源视频属组或权限", err)
	}
}
