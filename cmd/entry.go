package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/mageg-x/dedupfs/internal/log"
)

var (
	// 获取logger实例用于输出日志
	logger = log.GetLogger("dedupfs")

	// Verbose flag
	verbose int
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dedupfs",
	Short: "Deduplicated filesystem",
	Long:  `A deduplicated filesystem for efficient storage management`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		setLogLevel()
	},
	// 禁用自动生成的 completion 命令
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

// ExecuteCommand processes the given command and executes the corresponding functionality
func ExecuteCommand() {
	// 预处理参数，提取 verbose 级别
	processedArgs, verboseCount := preprocessArgs(os.Args)
	verbose = verboseCount

	// 设置处理后的参数
	rootCmd.SetArgs(processedArgs[1:]) // 去掉程序名

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// preprocessArgs 预处理命令行参数，提取 verbose 级别并移除 -v 参数
func preprocessArgs(args []string) ([]string, int) {
	processedArgs := []string{args[0]} // 保留程序名
	verboseCount := 0

	for i := 1; i < len(args); i++ {
		arg := args[i]

		// 检查是否是 -v 参数（短选项）
		if len(arg) > 1 && arg[0] == '-' && arg[1] != '-' {
			// 检查是否所有字符都是 'v'
			isVOnly := true
			for j := 1; j < len(arg); j++ {
				if arg[j] != 'v' {
					isVOnly = false
					break
				}
			}

			if isVOnly {
				verboseCount += len(arg) - 1 // 计算v的数量
				continue                     // 跳过这个参数，不添加到processedArgs
			}
		}

		// 检查长格式的 verbose 标志
		if strings.HasPrefix(arg, "--verbose") {
			// 处理 --verbose 或 --verbose=N 格式
			if arg == "--verbose" {
				verboseCount++
			} else if strings.HasPrefix(arg, "--verbose=") {
				// 解析 --verbose=N 格式
				// 这里简单处理，实际使用时可能需要更复杂的解析
				verboseCount++
			}
			continue
		}

		// 添加非-v参数到处理后的参数列表
		processedArgs = append(processedArgs, arg)
	}

	return processedArgs, verboseCount
}

// setLogLevel 根据 verbose 级别设置日志级别
func setLogLevel() {
	var level logrus.Level

	switch verbose {
	case 0:
		level = logrus.ErrorLevel // 默认错误级别
	case 1:
		level = logrus.WarnLevel // -v: 警告级别
	case 2:
		level = logrus.InfoLevel // -vv: 信息级别
	case 3:
		level = logrus.DebugLevel // -vvv: 调试级别
	default:
		level = logrus.TraceLevel // -vvvv 或更多: 跟踪级别
	}

	logger.SetLevel(level)
	logger.Debugf("Verbose level: %d, Log level: %s", verbose, level.String())
}

func init() {
	// 初始化各个命令
	initMount()
	initUnmount()
	initStats()
	initDebug()
	initServer()
	initRestore()

	// 添加命令到 root
	rootCmd.AddCommand(mountCmd)
	rootCmd.AddCommand(unmountCmd)
	rootCmd.AddCommand(statsCmd)
	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(serverCmd)
	rootCmd.AddCommand(restoreCmd)

	// 添加全局的 verbose 标志（用于帮助信息）
	rootCmd.PersistentFlags().CountVarP(&verbose, "verbose", "v", "Increase verbosity level (use -v for warn, -vv for info, -vvv for debug, -vvvv for trace)")
}
