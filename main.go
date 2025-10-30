package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/mageg-x/dedupfs/cmd"
	"github.com/mageg-x/dedupfs/internal/log"
	"github.com/mageg-x/dedupfs/internal/mount"
)

// setupCleanup 设置程序退出时的清理工作
func cleanup() {
	// 设置信号处理，捕获中断信号
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// 在单独的goroutine中处理信号
	go func() {
		sig := <-sigChan
		logger := log.GetLogger("dedupfs")
		logger.Infof("received signal: %s, initiating cleanup", sig)
		// 显式调用清理函数
		logger.Info("cleaning up mounted directories")
		mount.CleanupMounts()
		logger.Info("dedupfs program exited")
		// 退出程序
		os.Exit(1)
	}()
}

func main() {
	// 使用内部日志包初始化，可以传入nil使用默认配置
	log.Init(nil)

	// 设置清理和信号处理
	cleanup()

	// 执行主命令
	cmd.ExecuteCommand()
}
