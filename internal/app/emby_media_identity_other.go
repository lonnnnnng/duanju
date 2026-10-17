//go:build !linux && !darwin

package app

import "os"

func embyMediaIdentity(info os.FileInfo) string {
	// long: 无法取得可靠变更时间的平台继续完整校验，不用较弱的时间戳缓存放宽用户文件保护。
	return ""
}
