package main

import (
	"github.com/mageg-x/dedupfs/common/log"
	"github.com/mageg-x/dedupfs/platform/dedupfs-linux/cmd"
)

func main() {
	// 使用内部日志包初始化，可以传入nil使用默认配置
	log.Init(nil)

	// 执行主命令
	cmd.ExecuteCommand()
}
