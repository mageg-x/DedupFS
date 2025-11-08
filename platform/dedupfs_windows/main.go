//go:build windows

package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/mageg-x/dedupfs/common/cmd"
	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/log"
	"github.com/mageg-x/dedupfs/common/mount"
	"github.com/mageg-x/dedupfs/common/utils"
)

//go:embed frontend/dist
var Assets embed.FS

//go:embed frontend/assets/icon.ico
var iconICO []byte

var (
	logger = log.GetLogger("dedupfs")
)

// Stats 统计信息结构
type Stats struct {
	FsId             string  `json:"fsId"`
	BaseDir          string  `json:"baseDir"`
	Files            int     `json:"files"`
	SpaceUsed        uint64  `json:"spaceUsed"`
	Directories      int     `json:"directories"`
	RealSize         int64   `json:"realSize"`
	TotalChunks      int     `json:"totalChunks"`
	Blocks           int     `json:"blocks"`
	ReferencedChunks int     `json:"referencedChunks"`
	CompressionRatio float64 `json:"compressionRatio"`
	LastUpdated      string  `json:"lastUpdated"`
}

// MountPoint 挂载点结构，与前端数据结构保持一致
type MountPoint struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	MountPath   string           `json:"mountPath"`
	DataDir     string           `json:"dataDir"`
	IsMounted   bool             `json:"isMounted"`
	UsedSpace   int64            `json:"usedSpace"`
	TotalSpace  int64            `json:"totalSpace"`
	BlockConfig *dfs.BlockConfig `json:"blockConfig,omitempty"`
	ChunkConfig *dfs.ChunkConfig `json:"chunkConfig,omitempty"`
	Stats       *Stats           `json:"stats,omitempty"`
	cmd         *exec.Cmd        `json:"-"`
}

// App 结构体，绑定到前端
type App struct {
	ctx        context.Context
	mps        map[string]*MountPoint
	mu         sync.RWMutex
	configFile string
	cancelFunc context.CancelFunc
}

// NewApp 创建 App 实例
func NewApp() *App {
	return &App{
		mps: make(map[string]*MountPoint),
	}
}

// Wails 启动时注入上下文
func (a *App) startup(ctx context.Context) {
	logger.SetLevel(logrus.DebugLevel)
	logger.Info("app started")
	a.ctx = ctx

	// 获取程序安装目录
	appDir, err := os.Executable()
	if err != nil {
		// 如果获取失败，使用当前目录
		appDir, _ = os.Getwd()
	} else {
		appDir = filepath.Dir(appDir)
	}

	// 设置配置文件路径
	a.configFile = filepath.Join(appDir, "config.json")

	// 从配置文件加载挂载点信息
	a.loadConfig()

	for _, mp := range a.mps {
		logger.Infof("loading mount point: %#v", *mp)
		if mp.IsMounted {
			// 启动挂载点
			if err := a.Mount(mp.ID); err != nil {
				logger.Errorf("failed to mount %s: %v", mp.Name, err)
				mp.IsMounted = false
			}
		}
	}

	// 启动一个定时器， 定时检查挂载点状态
	go func() {
		ticker := time.NewTicker(time.Second * 5)
		defer ticker.Stop()
		for range ticker.C {
			for _, mp := range a.mps {
				if mp.IsMounted {
					// 检查挂载点状态
					if !mount.IsDriveAvailable(mp.MountPath) {
						utils.WithLock(&a.mu, func() error {
							mp.IsMounted = false
							return nil
						})

						continue
					}

					if !cmd.InvokeStat(mp.MountPath) {
						utils.WithLock(&a.mu, func() error {
							mp.IsMounted = false
							return nil
						})

						continue
					}
				} else {
					if cmd.InvokeStat(mp.MountPath) {
						utils.WithLock(&a.mu, func() error {
							mp.IsMounted = true
							return nil
						})
						continue
					}
				}
			}
		}
	}()
}

// loadConfig 从配置文件加载挂载点信息，动态字段设置默认值
func (a *App) loadConfig() {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 检查配置文件是否存在
	if _, err := os.Stat(a.configFile); os.IsNotExist(err) {
		logger.Errorf("not found config %s exists", a.configFile)
		return
	}

	// 读取配置文件
	data, err := os.ReadFile(a.configFile)
	if err != nil {
		logger.Errorf("failed to read config: %v", err)
		return
	}

	// 解析配置文件
	var mps map[string]*MountPoint
	if err := json.Unmarshal(data, &mps); err != nil {
		logger.Errorf("failed to unmarshal config from data %s: %v ", data, err)
		return
	}
	for _, mp := range mps {
		a.mps[mp.ID] = mp
	}
	logger.Infof("loaded mount points: %#v", mps)
}

// saveConfig 保存挂载点信息到配置文件，不包含动态字段
func (a *App) SaveConfig() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	newMPS := make(map[string]*MountPoint)
	for _, mp := range a.mps {
		mp.Stats = nil
		newMPS[mp.ID] = mp
	}
	a.mps = newMPS
	// 序列化配置
	data, err := json.MarshalIndent(a.mps, "", "  ")
	if err != nil {
		logger.Errorf("failed to marshal config: %v", err)
		return err
	}

	// 写入配置文件
	err = os.WriteFile(a.configFile, data, 0644)
	if err != nil {
		logger.Errorf("failed to write config: %v", err)
		return err
	}
	logger.Infof("success save config to %s", a.configFile)
	return nil
}

// CreateDefaultConfig 创建默认配置
func (a *App) CreateDefaultConfig() *MountPoint {
	// 创建默认挂载点配置
	defaultMP := &MountPoint{
		ID:         uuid.New().String(),
		Name:       "默认挂载点",
		MountPath:  "X:",
		DataDir:    "D:\\data",
		IsMounted:  false, // 动态字段
		UsedSpace:  0,     // 动态字段
		TotalSpace: 0,     // 动态字段
		BlockConfig: &dfs.BlockConfig{
			Size:     64, // 64MB
			Compress: true,
			Encrypt:  false,
		},
		ChunkConfig: &dfs.ChunkConfig{
			FixedSize: false,
			MinSize:   1024,     // 1MB
			AvgSize:   2 * 1024, // 2MB
			MaxSize:   4 * 1024, // 4MB
		},
		Stats: &Stats{}, // 动态字段
	}

	if driverList, err := a.BrowseDriver(); err == nil && len(driverList) > 0 {
		// 最后一个驱动器
		defaultMP.MountPath = driverList[len(driverList)-1]
	}
	return defaultMP
}

// GetMountPoints 是暴露给前端的方法（首字母大写！）
func (a *App) GetMountPoints() ([]*MountPoint, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	mountPoints := []*MountPoint{}
	for _, mp := range a.mps {
		mountPoints = append(mountPoints, mp)
	}
	logger.Infof("get mount points: %#v", mountPoints)
	return mountPoints, nil
}

func (a *App) GetMountPoint(id string) (*MountPoint, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.mps[id], nil
}

// SaveMountPoint 保存挂载点配置（供前端调用）
func (a *App) SaveMountPoint(mp *MountPoint) error {
	utils.WithLock(&a.mu, func() error {
		// 更新挂载点
		a.mps[mp.ID] = mp
		return nil
	})

	// 保存配置到文件
	return a.SaveConfig()
}

// AddMountPoint 添加新挂载点（供前端调用）
func (a *App) AddMountPoint(mp *MountPoint) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// 添加挂载点
	a.mps[mp.ID] = mp
	return nil
}

// DeleteMountPoint 删除挂载点（供前端调用）
func (a *App) DeleteMountPoint(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	// 删除挂载点
	delete(a.mps, id)
	return nil
}

// BrowseDriver 返回 Windows 系统中未被使用的盘符（保留兼容）
func (a *App) BrowseDriver() ([]string, error) {
	var (
		kernel32  = syscall.NewLazyDLL("kernel32.dll")
		getDrives = kernel32.NewProc("GetLogicalDrives")
	)

	// 获取已使用的驱动器位掩码
	bitmask, _, err := getDrives.Call()
	if bitmask == 0 {
		return nil, err
	}

	// 使用 map 作为 set 来去重
	uniquePaths := make(map[string]bool)

	// 添加未使用的盘符
	for i := 0; i < 26; i++ {
		if bitmask&(1<<uint(i)) == 0 {
			path := fmt.Sprintf("%c:", 'A'+i)
			uniquePaths[path] = true
		}
	}

	// 加上 a.mps 中的挂载点
	for _, mp := range a.mps {
		uniquePaths[mp.MountPath] = true
	}

	// 转换回切片
	unused := make([]string, 0, len(uniquePaths))
	for path := range uniquePaths {
		unused = append(unused, path)
	}

	// 排序
	sort.Strings(unused)

	return unused, nil
}

func (a *App) BrowseDataDir(title, initialDir string) (string, error) {
	// 判断 initialDir 是否存在
	if _, err := os.Stat(initialDir); os.IsNotExist(err) {
		initialDir = ""
	}

	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: initialDir,
	})
}

// Mount 挂载文件系统（供前端调用）
func (a *App) Mount(id string) error {
	var mp *MountPoint
	err := utils.WithLock(&a.mu, func() error {
		// 查找挂载点
		_mp, exists := a.mps[id]
		if !exists {
			return fmt.Errorf("mount point not exists")
		}

		mp = _mp
		return nil
	})
	if err != nil || mp == nil {
		logger.Errorf("mount point %s not exists", id)
		return err
	}

	err = utils.RetryCall(5, func() error {
		if mount.IsDriveAvailable(mp.MountPath) {
			cmd.InvokeUnmount(mp.MountPath)
			return fmt.Errorf("drive %s not available", mp.MountPath)
		}
		return nil
	})
	if err != nil {
		logger.Errorf("drive %s not available", mp.MountPath)
		return err
	}
	// 构建命令行参数
	args := []string{"mount", mp.MountPath, mp.DataDir}

	// 添加块配置参数
	if mp.BlockConfig != nil {
		args = append(args, "--block-size="+fmt.Sprintf("%d", mp.BlockConfig.Size*1024*1024))
		args = append(args, "--compress="+fmt.Sprintf("%t", mp.BlockConfig.Compress))
		args = append(args, "--encrypt="+fmt.Sprintf("%t", mp.BlockConfig.Encrypt))
		if mp.BlockConfig.Password != "" {
			args = append(args, "--password="+mp.BlockConfig.Password)
		}
	}

	// 添加分块配置参数
	if mp.ChunkConfig != nil {
		args = append(args, "--fixed-size="+fmt.Sprintf("%t", mp.ChunkConfig.FixedSize))
		args = append(args, "--min-size="+fmt.Sprintf("%d", mp.ChunkConfig.MinSize*1024))
		args = append(args, "--avg-size="+fmt.Sprintf("%d", mp.ChunkConfig.AvgSize*1024))
		args = append(args, "--max-size="+fmt.Sprintf("%d", mp.ChunkConfig.MaxSize*1024))
	}

	// 获取可执行文件路径
	exePath := "dedupfs-cli.exe"
	// 尝试查找完整路径
	if fullPath, err := exec.LookPath(exePath); err == nil {
		exePath = fullPath
	} else {
		// 尝试在当前目录查找
		currentDir, _ := os.Getwd()
		localExePath := filepath.Join(currentDir, exePath)
		if _, err := os.Stat(localExePath); err == nil {
			exePath = localExePath
		}
	}

	// 启动进程
	logger.Errorf("starting mount process: %s %+v", exePath, args)
	command := exec.Command(exePath, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := command.Start(); err != nil {
		logger.Errorf("failed to start mount process: %v", err)
		return fmt.Errorf("failed to start mount process: %w", err)
	}

	// 等待最多 5 秒，每 200ms 检查一次
	err = utils.RetryCall(5, func() error {
		if cmd.InvokeStat(mp.MountPath) {
			return nil
		}
		return fmt.Errorf("stat %s failed", mp.MountPath)
	})
	if err != nil {
		logger.Errorf("mount %s failed", mp.MountPath)
		// 清理
		if command.Process != nil {
			command.Process.Kill()
		} else {
			cmd.InvokeUnmount(mp.MountPath)
		}

		return fmt.Errorf("mount %s failed", mp.MountPath)
	}

	logger.Infof("mount %s success", mp.MountPath)
	mp.cmd = command
	mp.IsMounted = true

	a.Stats(id)

	utils.WithLock(&a.mu, func() error {
		// 更新挂载点
		a.mps[mp.ID] = mp
		return nil
	})

	a.SaveConfig()

	return nil
}

// Unmount 卸载文件系统（供前端调用）
func (a *App) Unmount(id string) error {
	var mp *MountPoint
	err := utils.WithLock(&a.mu, func() error {
		// 查找挂载点
		_mp, exists := a.mps[id]
		if !exists {
			return fmt.Errorf("mount point not exists")
		}
		mp = _mp
		return nil
	})

	if err != nil || mp == nil {
		logger.Errorf("mount point not exists")
		return err
	}

	// 如果已经是未挂载，则直接返回
	if !mount.IsDriveAvailable(mp.MountPath) {
		mp.IsMounted = false
		logger.Infof("drive %s not available", mp.MountPath)
		return nil
	}

	if err := cmd.InvokeUnmount(mp.MountPath); err != nil {
		logger.Errorf("force unmount %s failed: %v", mp.MountPath, err)
		return fmt.Errorf("unmount failed")
	}

	// 标记为未挂载
	mp.IsMounted = false

	a.SaveConfig()

	return nil
}

func (a *App) Stats(id string) (*Stats, error) {
	var mp *MountPoint
	err := utils.WithLock(&a.mu, func() error {
		// 查找挂载点
		_mp, exists := a.mps[id]
		if !exists {
			return fmt.Errorf("mount point not exists")
		}
		mp = _mp
		return nil
	})

	if err != nil || mp == nil || !mp.IsMounted {
		logger.Errorf("mount point not exists")
		return nil, fmt.Errorf("mount point not exists")
	}

	if stats, err := cmd.InvokeStats(mp.MountPath); err != nil {
		logger.Errorf("get stats failed: %v", err)
		return nil, fmt.Errorf("get stats failed")
	} else {
		mp.Stats = &Stats{
			FsId:             stats.ID,
			BaseDir:          stats.BaseDir,
			Files:            stats.FileCount,
			Directories:      stats.DirCount,
			SpaceUsed:        stats.SpaceUsed,
			RealSize:         stats.RealSize,
			TotalChunks:      stats.ChunkCount,
			Blocks:           stats.BlockCount,
			ReferencedChunks: stats.RefChunkCount,
			CompressionRatio: stats.DeduplicationRatio,
			LastUpdated:      stats.LastUpdated.Local().Format("2006-01-02 15:04:05"),
		}
		mp.UsedSpace = int64(stats.SpaceUsed)
		if _, _, space, err := utils.GetDiskFreeSpaceEx(mp.MountPath); err == nil {
			mp.TotalSpace = int64(space) + mp.UsedSpace
		}
	}
	return mp.Stats, nil
}

// IsWinFspInstalled 检查 WinFsp 是否安装（Windows 专用）
func (a *App) IsWinFspInstalled() bool {
	_, err := exec.LookPath("winfsp-launcher.exe")
	if err != nil {
		// 或检查系统目录
		_, err = exec.LookPath("C:\\Windows\\System32\\winfsp-x64.dll")
	}
	return err == nil
}

// Cleanup 执行程序退出时的清理工作
func (a *App) Cleanup() {
	logger.Info("Starting cleanup process...")

	// 先保存配置
	if err := a.SaveConfig(); err != nil {
		logger.Errorf("Failed to save config during cleanup: %v", err)
	}

	// 卸载所有挂载点
	for _, mp := range a.mps {
		cmd.InvokeUnmount(mp.MountPath)
	}
	// 强制杀死所有 mount 进程
	command := exec.Command("taskkill", "/F", "/IM", "dedupfs-cli.exe")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	command.Run()
	logger.Info("Cleanup completed successfully")
}

// 最小化窗口到托盘的方法
func (a *App) HideWindow() {
	logger.Info("Minimizing application to tray")
	runtime.WindowHide(a.ctx)
}

// 显示应用窗口的方法
func (a *App) ShowWindow() {
	logger.Info("Showing application window")
	runtime.WindowShow(a.ctx)
}

// 托盘逻辑（同前）
func (a *App) onReady() {
	systray.SetIcon(iconICO)
	systray.SetTooltip("DedupFS")

	show := systray.AddMenuItem("显示", "显示")
	quit := systray.AddMenuItem("退出", "退出")

	go func() {
		for {
			select {
			case <-show.ClickedCh:
				a.ShowWindow()
			case <-quit.ClickedCh:
				systray.Quit()
				a.QuitApp()
				os.Exit(0) // 不优雅，但能用
				return
			}
		}
	}()
}

func (a *App) onExit() {}

// 真正退出应用的方法
func (a *App) QuitApp() {
	logger.Info("Quitting application")
	a.Cleanup()
	runtime.Quit(a.ctx)
}

// Gui 启动GUI模式
func runGui() {
	logger.Infof("Initializing GUI mode...")

	// 强制杀死所有 mount 进程
	command := exec.Command("taskkill", "/F", "/IM", "dedupfs-cli.exe")
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	command.Run()

	// 初始化应用
	app := NewApp()

	// 创建 Wails 应用
	_, cancel := context.WithCancel(context.Background())
	app.cancelFunc = cancel

	// 启动托盘
	go func() {
		systray.Run(app.onReady, app.onExit)
	}()

	err := wails.Run(&options.App{
		Title:  "DedupFS Manager",
		Width:  800,
		Height: 540,
		// 关键：禁用默认关闭行为
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			EnableSwipeGestures:  true,
		},
		AssetServer: &assetserver.Options{
			Assets: Assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 128},
		OnStartup:        app.startup,
		OnBeforeClose: func(ctx context.Context) (preventQuit bool) {
			app.HideWindow()
			return true // 阻止退出
		},
		OnShutdown: func(ctx context.Context) {
			logger.Info("Application shutting down gracefully")
			app.Cleanup()
			cancel()
		},
		Bind: []interface{}{
			app,
		},
	})

	// 捕获系统信号
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		logger.Info("Received shutdown signal, cleaning up...")
		app.Cleanup()
		cancel()
		// 在Windows环境下，使用os.Exit终止进程
		os.Exit(0)
	}()

	if err != nil {
		// 发生错误时也执行清理
		logger.Errorf("Failed to start application: %v", err)
		app.Cleanup()
		os.Exit(1)
	}
	logger.Info("Application exited")
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
	runGui()
}
