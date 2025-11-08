package mount

import (
	"sync"

	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/log"
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
	MountMap = make(map[string]*dfs.DedupFS)
	// 用于保护mountMap的互斥锁
	MountMutex sync.RWMutex
)

// CleanupMounts cleans up all mounted directories
func CleanupMounts() {
	MountMutex.RLock()
	// 创建副本以避免在遍历过程中修改map
	mountPoints := make([]string, 0, len(MountMap))
	for mp := range MountMap {
		mountPoints = append(mountPoints, mp)
	}
	MountMutex.RUnlock()
	logger.Infof("cleaning mounted directories %+v", mountPoints)
	for _, mp := range mountPoints {
		logger.Infof("cleaning up mounted directory: ", mp)
		if err := Unmount(mp); err != nil {
			logger.Errorf("failed to unmount %s during cleanup: %v", mp, err)
		} else {
			logger.Infof("successfully unmounted %s during cleanup", mp)
		}
	}
}

func GetDedupFS(mountPoint string) *dfs.DedupFS {
	MountMutex.RLock()
	defer MountMutex.RUnlock()
	return MountMap[mountPoint]
}
