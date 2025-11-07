//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"

	ipccmd "github.com/mageg-x/dedupfs/common/cmd"
	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/ipc"
	"github.com/mageg-x/dedupfs/common/log"
	"github.com/mageg-x/dedupfs/common/mount"
	"github.com/mageg-x/dedupfs/common/utils"
)

var (
	logger = log.GetLogger("dedupfs")
)

var (
	// Verbose flag
	verbose int

	// Mount command flags
	fixedSize bool
	minSize   int64
	avgSize   int64
	maxSize   int64
	blockSize int64
	compress  bool
	encrypt   bool
	password  string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dedupfs",
	Short: "Deduplicated filesystem",
	Long:  `A deduplicated filesystem for efficient storage management`,
	Args:  cobra.MinimumNArgs(1),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// 设置日志级别
		setLogLevel()
	},
	// 禁用自动生成的 completion 命令
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

// mountCmd represents the mount command
var mountCmd = &cobra.Command{
	Use:     "mount [mountPoint] [dataDir]",
	Short:   "Mount a dedupfs filesystem",
	Aliases: []string{"m"},
	Args:    cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		mountAction(cmd, args)
		return nil
	},
	Example: `  dedupfs mount /data/dfs data/ --min-size=1048576 --compress=false
  dedupfs mount /data/dfs data/ --fixed-size --block-size=134217728`,
}

var statsCmd = &cobra.Command{
	Use:   "stats [mountPoint]",
	Short: "Show a mounted filesystems statistics",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		statsAction(cmd, args)
		return nil
	},
	Example: `  dedupfs stats /data/dfs`,
}

// unmountCmd represents the unmount command
var unmountCmd = &cobra.Command{
	Use:     "unmount [mountPoint]",
	Short:   "Unmount a dedupfs filesystem",
	Aliases: []string{"umount"},
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		unmountAction(cmd, args)
		return nil
	},
	Example: `  dedupfs unmount /data/dfs`,
}

func initRoot() {
	// 添加全局 verbose 标志
	rootCmd.PersistentFlags().IntVarP(&verbose, "verbose", "v", 0, "Verbose level (0-4)")

	// 初始化各个命令
	initMount()

	// 添加命令到 root
	rootCmd.AddCommand(mountCmd)
	rootCmd.AddCommand(unmountCmd)
	rootCmd.AddCommand(statsCmd)
}

// initMount initializes mount command flags
func initMount() {
	// Mount command flags
	mountCmd.Flags().BoolVar(&fixedSize, "fixed-size", false, "Use fixed size chunks")
	mountCmd.Flags().Int64Var(&minSize, "min-size", 1024*1024, "Minimum chunk size in bytes")
	mountCmd.Flags().Int64Var(&avgSize, "avg-size", 2*1024*1024, "Average chunk size in bytes")
	mountCmd.Flags().Int64Var(&maxSize, "max-size", 4*1024*1024, "Maximum chunk size in bytes")
	mountCmd.Flags().Int64Var(&blockSize, "block-size", 64*1024*1024, "Block size in bytes")
	mountCmd.Flags().BoolVar(&compress, "compress", true, "Enable compression")
	mountCmd.Flags().BoolVar(&encrypt, "encrypt", false, "Enable encryption")
	mountCmd.Flags().StringVar(&password, "password", "", "Password for encryption")
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
	// fmt.Println("Log level:", level.String())
	logger.SetLevel(level)

	logger.Debugf("verbose level: %d, log level: %s", verbose, level.String())
}

func mountAction(cmd *cobra.Command, args []string) error {
	mountPoint := strings.ToUpper(args[0])
	dataDir := args[1]

	logger.Infof("sending mount request: %s to %s", dataDir, mountPoint)
	// 调试信息
	logger.Debugf("arguments: mountPoint=%s, dataDir=%s", mountPoint, dataDir)
	logger.Debugf("flags: fixed-size=%v, min-size=%d, avg-size=%d, max-size=%d, block-size=%d, compress=%v, encrypt=%v, password_set=%v",
		fixedSize, minSize, avgSize, maxSize, blockSize, compress, encrypt, password != "")

	// 创建可取消的 context
	ctx, cancel := context.WithCancel(context.Background())

	// 启动 IPC 服务器，传入 ctx
	startIpcServer(ctx, mountPoint)

	// 确保在函数退出时停止 IPC 服务器
	defer func() {
		logger.Errorf("stopping ipc server on exit")
		cancel()
	}()

	// 注册信号处理，捕获程序终止信号
	setupSignalHandler(cancel)

	mount.Mount(mountPoint, dataDir, &mount.MountOptions{
		BlockConf: &mount.BlockConfig{
			Size:     blockSize,
			Compress: compress,
			Encrypt:  encrypt,
			Password: password,
		},
		ChunkConf: &mount.ChunkConfig{
			FixedSize: fixedSize,
			MinSize:   minSize,
			AvgSize:   avgSize,
			MaxSize:   maxSize,
		},
	})
	logger.Errorf("exit mount %s to %s", mountPoint, dataDir)
	return nil
}

// unmountAction is the cobra action for unmount command
func unmountAction(cmd *cobra.Command, args []string) error {
	mountPoint := strings.ToUpper(args[0])
	logger.Infof("sending unmount request for %s", mountPoint)

	if err := ipccmd.InvokeUnmount(mountPoint); err != nil {
		if errr := mount.ForceUnmount(mountPoint); errr != nil {
			logger.Errorf("force unmount failed: %v", errr)
		} else {
			logger.Infof("force unmount successful")
			return err
		}
		logger.Errorf("unmount failed: %v", err)
		return err
	}
	logger.Info("unmount successful")
	return nil
}

func statsAction(cmd *cobra.Command, args []string) error {
	logger.Info("collecting statistics for all mounted filesystems")
	fsStats, err := ipccmd.InvokeStats(args[0])
	if err != nil || fsStats == nil {
		logger.Errorf("failed to collect statistics: %v", err)
		return err
	}

	// 固定终端宽度，Windows下不支持动态获取
	contentWidth := 116 // 120 - 4

	// Header with padding
	headerText := "Deduplication Filesystem Statistics"
	border := "┌" + strings.Repeat("─", contentWidth) + "┐"
	fmt.Printf(" %s\n", border)
	fmt.Printf(" │%*s%*s│\n",
		(contentWidth+len(headerText))/2,
		headerText,
		(contentWidth+len(headerText)+1)/2-len(headerText),
		"",
	)
	fmt.Printf(" %s\n", "└"+strings.Repeat("─", contentWidth)+"┘")

	keyWidth := contentWidth / 3
	if keyWidth > 30 {
		keyWidth = 30
	}
	if keyWidth < 20 {
		keyWidth = 20
	}

	// 格式化字节数的内部函数
	formatBytes := func(bytes float64) string {
		units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
		size := bytes
		unitIndex := 0
		for size >= 1024 && unitIndex < len(units)-1 {
			size /= 1024
			unitIndex++
		}
		return fmt.Sprintf("%.2f %s", size, units[unitIndex])
	}

	// 处理ChunkConfig和BlockConfig的格式化
	formatChunkConfig := func(config *dfs.ChunkConfig) string {
		if config == nil {
			return "N/A"
		}
		if config.FixedSize {
			return fmt.Sprintf("Fixed: %s", formatBytes(float64(config.AvgSize)))
		}
		return fmt.Sprintf("min: %s, avg: %s, max: %s",
			formatBytes(float64(config.MinSize)),
			formatBytes(float64(config.AvgSize)),
			formatBytes(float64(config.MaxSize)))
	}

	formatBlockConfig := func(config *dfs.BlockConfig) string {
		if config == nil {
			return "N/A"
		}
		compress := "no"
		if config.Compress {
			compress = "yes"
		}
		encrypt := "no"
		passwordSet := ""
		if config.Encrypt {
			encrypt = "yes"
			if config.Password != "" {
				passwordSet = "yes (masked)"
			}
		}
		return fmt.Sprintf("Size: %s, Compress: %s, Encrypt: %s, Password: %s",
			formatBytes(float64(config.Size)), compress, encrypt, passwordSet)
	}

	// 输出挂载点信息
	fmt.Printf("\n  \x1b[1;96mMount Point: %s\x1b[0m\n", args[0])
	fmt.Printf("  %s\n", strings.Repeat("─", contentWidth))

	// 定义字段列表
	fields := []struct {
		Name  string
		Value string
	}{
		{"Filesystem ID", fsStats.ID},
		{"Base Directory", fsStats.BaseDir},
		{"Metadata Directory", fsStats.MetaDir},
		{"Data Directory", fsStats.DataDir},
		{"Chunk Config", formatChunkConfig(fsStats.ChunkConfig)},
		{"Block Config", formatBlockConfig(fsStats.BlockConfig)},
		{"Files", fmt.Sprintf("%d", fsStats.FileCount)},
		{"Directories", fmt.Sprintf("%d", fsStats.DirCount)},
		{"Space Used", formatBytes(float64(fsStats.SpaceUsed))},
		{"Real Size", formatBytes(float64(fsStats.RealSize))},
		{"Total Chunks", fmt.Sprintf("%d", fsStats.ChunkCount)},
		{"Blocks", fmt.Sprintf("%d", fsStats.BlockCount)},
		{"Referenced Chunks", fmt.Sprintf("%d", fsStats.RefChunkCount)},
	}

	// 计算去重率
	deduplicationRatio := "0.00 X"
	if fsStats.RealSize > 0 {
		ratio := float64(fsStats.SpaceUsed) / float64(fsStats.RealSize)
		deduplicationRatio = fmt.Sprintf("%.2f X", ratio)
	}
	fields = append(fields,
		struct {
			Name  string
			Value string
		}{"Compression Ratio", deduplicationRatio},
	)

	// 添加最后更新时间
	lastUpdated := "unknown"
	if !fsStats.LastUpdated.IsZero() {
		lastUpdated = fsStats.LastUpdated.Format("2006-01-02 15:04:05")
	}
	fields = append(fields,
		struct {
			Name  string
			Value string
		}{"Last Updated", lastUpdated},
	)

	// 输出所有字段
	for _, f := range fields {
		fmt.Printf("  %-*s : %s\n", keyWidth, f.Name, f.Value)
	}

	logger.Debug("stats command completed")
	return nil
}

// startIpcServer 启动 IPC 服务器，接收 ctx 作为参数
func startIpcServer(ctx context.Context, mountPoint string) *ipc.Server {
	go func() {
		// 根据 mountPoint 生成 IPC pipe 名称
		pipeName := fmt.Sprintf("\\\\.\\pipe\\dedupfs_%s", utils.CalcHash([]byte(mountPoint)))
		// 创建 IPC 服务器
		server := ipc.NewServer(pipeName)

		server.Register("unmount", ipccmd.HandleUnmountCommand)
		server.Register("stats", ipccmd.HandleStatsCommand)
		server.Register("stat", ipccmd.HandleStatCommand)

		logger.Errorf("starting ipc server")
		server.Start(ctx)
		logger.Errorf("ipc server stopped")
	}()

	return nil
}

// setupSignalHandler 设置信号处理器，在程序终止时清理资源
func setupSignalHandler(cancel context.CancelFunc) {
	// 捕获系统信号
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		logger.Errorf("received shutdown signal, cleaning up...")
		cancel()
		// 在Windows环境下，使用os.Exit终止进程
		os.Exit(0)
	}()
}

// Cmd 启动命令行模式
func runCmd() {
	logger.Errorf("running in command line mode")

	initRoot()

	// 设置处理后的参数
	rootCmd.SetArgs(os.Args[1:]) // 去掉程序名

	// 执行命令，不再重复打印错误
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func main() {
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
	runCmd()
}
