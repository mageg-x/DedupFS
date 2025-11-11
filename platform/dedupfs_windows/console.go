//go:build windows && console

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	ipccmd "github.com/mageg-x/dedupfs/common/cmd"
	"github.com/mageg-x/dedupfs/common/console"
	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/ipc"
	"github.com/mageg-x/dedupfs/common/log"
	"github.com/mageg-x/dedupfs/common/mount"
	"github.com/mageg-x/dedupfs/common/utils"
)

var (
	logger = log.GetLogger("dedupfs")
	// Verbose flag
	verbose int
)

var (
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

var debugCmd = &cobra.Command{
	Use:   "debug [mountpoint] block|inode [block_id|inode_name]",
	Short: "Debug block or inode",
	Long:  `Debug block or inode in the dedupfs filesystem.`,
	Args:  cobra.MinimumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		mountPoint := strings.ToUpper(args[0])
		mountPoint = strings.TrimRight(mountPoint, "\\")
		debugType := args[1]
		id := args[2]

		switch debugType {
		case "block":
			debugBlockAction(mountPoint, id)
			return nil
		case "inode":
			debugINodeAction(mountPoint, id)
			return nil
		default:
			return fmt.Errorf("invalid debug type: %s, must be 'block' or 'inode'", debugType)
		}
	},
}

// restoreCmd represents the restore command
var restoreCmd = &cobra.Command{
	Use:   "restore [dataPath] [toPath]",
	Short: "Restore data from dedupfs to target path",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		restoreAction(cmd, args)
		return nil
	},
	Example: `  dedupfs restore data/ /restore/target`,
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
	rootCmd.AddCommand(debugCmd)
	rootCmd.AddCommand(restoreCmd)
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

// formatSize formats file size with commas for readability
func formatSize(n int64) string {
	if n < 0 {
		return "-" + formatSize(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result string
	for i := len(s); i > 0; i -= 3 {
		if i-3 > 0 {
			result = "," + s[i-3:i] + result
		} else {
			result = s[:i] + result
		}
	}
	return result
}

func mountAction(_ *cobra.Command, args []string) error {
	// Pre-check
	if err := isWinFspInstalled(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Fprintf(os.Stderr, "Solution: Please ReInstall\n")
		os.Exit(1)
	}

	if !isWinFspLauncherRunning() {
		fmt.Fprintf(os.Stderr, "Error: WinFSP Service Not Running\n")
		os.Exit(1)
	}

	defer func() {
		if r := recover(); r != nil {
			if errStr, ok := r.(string); ok {
				if strings.Contains(errStr, "cannot find winfsp") {
					fmt.Fprintf(os.Stderr, "Error: Failed to load WinFSP driver\n")
					fmt.Fprintf(os.Stderr, "Please try running this command as administrator\n")
					os.Exit(1)
				}
			}
			panic(r) // Re-throw other panics
		}
	}()

	mountPoint := strings.ToUpper(args[0])
	mountPoint = strings.TrimRight(mountPoint, "\\")
	dataDir := args[1]

	logger.Infof("sending mount request: %s to %s", dataDir, mountPoint)
	// 调试信息
	logger.Debugf("arguments: mountPoint=%s, dataDir=%s", mountPoint, dataDir)
	logger.Debugf("flags: fixed-size=%v, min-size=%d, avg-size=%d, max-size=%d, block-size=%d, compress=%v, encrypt=%v, password_set=%v",
		fixedSize, minSize, avgSize, maxSize, blockSize, compress, encrypt, password != "")

	// 创建可取消的 context
	ctx, cancel := context.WithCancel(context.Background())
	// 注册信号处理，捕获程序终止信号
	setupSignalHandler(cancel)
	// 确保在函数退出时停止 IPC 服务器
	defer func() {
		logger.Errorf("stopping ipc server on exit")
		cancel()
	}()

	// 启动 IPC 服务器，传入 ctx
	startIpcServer(ctx, mountPoint)

	BlockConf := mount.BlockConfig{
		Size:     blockSize,
		Compress: compress,
		Encrypt:  encrypt,
		Password: password,
	}
	ChunkConf := mount.ChunkConfig{
		FixedSize: fixedSize,
		MinSize:   minSize,
		AvgSize:   avgSize,
		MaxSize:   maxSize,
	}

	logger.Errorf("mounting %s to %s %+v %+v ", dataDir, mountPoint, BlockConf, ChunkConf)
	mount.Mount(mountPoint, dataDir, &mount.MountOptions{
		BlockConf: &BlockConf,
		ChunkConf: &ChunkConf,
	})

	logger.Errorf("exit mount %s to %s", mountPoint, dataDir)
	return nil
}

// unmountAction is the cobra action for unmount command
func unmountAction(_ *cobra.Command, args []string) error {
	mountPoint := strings.ToUpper(args[0])
	mountPoint = strings.TrimRight(mountPoint, "\\")
	logger.Infof("sending unmount request for %s", mountPoint)

	if err := ipccmd.InvokeUnmount(mountPoint); err != nil {
		logger.Errorf("unmount failed: %v", err)
		return err
	}
	logger.Info("unmount successful")
	return nil
}

func statsAction(_ *cobra.Command, args []string) error {
	logger.Info("collecting statistics for all mounted filesystems")
	mountPoint := strings.ToUpper(args[0])
	mountPoint = strings.TrimRight(mountPoint, "\\")
	fsStats, err := ipccmd.InvokeStats(mountPoint)
	if err != nil || fsStats == nil {
		logger.Errorf("failed to collect statistics: %v", err)
		return err
	}
	console.RenderStats(mountPoint, fsStats)
	logger.Debug("stats command completed")
	return nil
}

func debugBlockAction(mountPoint, blockID string) error {
	mountPoint = strings.ToUpper(mountPoint)
	mountPoint = strings.TrimRight(mountPoint, "\\")

	// 读取block config 配置
	blockConfBytes, err := ipccmd.InvokeXattr(mountPoint, "/", "user.dedupfs.blockconf")
	if err != nil || blockConfBytes == nil {
		logger.Errorf("failed to read block config: %v", err)
		return err
	}

	blockConf := &dfs.BlockConfig{}
	err = json.Unmarshal(blockConfBytes, blockConf)
	if err != nil {
		logger.Errorf("failed to unmarshal block config: %v", err)
		return fmt.Errorf("failed to unmarshal block config: %w", err)
	}

	dataDirBytes, err := ipccmd.InvokeXattr(mountPoint, "/", "user.dedupfs.datadir")
	if err != nil || dataDirBytes == nil {
		logger.Errorf("failed to read data dir: %v", err)
		return err
	}
	dataDir := string(dataDirBytes)

	blockPath := filepath.Join(dataDir, dfs.GetBlockPath(blockID))
	if _, err := os.Stat(blockPath); os.IsNotExist(err) {
		logger.Errorf("block not found: %s", blockID)
		return fmt.Errorf("block not found: %s", blockID)
	} else if err != nil {
		logger.Errorf("failed to check block: %v", err)
		return fmt.Errorf("failed to check block: %w", err)
	}
	blockData, err := os.ReadFile(blockPath)
	if err != nil {
		logger.Errorf("failed to read block: %v", err)
		return fmt.Errorf("failed to read block: %w", err)
	}

	infoRows := [][]string{
		{"Block ID", blockID},
		{"Mount Point", mountPoint},
		{"Data Directory", dataDir},
		{"Block Path", blockPath},
		{"Block Size", formatSize(int64(len(blockData))) + " bytes"},
	}

	var headerRows [][]string
	var chunkLines []string
	var ChunkList []*dfs.BlockChunk
	var block *dfs.Block

	block, err = dfs.DeserializeBlock(blockData)
	if err != nil {
		headerRows = [][]string{{"Parse Error", err.Error()}}
		chunkLines = []string{"Block structure is invalid or corrupted."}
		ChunkList = nil
		block = nil
	} else {
		headerRows = [][]string{
			{"ID", block.Header.ID},
			{"Version", strconv.Itoa(int(block.Header.Ver))},
			{"Etag", fmt.Sprintf("%x", block.Header.Etag)},
			{"Total Size", formatSize(block.Header.TotalSize) + " bytes"},
			{"Real Size", formatSize(block.Header.RealSize) + " bytes"},
			{"Compressed", fmt.Sprintf("%t", block.Header.Compressed)},
			{"Encrypted", fmt.Sprintf("%t", block.Header.Encrypted)},
			{"Created At", time.Unix(0, int64(block.Header.CreatedAt)).Format(time.RFC3339)},
			{"Updated At", time.Unix(0, int64(block.Header.UpdatedAt)).Format(time.RFC3339)},
			{"Chunk Count", strconv.Itoa(len(block.Header.ChunkList))},
			{"Data Size", formatSize(int64(len(block.Data))) + " bytes"},
		}

		for i, chunk := range block.Header.ChunkList {
			refCount := int32(0)

			// 特殊chunk 特殊处理
			if !strings.HasSuffix(blockID, "000000000") {
				attrName := fmt.Sprintf("user.dedupfs.chunk.meta.%s", chunk.Hash)
				if chunkMetaBytes, err := ipccmd.InvokeXattr(mountPoint, "/", attrName); err == nil && len(chunkMetaBytes) > 0 {
					var _chunk dfs.Chunk
					if err := json.Unmarshal(chunkMetaBytes, &_chunk); err == nil {
						refCount = _chunk.RefCount
					}
				}
			}

			chunkLines = append(chunkLines, fmt.Sprintf("%3d: %s, size=%d, ref=%d", i, chunk.Hash, chunk.Size, refCount))
		}
		ChunkList = block.Header.ChunkList

		if block.Header.Encrypted {
			if d, err := utils.Decrypt(block.Data, blockID+blockConf.Password); err != nil {
				logger.Errorf("decrypt block %s failed: %v", block.Header.ID, err)
				return fmt.Errorf("decrypt block failed %w", err)
			} else {
				block.Data = d
			}
		}

		if block.Header.Compressed {
			if d, err := utils.Decompress(block.Data); err != nil {
				logger.Errorf("decompress block %s failed: %v", block.Header.ID, err)
				return fmt.Errorf("decompress block failed %w", err)
			} else {
				block.Data = d
			}
		}
	}

	logger.Debug("debug block command completed")
	console.RenderBlock(mountPoint, block, infoRows, headerRows, chunkLines, ChunkList)
	return nil
}

func debugINodeAction(mountPoint, inodeName string) error {
	mountPoint = strings.ToUpper(mountPoint)
	mountPoint = strings.TrimRight(mountPoint, "\\")

	dataDirBytes, err := ipccmd.InvokeXattr(mountPoint, "/", "user.dedupfs.datadir")
	if err != nil || dataDirBytes == nil {
		logger.Errorf("failed to read data dir: %v", err)
		return err
	}
	dataDir := string(dataDirBytes)

	inodePath := path.Join("/", inodeName)
	inodePath = utils.ToUnixPath(inodePath)
	inodeBytes, err := ipccmd.InvokeXattr(mountPoint, inodePath, "user.dedupfs.inode")
	if err != nil || inodeBytes == nil {
		logger.Errorf("failed to read inode: %v", err)
		return fmt.Errorf("failed to read inode: %w", err)
	}
	var inode dfs.INode
	if err := json.Unmarshal(inodeBytes, &inode); err != nil {
		logger.Errorf("failed to unmarshal inode: %v", err)
		return fmt.Errorf("failed to unmarshal inode: %w", err)
	}

	// 准备inode信息显示数据
	infoRows := [][]string{
		{"INode Name", inode.Name},
		{"Mount Point", mountPoint},
		{"Data Directory", dataDir},
		{"INode Number", fmt.Sprintf("%d", inode.Ino)},
		{"File Size", formatSize(int64(inode.Size)) + " bytes"},
	}

	inodeRows := [][]string{
		{"Inode Number", fmt.Sprintf("%d", inode.Ino)},
		{"Name", inode.Name},
		{"Parent Inode", fmt.Sprintf("%d", inode.Parent)},
		{"Type", string(inode.Kind)},
		{"Size", formatSize(int64(inode.Size)) + " bytes"},
		{"Blocks", fmt.Sprintf("%d", inode.Blocks)},
		{"Mode", fmt.Sprintf("%o", inode.Mode)},
		{"Links", fmt.Sprintf("%d", inode.Nlink)},
		{"UID", fmt.Sprintf("%d", inode.Uid)},
		{"GID", fmt.Sprintf("%d", inode.Gid)},
		{"Block Size", fmt.Sprintf("%d", inode.Blksize)},
		{"Flags", fmt.Sprintf("%d", inode.Flags)},
		{"Access Time", inode.Atime.Format(time.RFC3339)},
		{"Modify Time", inode.Mtime.Format(time.RFC3339)},
		{"Change Time", inode.Ctime.Format(time.RFC3339)},
		{"Create Time", inode.Crtime.Format(time.RFC3339)},
		{"Chunk Count", fmt.Sprintf("%d", len(inode.Chunks))},
	}

	if inode.Kind == dfs.FileTypeSymlink && inode.SymlinkTarget != nil {
		inodeRows = append(inodeRows, []string{"Symlink Target", *inode.SymlinkTarget})
	}

	// 准备chunk数据
	var chunkLines []string

	for i, chunk := range inode.Chunks {
		attrName := fmt.Sprintf("user.dedupfs.chunk.meta.%s", chunk.Hash)
		refCount := int32(0)
		size := int32(0)
		if chunkMetaBytes, err := ipccmd.InvokeXattr(mountPoint, "/", attrName); err == nil && len(chunkMetaBytes) > 0 {
			var _chunk dfs.Chunk
			if err := json.Unmarshal(chunkMetaBytes, &_chunk); err == nil {
				refCount = _chunk.RefCount
				size = _chunk.Size
			}
		}
		chunkLines = append(chunkLines, fmt.Sprintf("%3d: %s, size=%d, ref=%d", i, chunk.Hash, size, refCount))
	}
	logger.Debug("debug inode command completed")
	console.RenderInode(mountPoint, &inode, infoRows, inodeRows, chunkLines)
	return nil
}

// restoreAction is the cobra action for restore command
func restoreAction(_ *cobra.Command, args []string) error {
	dataDir := args[0]
	toPath := args[1]

	logger.Infof("Restoring data from %s to %s", dataDir, toPath)
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		logger.Errorf("Failed to get absolute path for blocks directory: %v", err)
		return fmt.Errorf("failed to get absolute path for blocks directory: %w", err)
	}

	// 检查数据路径是否存在
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		logger.Errorf("Data path does not exist: %s", dataDir)
		return fmt.Errorf("data path does not exist: %s", dataDir)
	}

	toPath, err = filepath.Abs(toPath)
	if err != nil {
		logger.Errorf("Failed to get absolute path for target path: %v", err)
		return fmt.Errorf("failed to get absolute path for target path: %w", err)
	}
	// 确保目标路径存在
	if err := os.MkdirAll(toPath, 0755); err != nil {
		logger.Errorf("Failed to create target directory: %v", err)
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// 执行实际的恢复逻辑
	if err := console.DoRestore(dataDir+"/data", toPath); err != nil {
		logger.Error("Restore failed")
		logger.Errorf("Restore failed: %v", err)
		return err
	}

	logger.Info("Restore completed successfully")
	return nil
}

// startIpcServer 启动 IPC 服务器，接收 ctx 作为参数
func startIpcServer(ctx context.Context, mountPoint string) *ipc.Server {
	logger.Infof("starting ipc server for %s", mountPoint)
	go func() {
		// 根据 mountPoint 生成 IPC pipe 名称
		ipcpath := ipc.GetPath(mountPoint) // 创建 IPC 服务器
		server := ipc.NewServer(ipcpath)

		server.Register("unmount", ipccmd.HandleUnmountCommand)
		server.Register("stats", ipccmd.HandleStatsCommand)
		server.Register("stat", ipccmd.HandleStatCommand)
		server.Register("xattr", ipccmd.HandleXAttrCommand)

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

// 检查 WinFSP 是否安装
func isWinFspInstalled() error {
	// Check registry
	_, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\WinFsp`, registry.QUERY_VALUE)
	if err != nil {
		// Check 32-bit system registry path
		_, err = registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\WinFsp`, registry.QUERY_VALUE)
		if err != nil {
			return fmt.Errorf("WinFSP is not installed")
		}
	}

	return nil
}

func isWinFspLauncherRunning() bool {
	// 仅请求连接权限（普通用户允许）
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(scm)

	// 打开服务，仅请求查询状态权限
	svcName, _ := windows.UTF16PtrFromString("WinFsp.Launcher")
	svc, err := windows.OpenService(scm, svcName, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return false
	}
	defer windows.CloseServiceHandle(svc)

	var status windows.SERVICE_STATUS
	err = windows.QueryServiceStatus(svc, &status)
	if err != nil {
		return false
	}

	// 检查是否正在运行
	return status.CurrentState == windows.SERVICE_RUNNING
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
