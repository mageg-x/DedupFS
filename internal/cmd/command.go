package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/mageg-x/dedupfs/internal/log"
	"github.com/mageg-x/dedupfs/internal/mount"
	"github.com/sirupsen/logrus"
)

var (
	// 获取logger实例用于输出日志
	logger = log.GetLogger("dedupfs")
)

// ExecuteCommand processes the given command and executes the corresponding functionality
func ExecuteCommand() {
	// 解析全局日志级别参数
	var verbose int
	for i := 1; i < len(os.Args); i++ {
		if len(os.Args[i]) > 1 && os.Args[i][0] == '-' {
			allV := true
			for j := 1; j < len(os.Args[i]); j++ {
				if os.Args[i][j] != 'v' {
					allV = false
					break
				}
			}
			if allV {
				verbose += len(os.Args[i]) - 1
			}
		}
	}

	// 根据-v参数数量设置日志级别
	// 计算日志级别，确保不会超出最低级别（TraceLevel）
	level := int(logrus.ErrorLevel) + verbose
	if level > int(logrus.TraceLevel) {
		level = int(logrus.TraceLevel)
	}
	logger.SetLevel(logrus.Level(level))

	// 过滤掉日志级别参数，获取实际命令
	filteredArgs := []string{os.Args[0]}
	for i := 1; i < len(os.Args); i++ {
		// 检查是否是纯v参数（如-v, -vv, -vvv等）
		isVParam := len(os.Args[i]) > 1 && os.Args[i][0] == '-'
		if isVParam {
			for j := 1; j < len(os.Args[i]); j++ {
				if os.Args[i][j] != 'v' {
					isVParam = false
					break
				}
			}
		}
		if !isVParam {
			filteredArgs = append(filteredArgs, os.Args[i])
		}
	}
	os.Args = filteredArgs

	if len(os.Args) < 2 {
		ShowUsage()
		return
	}

	command := os.Args[1]

	switch command {
	case "mount":
		logger.Info("Executing mount command")
		MountCommand()
	case "unmount":
		logger.Info("Executing unmount command")
		UnmountCommand()
	case "stats":
		logger.Info("Executing stats command")
		StatsCommand()
	default:
		logger.Errorf("unknown command: %s", command)
		fmt.Printf("unknown command: %s\n", command)
		ShowUsage()
	}
}

// MountCommand handles the mount command logic
func MountCommand() {
	flagSet := flag.NewFlagSet("mount", flag.ExitOnError)
	flagSet.Parse(os.Args[2:])

	args := flagSet.Args()
	if len(args) < 2 {
		fmt.Printf("usage: dedupfs mount <mountpoint> <datadir>\n")
		return
	}

	mountPoint := args[0]
	dataDir := args[1]

	logger.Infof("mounting %s to %s", dataDir, mountPoint)

	if err := mount.Mount(mountPoint, dataDir); err != nil {
		logger.Errorf("mount failed: %v", err)
		fmt.Printf("mount failed: %v\n", err)
		os.Exit(1)
	}
	logger.Info("Mount successful")
	fmt.Printf("mount successful\n")
}

// UnmountCommand handles the unmount command logic
func UnmountCommand() {
	flagSet := flag.NewFlagSet("unmount", flag.ExitOnError)
	flagSet.Parse(os.Args[2:])

	args := flagSet.Args()
	if len(args) < 1 {
		fmt.Printf("usage: dedupfs unmount <mountpoint>\n")
		return
	}

	mountPoint := args[0]
	logger.Infof("unmounting %s", mountPoint)

	if err := mount.Unmount(mountPoint); err != nil {
		logger.Errorf("unmount failed: %v", err)
		fmt.Printf("unmount failed: %v\n", err)
		os.Exit(1)
	}
	logger.Info("Unmount successful")
	fmt.Printf("unmount successful\n")
}

// StatsCommand handles the stats command logic
func StatsCommand() {
	flagSet := flag.NewFlagSet("stats", flag.ExitOnError)
	flagSet.Parse(os.Args[2:])

	args := flagSet.Args()
	if len(args) < 1 {
		fmt.Printf("usage: dedupfs stats <mountpoint>\n")
		return
	}

	mountPoint := args[0]
	logger.Infof("showing statistics for %s", mountPoint)

	fmt.Printf("showing statistics for %s\n", mountPoint)
	// Implementation of actual statistics collection logic will go here
	fmt.Printf("file count: 0\n")
	fmt.Printf("deduplication ratio: 0%%\n")
	fmt.Printf("space used: 0\n")
	logger.Debug("Stats command completed")
}

// ShowUsage displays the usage information for the dedupfs command
func ShowUsage() {
	fmt.Printf("usage: dedupfs <command> [arguments]\n")
	fmt.Printf("commands:\n")
	fmt.Printf("  mount    mount a dedupfs filesystem\n")
	fmt.Printf("  unmount  unmount a dedupfs filesystem\n")
	fmt.Printf("  stats    show filesystem statistics\n")
	fmt.Printf("\n")
	fmt.Printf("examples:\n")
	fmt.Printf("  dedupfs mount /data/dfs data/\n")
	fmt.Printf("  dedupfs unmount /data/dfs\n")
	fmt.Printf("  dedupfs stats /data/dfs\n")
}
