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
	// long: ctime 随原地写入改变，可识别同 inode、同长度且被还原 mtime 的修改。
	return fmt.Sprintf("linux:%d:%d:%d:%d", stat.Dev, stat.Ino, stat.Ctim.Sec, stat.Ctim.Nsec)
}
