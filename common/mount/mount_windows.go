//go:build windows

package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

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
	os.MkdirAll(sourceDir, 0777)
	// 判断 sourceDir 是否存在
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		logger.Errorf("Source directory does not exist: %s", sourceDir)
		return fmt.Errorf("Source directory does not exist: %s", sourceDir)
	}

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
	host.SetCapReaddirPlus(true) // 支持增强的目录读取

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

// 检查盘符是否可用
func IsDriveAvailable(drive string) bool {
	_, err := os.Stat(drive + "\\")
	if err != nil {
		return false // 盘符不存在，可用
	}
	return true // 盘符存在，被占用
}
