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
	"unsafe"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/sys/windows"

	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/mount"
	"github.com/mageg-x/dedupfs/common/utils"
)

//go:embed frontend/dist
var assets embed.FS

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
		Stats: nil, // 动态字段
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
	a.mu.Lock()
	defer a.mu.Unlock()

	// 查找挂载点
	mp, exists := a.mps[id]
	if !exists {
		logger.Errorf("MountPoint Not Exists")
		return fmt.Errorf("MountPoint Not Exists")
	}

	err := mount.Mount(mp.MountPath, mp.DataDir, &mount.MountOptions{
		ChunkConf: &mount.ChunkConfig{
			FixedSize: mp.ChunkConfig.FixedSize,
			MinSize:   mp.ChunkConfig.MinSize,
			AvgSize:   mp.ChunkConfig.AvgSize,
			MaxSize:   mp.ChunkConfig.MaxSize,
		},
		BlockConf: &mount.BlockConfig{
			Compress: mp.BlockConfig.Compress,
			Encrypt:  mp.BlockConfig.Encrypt,
			Size:     mp.BlockConfig.Size,
			Password: mp.BlockConfig.Password,
		},
	})

	if err != nil {
		logger.Errorf("Failed to mount: %v", err)
		return fmt.Errorf("Mount Failed")
	}

	// 标记为已挂载
	mp.IsMounted = true

	return nil
}

// Unmount 卸载文件系统（供前端调用）
func (a *App) Unmount(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 查找挂载点
	mp, exists := a.mps[id]
	if !exists {
		logger.Errorf("MountPoint Not Exists")
		return fmt.Errorf("MountPoint Not Exists")
	}

	err := mount.Unmount(mp.MountPath)
	if err != nil {
		logger.Errorf("Failed to unmount: %v", err)
		return fmt.Errorf("Mount Failed")
	}
	// 标记为未挂载
	mp.IsMounted = false

	// TODO: 实际卸载逻辑

	return nil
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

// runGui 启动GUI模式
func runGui() {
	logger.Info("Initializing GUI mode...")

	// 初始化应用
	app := NewApp()

	// 创建 Wails 应用
	_, cancel := context.WithCancel(context.Background())
	err := wails.Run(&options.App{
		Title:  "DedupFS Manager",
		Width:  800,
		Height: 540,
		AssetServer: &assetserver.Options{
			Assets: assets,
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
