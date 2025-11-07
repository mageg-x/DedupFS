//go:build windows

package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/utils"
	"github.com/winfsp/cgofuse/fuse"
)

func Mount(mountPoint, sourceDir string, options *MountOptions) error {
	logger.Infof("Mounting deduplicated file system at %s", mountPoint)
	err := utils.WithLock(&MountMutex, func() error {
		if _, ok := MountMap[mountPoint]; ok {
			return fmt.Errorf("mount point %s is already mounted", mountPoint)
		}
		return nil
	})
	if err != nil {
		logger.Errorf("mount point %s is already mounted", mountPoint)
		return err
	}

	// 把 sourceDir 转成 绝对路径
	sourceDir, err = filepath.Abs(sourceDir)
	if err != nil {
		logger.Errorf("Failed to get absolute path for source directory: %v", err)
		return fmt.Errorf("Failed to get absolute path for source directory: %w", err)
	}
	// 判断 sourceDir 是否存在
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		logger.Errorf("Source directory does not exist: %s", sourceDir)
		return fmt.Errorf("Source directory does not exist: %s", sourceDir)
	}

	ForceUnmount(mountPoint)

	// 设置默认配置
	chunkConf := &dfs.ChunkConfig{
		FixedSize: false,
		MinSize:   1024 * 1024,
		AvgSize:   2 * 1024 * 1024,
		MaxSize:   4 * 1024 * 1024,
	}

	blockConf := &dfs.BlockConfig{
		Size:     1024 * 1024 * 64,
		Compress: true,
		Encrypt:  false,
	}

	// 如果提供了选项，则覆盖默认配置
	if options != nil {
		if options.ChunkConf != nil {
			chunkConf.FixedSize = options.ChunkConf.FixedSize
			chunkConf.MinSize = options.ChunkConf.MinSize
			chunkConf.AvgSize = options.ChunkConf.AvgSize
			chunkConf.MaxSize = options.ChunkConf.MaxSize
		}

		if options.BlockConf != nil {
			blockConf.Size = options.BlockConf.Size
			blockConf.Compress = options.BlockConf.Compress
			blockConf.Encrypt = options.BlockConf.Encrypt
			blockConf.Password = options.BlockConf.Password
		}
	}
	fsys, err := dfs.NewDedupFS(mountPoint, sourceDir, chunkConf, blockConf)
	if err != nil {
		logger.Errorf("failed to create dedupfs instance: %v", err)
		return fmt.Errorf("failed to create dedupfs instance: %w", err)
	}
	logger.Infof("created dedupfs instance with id: %s", fsys.ID)

	host := fuse.NewFileSystemHost(fsys)
	//  设置文件系统能力
	// host.SetCapCaseInsensitive(true) // 支持大小写不敏感
	host.SetCapReaddirPlus(true) // 支持增强的目录读取

	// 锁定目录
	// if ld, err := utils.LockDirectory(sourceDir); err != nil {
	// 	logger.Errorf("failed to lock source directory: %v", err)
	// 	return fmt.Errorf("failed to lock source directory: %w", err)
	// } else {
	// 	defer ld.Close()
	// }

	// 配置挂载参数
	opts := []string{
		mountPoint,
	}

	fsys.Host = host

	// 添加到已挂载目录列表并存储文件系统实例
	utils.WithLock(&MountMutex, func() error {
		MountMap[mountPoint] = fsys
		return nil
	})

	defer func() {
		// 确保在函数退出时停止 fuse 服务器
		utils.WithLock(&MountMutex, func() error {
			delete(MountMap, mountPoint)
			return nil
		})
	}()

	logger.Errorf("Mounting deduplicated file system at %s", mountPoint)
	// 执行挂载， 会阻塞住
	success := host.Mount("", opts)
	if !success {
		logger.Errorf("Failed to mount deduplicated file system at %s", mountPoint)
		// 如果失败，可以获取具体的错误信息（在某些版本中可能需要通过其他方式）
		return syscall.EAGAIN // 或返回一个更具描述性的错误
	}

	return nil
}

func Unmount(mountPoint string) error {
	logger.Infof("unmounting deduplicated file system at %s", mountPoint)
	// 从已挂载目录列表中移除
	MountMutex.Lock()
	defer MountMutex.Unlock()

	fs, ok := MountMap[mountPoint]
	if !ok {
		logger.Errorf("mount point not found: %s", mountPoint)
		return fmt.Errorf("mount point not found: %s", mountPoint)
	}

	if fs != nil && fs.Host != nil {
		// 把 fs.Host 转成  *FileSystemHost
		host, ok := fs.Host.(*fuse.FileSystemHost)
		if ok && host != nil {
			success := host.Unmount()
			if !success {
				logger.Errorf("Failed to unmount deduplicated file system at %s", mountPoint)
				return fmt.Errorf("Failed to unmount deduplicated file system at %s", mountPoint)
			}
			fs.Host = nil
		} else {
			logger.Errorf("Failed to unmount deduplicated file system at %s", mountPoint)
			return fmt.Errorf("Failed to unmount deduplicated file system at %s", mountPoint)
		}
	}
	delete(MountMap, mountPoint)
	logger.Infof("successfully unmounted file %s", mountPoint)
	return nil
}

// 强制清理挂载点
func ForceUnmount(mountPoint string) error {
	var (
		mpr                   = syscall.NewLazyDLL("mpr.dll")
		wNetCancelConnection2 = mpr.NewProc("WNetCancelConnection2W")
	)

	drivePtr, err := syscall.UTF16PtrFromString(mountPoint[:2]) // "S:"
	if err != nil {
		logger.Errorf("Failed to convert drive letter to UTF-16: %v", err)
		return err
	}

	// 参数: 驱动器名, 标志(0=立即断开, 1=无连接时失败), 强制断开(TRUE=1)
	ret, _, err := wNetCancelConnection2.Call(
		uintptr(unsafe.Pointer(drivePtr)),
		uintptr(0x1), // CONNECT_UPDATE_PROFILE: 确保永久断开，防止重启后重现
		uintptr(1),   // TRUE: 强制断开
	)

	if ret != 0 {
		return fmt.Errorf("WNetCancelConnection2 failed: %d", ret)
	}

	// 轮询检查盘符是否可用，例如最多等待3秒，每次检查间隔300毫秒
	timeout := 3 * time.Second
	checkInterval := 300 * time.Millisecond
	startTime := time.Now()

	for time.Since(startTime) < timeout {
		if IsDriveAvailable(mountPoint) { // 你需要实现这个函数
			return nil
		}
		time.Sleep(checkInterval)
	}
	return fmt.Errorf("unmount %s time out", mountPoint)
}

// 检查盘符是否可用
func IsDriveAvailable(drive string) bool {
	_, err := os.Stat(drive + "\\")
	if err != nil {
		return true // 盘符不存在，可用
	}
	return false // 盘符存在，被占用
}
