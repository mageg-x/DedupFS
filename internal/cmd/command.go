package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	"github.com/mageg-x/dedupfs/internal/log"
	"github.com/mageg-x/dedupfs/internal/mount"
)

var (
	// 获取logger实例用于输出日志
	logger = log.GetLogger("dedupfs")

	// Mount command flags
	fixedSize bool
	minSize   int64
	avgSize   int64
	maxSize   int64
	blockSize int64
	compress  bool
	encrypt   bool
	password  string

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
}

// mountCmd represents the mount command
var mountCmd = &cobra.Command{
	Use:     "mount [mountPoint] [dataDir]",
	Short:   "Mount a dedupfs filesystem",
	Aliases: []string{"m"},
	Args:    cobra.ExactArgs(2),
	RunE:    mountAction,
	Example: `  dedupfs mount /data/dfs data/ --min-size=1048576 --compress=false
  dedupfs mount /data/dfs data/ --fixed-size --block-size=134217728`,
}

// unmountCmd represents the unmount command
var unmountCmd = &cobra.Command{
	Use:     "unmount [mountPoint]",
	Short:   "Unmount a dedupfs filesystem",
	Aliases: []string{"umount"},
	Args:    cobra.ExactArgs(1),
	RunE:    unmountAction,
	Example: `  dedupfs unmount /data/dfs
  dedupfs umount /data/dfs`,
}

// statsCmd represents the stats command
var statsCmd = &cobra.Command{
	Use:     "stats [mountPoint]",
	Short:   "Show filesystem statistics",
	Args:    cobra.ExactArgs(1),
	RunE:    statsAction,
	Example: `  dedupfs stats /data/dfs`,
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
	// Add commands to root
	rootCmd.AddCommand(mountCmd)
	rootCmd.AddCommand(unmountCmd)
	rootCmd.AddCommand(statsCmd)

	// Mount command flags
	mountCmd.Flags().BoolVar(&fixedSize, "fixed-size", false, "Use fixed size chunks")
	mountCmd.Flags().Int64Var(&minSize, "min-size", 4096, "Minimum chunk size in bytes")
	mountCmd.Flags().Int64Var(&avgSize, "avg-size", 8*1024, "Average chunk size in bytes")
	mountCmd.Flags().Int64Var(&maxSize, "max-size", 16*1024, "Maximum chunk size in bytes")
	mountCmd.Flags().Int64Var(&blockSize, "block-size", 64*1024*1024, "Block size in bytes")
	mountCmd.Flags().BoolVar(&compress, "compress", true, "Enable compression")
	mountCmd.Flags().BoolVar(&encrypt, "encrypt", false, "Enable encryption")
	mountCmd.Flags().StringVar(&password, "password", "", "Password for encryption")

	// 添加全局的 verbose 标志（用于帮助信息）
	rootCmd.PersistentFlags().CountVarP(&verbose, "verbose", "v", "Increase verbosity level (use -v for warn, -vv for info, -vvv for debug, -vvvv for trace)")
}

// mountAction is the cobra action for mount command
func mountAction(cmd *cobra.Command, args []string) error {
	mountPoint := args[0]
	dataDir := args[1]

	logger.Infof("mounting %s to %s", dataDir, mountPoint)

	// 调试信息
	logger.Debugf("Arguments: mountPoint=%s, dataDir=%s", mountPoint, dataDir)
	logger.Debugf("Flags: fixed-size=%v, min-size=%d, avg-size=%d, max-size=%d, block-size=%d, compress=%v, encrypt=%v, password_set=%v",
		fixedSize, minSize, avgSize, maxSize, blockSize, compress, encrypt, password != "")

	opts := &mount.MountOptions{
		ChunkConf: &mount.ChunkConfig{
			FixedSize: fixedSize,
			MinSize:   minSize,
			AvgSize:   avgSize,
			MaxSize:   maxSize,
		},
		BlockConf: &mount.BlockConfig{
			Size:     blockSize,
			Compress: compress,
			Encrypt:  encrypt,
			Password: password,
		},
	}

	logger.Infof("Chunk config: fixed=%v, min=%d, avg=%d, max=%d",
		opts.ChunkConf.FixedSize, opts.ChunkConf.MinSize, opts.ChunkConf.AvgSize, opts.ChunkConf.MaxSize)
	logger.Infof("Block config: size=%d, compress=%v, encrypt=%v",
		opts.BlockConf.Size, opts.BlockConf.Compress, opts.BlockConf.Encrypt)

	if err := mount.Mount(mountPoint, dataDir, opts); err != nil {
		logger.Errorf("mount failed: %v", err)
		return err
	}

	logger.Info("Mount successful")
	fmt.Printf("mount successful\n")
	return nil
}

// unmountAction is the cobra action for unmount command
func unmountAction(cmd *cobra.Command, args []string) error {
	mountPoint := args[0]
	logger.Infof("unmounting %s", mountPoint)

	if err := mount.Unmount(mountPoint); err != nil {
		logger.Errorf("unmount failed: %v", err)
		return err
	}

	logger.Info("Unmount successful")
	fmt.Printf("unmount successful\n")
	return nil
}

// statsAction is the cobra action for stats command
func statsAction(cmd *cobra.Command, args []string) error {
	mountPoint := args[0]
	logger.Infof("showing statistics for %s", mountPoint)

	fmt.Printf("showing statistics for %s\n", mountPoint)
	fmt.Printf("file count: 0\n")
	fmt.Printf("deduplication ratio: 0%%\n")
	fmt.Printf("space used: 0\n")
	logger.Debug("Stats command completed")
	return nil
}
