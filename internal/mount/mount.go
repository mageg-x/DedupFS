package mount

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/mageg-x/dedupfs/dfs"
	"github.com/mageg-x/dedupfs/internal/log"
)

// ChunkConfig represents chunk configuration options
type ChunkConfig struct {
	FixedSize bool
	MinSize   int64
	AvgSize   int64
	MaxSize   int64
}

// BlockConfig represents block configuration options
type BlockConfig struct {
	Size     int64
	Compress bool
	Encrypt  bool
	Password string
}

// MountOptions contains all mount options
type MountOptions struct {
	ChunkConf *ChunkConfig
	BlockConf *BlockConfig
}

var (
	// 获取logger实例用于输出日志
	logger = log.GetLogger("dedupfs")
	// 存储已挂载的目录和对应的文件系统实例，用于程序异常退出时清理
	mountMap = make(map[string]*dfs.DedupFS)
	// 用于保护mountMap的互斥锁
	mountMutex sync.RWMutex
)

// Mount mounts the deduplicated file system at the specified mount point
func Mount(mountPoint, sourceDir string, options *MountOptions) error {
	logger.Infof("starting mount process: %s -> %s, opt %#v, %#v", sourceDir, mountPoint, options.BlockConf, options.ChunkConf)

	var err error
	mountPoint, err = filepath.Abs(mountPoint)
	if err != nil {
		logger.Errorf("failed to get absolute path for mount point: %v", err)
		return fmt.Errorf("failed to get absolute path for mount point: %w", err)
	}

	sourceDir, err = filepath.Abs(sourceDir)
	if err != nil {
		logger.Errorf("failed to get absolute path for source directory: %v", err)
		return fmt.Errorf("failed to get absolute path for source directory: %w", err)
	}

	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		logger.Errorf("source directory does not exist: %s", sourceDir)
		return fmt.Errorf("source directory does not exist: %s", sourceDir)
	}

	logger.Infof("mountPoint is: %s , sourceDir is: %s", mountPoint, sourceDir)

	// 设置默认配置
	chunkConf := &dfs.ChunkConfig{
		FixedSize: false,
		MinSize:   4096,
		AvgSize:   8 * 1024,
		MaxSize:   16 * 1024,
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

	fuse.Unmount(mountPoint)

	// Mount the file system
	logger.Infof("attempting to mount file system at %s", mountPoint)
	c, err := fuse.Mount(mountPoint,
		fuse.FSName("dedupfs"),
		fuse.Subtype("dedupfs"),
		fuse.AllowOther(),
	)

	if err != nil {
		c, err = fuse.Mount(mountPoint,
			fuse.FSName("dedupfs"),
			fuse.Subtype("dedupfs"),
		)
		if err != nil {
			logger.Errorf("failed to mount file system: %v", err)
			return fmt.Errorf("failed to mount file system: %w", err)
		}
	}
	logger.Info("successfully mounted file system")

	// 添加到已挂载目录列表并存储文件系统实例
	mountMutex.Lock()
	mountMap[mountPoint] = fsys
	mountMutex.Unlock()
	logger.Debugf("added %s to mounted directories list", mountPoint)
	defer c.Close()

	// 创建一个通道来接收服务错误
	serveErrCh := make(chan error)

	// 在单独的goroutine中提供文件系统服务
	go func() {
		logger.Info("starting to serve file system")
		if err := fs.Serve(c, fsys); err != nil {
			logger.Errorf("failed to serve file system: %v", err)
			serveErrCh <- err
			return
		}
		// 服务正常结束
		close(serveErrCh)
	}()

	// 等待服务错误或被中断
	err = <-serveErrCh
	if err != nil {
		return fmt.Errorf("failed to serve file system: %w", err)
	}

	return nil
}

// CleanupMounts cleans up all mounted directories
func CleanupMounts() {
	mountMutex.RLock()
	// 创建副本以避免在遍历过程中修改map
	mountPoints := make([]string, 0, len(mountMap))
	for mp := range mountMap {
		mountPoints = append(mountPoints, mp)
	}
	mountMutex.RUnlock()

	for _, mp := range mountPoints {
		logger.Info("cleaning up mounted directory: ", mp)
		if err := Unmount(mp); err != nil {
			logger.Errorf("failed to unmount %s during cleanup: %v", mp, err)
		} else {
			logger.Info("successfully unmounted", mp, "during cleanup")
		}
	}
}

// Unmount unmounts the file system from the specified mount point
func Unmount(mountPoint string) error {
	logger.Debugf("starting unmount process for: %s", mountPoint)

	// Check if mount point exists
	if _, err := os.Stat(mountPoint); os.IsNotExist(err) {
		logger.Errorf("mount point does not exist: %s", mountPoint)
		return fmt.Errorf("mount point does not exist: %s", mountPoint)
	}

	// Unmount the file system
	logger.Info("attempting to unmount file system")
	if err := fuse.Unmount(mountPoint); err != nil {
		logger.Errorf("failed to unmount file system: %v", err)
		return fmt.Errorf("failed to unmount file system: %w", err)
	}
	logger.Info("successfully unmounted file system")
	// 从已挂载目录列表中移除
	mountMutex.Lock()
	delete(mountMap, mountPoint)
	mountMutex.Unlock()
	logger.Debugf("removed %s from mounted directories list", mountPoint)

	return nil
}
