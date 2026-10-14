//go:build !windows

package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// long: Unix 容器和服务器会发送 SIGTERM，和 Ctrl-C 一样交给 UI 服务做有序收尾。
func newUIShutdownContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}
