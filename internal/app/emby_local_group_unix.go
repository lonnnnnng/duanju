//go:build unix

package app

import (
	"os"
	"syscall"
)

func sameEmbyLocalGroup(source, destination os.FileInfo) bool {
	first, firstOK := source.Sys().(*syscall.Stat_t)
	second, secondOK := destination.Sys().(*syscall.Stat_t)
	return firstOK && secondOK && first.Gid == second.Gid
}
