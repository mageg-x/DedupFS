package main

import (
	"github.com/mageg-x/dedupfs/cmd"
	"github.com/mageg-x/dedupfs/internal/log"
)

func main() {
	// 使用内部日志包初始化，可以传入nil使用默认配置
	log.Init(nil)

	// 执行主命令
	cmd.ExecuteCommand()
}
