//go:build windows

package main

import (
	"flag"
)

var (
	// 命令行参数
	mountCmd = flag.String("mount", "", "Mount a filesystem with specified arguments")
)

// runCmd 启动命令行模式
func runCmd() {
	logger.Info("Running in command line mode")

}
