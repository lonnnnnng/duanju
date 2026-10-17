//go:build !unix

package app

import "os"

func sameEmbyLocalGroup(source, destination os.FileInfo) bool {
	// long: 无法用 POSIX 属组确认共享权限的平台通过复制继承目标目录的权限，避免复用源文件的访问边界。
	return false
}
