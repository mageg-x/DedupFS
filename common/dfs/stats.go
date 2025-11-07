package dfs

import (
	"fmt"
	"sync"
	"time"

	"github.com/mageg-x/dedupfs/common/utils"
)

// FSStats stores filesystem stats data
type FSStats struct {
	MountPoint         string       `json:"mountPoint"`
	ID                 string       `json:"id"`
	BaseDir            string       `json:"baseDir"`
	MetaDir            string       `json:"metaDir"`
	DataDir            string       `json:"dataDir"`
	ChunkConfig        *ChunkConfig `json:"chunkConfig,omitempty"`
	BlockConfig        *BlockConfig `json:"blockConfig,omitempty"`
	FileCount          int          `json:"fileCount"`
	DirCount           int          `json:"dirCount"`
	SpaceUsed          uint64       `json:"spaceUsed"`
	RealSize           int64        `json:"realSize"`
	ChunkCount         int          `json:"chunkCount"`
	BlockCount         int          `json:"blockCount"`
	RefChunkCount      int          `json:"refChunkCount"`
	DeduplicationRatio float64      `json:"deduplicationRatio"`
	LastUpdated        time.Time    `json:"lastUpdated"`
}

// Stats collects filesystem stats async and stores to kvstore
func (fs *DedupFS) Stats() {
	t := func(filesystem *DedupFS) {
		logger.Debugf("start collecting statistics for filesystem %s", filesystem.MountPoint)

		stats := &FSStats{
			MountPoint:  filesystem.MountPoint,
			ID:          filesystem.ID,
			BaseDir:     filesystem.BaseDir,
			MetaDir:     filesystem.MetaDir,
			DataDir:     filesystem.DataDir,
			LastUpdated: time.Now(),
		}

		// config info
		if filesystem.ChunkConf != nil {
			stats.ChunkConfig = filesystem.ChunkConf
		}

		if filesystem.BlockConf != nil {
			stats.BlockConfig = filesystem.BlockConf
		}

		// calculate detailed stats if root node exists
		if filesystem.RootNode != nil {
			fileCount, dirCount, totalSize, realSize, chunkCount, blockCount, refChunkCount := calculateNodeStats(filesystem)
			stats.FileCount = fileCount
			stats.DirCount = dirCount
			stats.SpaceUsed = totalSize
			stats.RealSize = realSize
			stats.ChunkCount = chunkCount
			stats.BlockCount = blockCount
			stats.RefChunkCount = refChunkCount

			// calculate dedup ratio
			if totalSize > 0 && realSize > 0 {
				stats.DeduplicationRatio = float64(totalSize) / float64(realSize)
			} else {
				stats.DeduplicationRatio = 1.0
			}
		}

		// store stats to kvstore
		if filesystem.KVStore != nil {
			if err := filesystem.KVStore.Set("stats:fs", stats); err != nil {
				logger.Errorf("failed to store statistics to kvstore: %v", err)
				return
			}
			logger.Debugf("successfully stored statistics for filesystem %s", filesystem.MountPoint)
		}
	}

	// 避免重复添加任务
	err := utils.WithTryLockKey(fs.ID, func() error {
		go t(fs)
		return nil
	})
	if err != nil {
		logger.Errorf("failed to add  static %s task to lock: %v", fs.ID, err)
	}
}

// GetStats retrieves latest stats from kvstore
func (fs *DedupFS) GetStats() (*FSStats, error) {
	if fs.KVStore == nil {
		logger.Errorf("%s kvstore is nil", fs.ID)
		return nil, fmt.Errorf("kvstore is nil")
	}

	var stats FSStats
	if err := fs.KVStore.Get("stats:fs", &stats); err != nil {
		fs.Stats()
		logger.Errorf("failed to retrieve statistics from kvstore: %v", err)
		return nil, fmt.Errorf("failed to retrieve statistics")
	}

	return &stats, nil
}

// calculateNodeStats calculates filesystem node stats
func calculateNodeStats(fs *DedupFS) (fileCount, dirCount int, totalSize uint64, realSize int64, chunkCount int, blockCount int, refChunkCount int) {
	var wg sync.WaitGroup
	var mu sync.Mutex

	fileCount = 0
	dirCount = 0
	totalSize = 0
	realSize = 0
	chunkCount = 0

	chunkMap := make(map[string]struct{})
	blockMap := make(map[string]int64)

	visited := make(map[uint64]bool)
	queue := []uint64{1} // 根节点inode
	visited[1] = true
	dirCount++

	workers := 4
	nodeChan := make(chan uint64, 100)
	resultChan := make(chan struct{}, workers)

	// start worker goroutines
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ino := range nodeChan {
				inode, err := GetINode(fs, ino)
				if err != nil || inode == nil {
					resultChan <- struct{}{}
					continue
				}

				mu.Lock()
				chunkCount += len(inode.Chunks)
				// collect block information
				for _, chunk := range inode.Chunks {
					chunkMap[chunk.Hash] = struct{}{}
					if c, err := GetChunkMeta(chunk.Hash, fs); err == nil && c != nil {
						if _, ok := blockMap[c.BlockID]; !ok {
							if b, err := ReadBlockMeta(c.BlockID, fs.DataDir); err == nil && b != nil {
								blockMap[c.BlockID] = b.Header.RealSize
							}
						}
					}
				}
				// count files and directories
				if inode.Kind == FileTypeFile || inode.Kind == FileTypeSymlink {
					fileCount++
					totalSize += inode.Size
				} else if inode.Kind == FileTypeDir && ino != 1 {
					dirCount++
				}
				mu.Unlock()

				resultChan <- struct{}{}
			}
		}()
	}

	// distribute tasks
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		nodeChan <- current
		<-resultChan

		// add children to queue
		if node, exists := fs.RootNode.Get(current); exists {
			for _, child := range node.Children {
				if child != nil && !visited[child.ID] {
					visited[child.ID] = true
					queue = append(queue, child.ID)
				}
			}
		}
	}

	close(nodeChan)
	wg.Wait()

	// calculate actual size
	for _, size := range blockMap {
		realSize += size
	}
	blockCount = len(blockMap)
	refChunkCount = chunkCount - len(chunkMap)

	return fileCount, dirCount, totalSize, realSize, chunkCount, blockCount, refChunkCount
}
