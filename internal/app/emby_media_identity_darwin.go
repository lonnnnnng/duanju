package app

import (
	"fmt"
	"os"
	"syscall"
)

func embyMediaIdentity(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	// long: 文件身份包含变更时间，避免仅凭大小和 mtime 复用已经被改写的全集。
	return fmt.Sprintf("darwin:%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctimespec.Sec, stat.Ctimespec.Nsec)
}
