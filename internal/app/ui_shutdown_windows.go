//go:build windows

package app

import (
	"context"
	"os"
	"os/signal"
)

// long: Windows 使用 Ctrl-C 中断信号，保持与 Unix 启动路径相同的可取消服务接口。
func newUIShutdownContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt)
}
