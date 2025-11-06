//go:build linux || darwin

package mount

import (
	"fmt"
	"os"
	"path/filepath"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/utils"
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

	err = utils.WithLock(&MountMutex, func() error {
		if _, ok := MountMap[mountPoint]; ok {
			return fmt.Errorf("mount point %s is already mounted", mountPoint)
		}
		return nil
	})
	if err != nil {
		logger.Errorf("mount point %s is already mounted", mountPoint)
		return err
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
	utils.WithLock(&MountMutex, func() error {
		MountMap[mountPoint] = fsys
		return nil
	})

	logger.Debugf("added %s to mounted directories list", mountPoint)

	defer func() {
		c.Close()
		Unmount(mountPoint)
	}()

	logger.Info("starting to serve file system")
	if err := fs.Serve(c, fsys); err != nil {
		logger.Errorf("failed to serve file system: %v", err)
		return fmt.Errorf("failed to serve file system: %w", err)
	}

	return nil
}

// Unmount unmounts the file system from the specified mount point
func Unmount(mountPoint string) error {
	logger.Debugf("starting unmount process for: %s", mountPoint)
	// 从已挂载目录列表中移除
	MountMutex.Lock()
	defer MountMutex.Unlock()

	fs, ok := MountMap[mountPoint]
	if !ok {
		logger.Errorf("mount point not found: %s", mountPoint)
		return fmt.Errorf("mount point not found: %s", mountPoint)
	}

	// Unmount the file system
	logger.Infof("attempting to unmount for %s", mountPoint)
	if err := fuse.Unmount(mountPoint); err != nil {
		logger.Errorf("failed to unmount file %s: %v", mountPoint, err)
		return fmt.Errorf("failed to unmount file system: %w", err)
	}

	logger.Infof("successfully unmounted file %s", mountPoint)
	if fs != nil {
		dfs.ClearINodeCache(fs)
		dfs.ClearChunkCache(fs)
		dfs.ClearBlockCache(fs)

		if fs.KVStore != nil {
			fs.KVStore.Close()
			fs.KVStore = nil
		}
		if fs.Timer != nil {
			fs.Timer.Stop()
			fs.Timer = nil
		}
	}

	delete(MountMap, mountPoint)
	logger.Debugf("removed %s from mounted directories list", mountPoint)

	return nil
}
