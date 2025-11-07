//go:build windows

package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"github.com/mageg-x/dedupfs/common/cmd"
	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/log"
	"github.com/mageg-x/dedupfs/common/mount"
	"github.com/mageg-x/dedupfs/common/utils"
)

//go:embed frontend/dist
var Assets embed.FS

var (
	logger = log.GetLogger("dedupfs")
)

// Stats 统计信息结构
type Stats struct {
	FsId             string  `json:"fsId"`
	BaseDir          string  `json:"baseDir"`
	Files            int     `json:"files"`
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
		if !mount.IsDriveAvailable(_mp.MountPath) {
			logger.Errorf("drive %s not available", _mp.MountPath)
			return fmt.Errorf("drive %s not available", _mp.MountPath)
		}
		mp = _mp
		return nil
	})
	if err != nil || mp == nil {
		logger.Errorf("mount point %s not exists", id)
		return err
	}

	// 构建命令行参数
	args := []string{"mount", mp.MountPath, mp.DataDir}

	// // 添加块配置参数
	// if mp.BlockConfig != nil {
	// 	args = append(args, "--block-size", fmt.Sprintf("%d", mp.BlockConfig.Size))
	// 	args = append(args, "--compress", fmt.Sprintf("%t", mp.BlockConfig.Compress))
	// 	args = append(args, "--encrypt", fmt.Sprintf("%t", mp.BlockConfig.Encrypt))
	// 	if mp.BlockConfig.Password != "" {
	// 		args = append(args, "--password", mp.BlockConfig.Password)
	// 	}
	// }

	// // 添加分块配置参数
	// if mp.ChunkConfig != nil {
	// 	args = append(args, "--fixed-size", fmt.Sprintf("%t", mp.ChunkConfig.FixedSize))
	// 	args = append(args, "--min-size", fmt.Sprintf("%d", mp.ChunkConfig.MinSize))
	// 	args = append(args, "--avg-size", fmt.Sprintf("%d", mp.ChunkConfig.AvgSize))
	// 	args = append(args, "--max-size", fmt.Sprintf("%d", mp.ChunkConfig.MaxSize))
	// }

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
	command := exec.Command(exePath, args...)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}

	if err := command.Start(); err != nil {
		logger.Errorf("failed to start mount process: %v", err)
		return fmt.Errorf("failed to start mount process: %w", err)
	}

	// 等待最多 5 秒，每 200ms 检查一次
	alive := false
	for i := 0; i < 25; i++ {
		if cmd.InvokeStat(mp.MountPath) {
			alive = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !alive {
		logger.Errorf("mount %s failed", mp.MountPath)
		// 清理
		command.Process.Kill()
		mount.ForceUnmount(mp.MountPath)
		return fmt.Errorf("mount %s failed", mp.MountPath)
	}

	logger.Infof("mount %s success", mp.MountPath)
	mp.cmd = command
	mp.IsMounted = true
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

	if err := cmd.InvokeUnmount(mp.MountPath); err != nil {
		if err := mount.ForceUnmount(mp.MountPath); err != nil {
			logger.Errorf("force unmount %s failed: %v", mp.MountPath, err)
			return fmt.Errorf("unmount failed")
		}
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
			RealSize:         stats.RealSize,
			TotalChunks:      stats.ChunkCount,
			Blocks:           stats.BlockCount,
			ReferencedChunks: stats.RefChunkCount,
			CompressionRatio: stats.DeduplicationRatio,
			LastUpdated:      stats.LastUpdated.Local().Format("2006-01-02 15:04:05"),
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
	mount.CleanupMounts()

	logger.Info("Cleanup completed successfully")
}

// IsElevated 检查当前进程是否具有管理员权限。
func IsElevated() bool {
	var token windows.Token
	if err := windows.OpenProcessToken(windows.CurrentProcess(), windows.TOKEN_QUERY, &token); err != nil {
		return false
	}
	defer token.Close()

	elevated := token.IsElevated()
	return elevated
}

// ElevateSelf 以管理员身份重新启动当前程序（无参数）。
func ElevateSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := windows.UTF16PtrFromString(exe)
	if err != nil {
		return err
	}

	shell32 := windows.NewLazyDLL("shell32.dll")
	shellExecute := shell32.NewProc("ShellExecuteW")

	ret, _, _ := shellExecute.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0, // params
		0, // dir
		uintptr(windows.SW_SHOW),
	)

	if ret <= 32 {
		if ret == 1223 { // ERROR_CANCELLED
			return errors.New("需要管理员权限才能执行此操作")
		}
		return errors.New("请求管理员权限失败")
	}

	os.Exit(0)
	return nil
}

// Gui 启动GUI模式
func runGui() {
	logger.Infof("Initializing GUI mode...")

	// 初始化应用
	app := NewApp()

	// 创建 Wails 应用
	_, cancel := context.WithCancel(context.Background())
	err := wails.Run(&options.App{
		Title:  "DedupFS Manager",
		Width:  800,
		Height: 540,
		AssetServer: &assetserver.Options{
			Assets: Assets,
		},
		BackgroundColour: &options.RGBA{R: 15, G: 23, B: 42, A: 128},
		OnStartup:        app.startup,
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
