//go:build windows

package main

import (
	"flag"
	"os"
	"path/filepath"

	"github.com/mageg-x/dedupfs/common/log"
)

var (
	logger = log.GetLogger("dedupfs")
)

func main() {
	// 解析命令行参数
	flag.Parse()

	// 获取程序安装目录，初始化日志
	appDir, err := os.Executable()
	if err != nil {
		appDir, _ = os.Getwd()
	} else {
		appDir = filepath.Dir(appDir)
	}

	// 初始化日志系统
	log.Init(&log.Config{
		LogDir:     appDir + "/logs",
		MaxSize:    10,
		MaxBackups: 7,
		MaxAge:     30,
		Compress:   true,
	})

	// 判断运行模式
	// 如果指定了-cmd参数或者有其他命令行参数，运行命令行模式
	if len(os.Args) > 1 {
		logger.Info("Running in command line mode")
		runCmd()
	} else {
		// 否则运行GUI模式
		logger.Info("Running in GUI mode")
		runGui()
	}
}
