package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mageg-x/dedupfs/dfs"
	"github.com/mageg-x/dedupfs/internal/ipc"
	"github.com/mageg-x/dedupfs/internal/mount"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var statsCmd = &cobra.Command{
	Use:     "stats",
	Short:   "Show all mounted filesystems statistics",
	Args:    cobra.ExactArgs(0),
	RunE:    statsAction,
	Example: `  dedupfs stats`,
}

func initStats() {}

func formatBytes(bytes float64) string {
	if bytes < 1024 {
		return fmt.Sprintf("%.0f B", bytes)
	} else if bytes < 1024*1024 {
		return fmt.Sprintf("%.2f KB", bytes/1024)
	} else if bytes < 1024*1024*1024 {
		return fmt.Sprintf("%.2f MB", bytes/(1024*1024))
	} else {
		return fmt.Sprintf("%.2f GB", bytes/(1024*1024*1024))
	}
}

func statsAction(cmd *cobra.Command, args []string) error {
	logger.Infof("Collecting statistics for all mounted filesystems")

	client := ipc.NewClient(SocketPath)
	resp, err := client.Call("stats", nil)
	if err != nil {
		logger.Errorf("Failed to send stats command: %v", err)
		return err
	}
	if !resp.Ok {
		logger.Errorf("Stats command rejected: %s", resp.Msg)
		return fmt.Errorf("stats failed: %s", resp.Msg)
	}

	// Detect terminal width, fallback to 120
	termWidth := 120
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w >= 60 {
		termWidth = w
	}
	if termWidth > 120 {
		termWidth = 120
	}

	// Reduce effective width by 4 (2 spaces left + 2 right padding)
	contentWidth := termWidth - 4
	if contentWidth < 60 {
		contentWidth = 60
	}

	// Header with padding
	headerText := "Deduplication Filesystem Statistics"
	border := "┌" + strings.Repeat("─", contentWidth) + "┐"
	fmt.Printf(" %s\n", border)
	fmt.Printf(" │%*s%*s│\n",
		(contentWidth+len(headerText))/2,
		headerText,
		(contentWidth+len(headerText)+1)/2-len(headerText),
		"",
	)
	fmt.Printf(" %s\n", "└"+strings.Repeat("─", contentWidth)+"┘")

	if data, ok := resp.Data.(map[string]interface{}); ok {
		if len(data) == 0 {
			fmt.Println("\n  No filesystems currently mounted")
			return nil
		}

		keyWidth := contentWidth / 3
		if keyWidth > 30 {
			keyWidth = 30
		}
		if keyWidth < 20 {
			keyWidth = 20
		}

		fields := []struct {
			Key  string
			Name string
		}{
			{"id", "Filesystem ID"},
			{"baseDir", "Base Directory"},
			{"metaDir", "Metadata Directory"},
			{"dataDir", "Data Directory"},
			{"chunkConfig", "Chunk Config"},
			{"blockConfig", "Block Config"},
			{"fileCount", "Files"},
			{"dirCount", "Directories"},
			{"spaceUsed", "Space Used"},
			{"realSize", "Real Size"},
			{"chunkCount", "Total Chunks"},
			{"blockCount", "Blocks"},
			{"refChunkCount", "Referenced Chunks"},
			{"deduplicationRatio", "Compression Ratio"},
			{"lastUpdated", "Last Updated"},
		}

		for mp, fsData := range data {
			if fs, ok := fsData.(map[string]interface{}); ok {
				fmt.Printf("\n  \x1b[1;96mMount Point: %s\x1b[0m\n", mp)
				fmt.Printf("  %s\n", strings.Repeat("─", contentWidth))

				for _, f := range fields {
					var value string
					switch f.Key {
					case "fileCount", "dirCount", "chunkCount", "blockCount", "refChunkCount":
						value = fmt.Sprintf("%.0f", getFloatValue(fs, f.Key, 0))
					case "spaceUsed", "realSize":
						value = formatBytes(getFloatValue(fs, f.Key, 0))
					case "deduplicationRatio":
						value = fmt.Sprintf("%.2f X", getFloatValue(fs, f.Key, 0))
					default:
						value = getStringValue(fs, f.Key, "unknown")
					}
					fmt.Printf("  %-*s : %s\n", keyWidth, f.Name, value)
				}
			}
		}
	} else {
		fmt.Println("\n  No statistics available")
	}

	logger.Debug("Stats command completed")
	return nil
}

func getStringValue(m map[string]interface{}, key, defaultValue string) string {
	if val, ok := m[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return defaultValue
}

func getFloatValue(m map[string]interface{}, key string, defaultValue float64) float64 {
	if val, ok := m[key]; ok {
		if f, ok := val.(float64); ok {
			return f
		}
	}
	return defaultValue
}

func collectFilesystemStats(mountPoint string, fs *dfs.DedupFS) map[string]interface{} {
	if fs == nil {
		return map[string]interface{}{
			"mountPoint": mountPoint,
		}
	}

	chunkConfig := "N/A"
	blockConfig := "N/A"
	if fs.ChunkConf != nil {
		chunkConfig = fmt.Sprintf("AvgSize: %d, Min: %d, Max: %d",
			fs.ChunkConf.AvgSize, fs.ChunkConf.MinSize, fs.ChunkConf.MaxSize)
	}
	if fs.BlockConf != nil {
		compressStr := "false"
		encryptStr := "false"
		if fs.BlockConf.Compress {
			compressStr = "true"
		}
		if fs.BlockConf.Encrypt {
			encryptStr = "true"
		}
		passwordStr := ""
		if fs.BlockConf.Password != "" {
			passwordStr = "******"
		}
		blockConfig = fmt.Sprintf("Size: %d, Compress: %s, Encrypt: %s, Password: %s",
			fs.BlockConf.Size, compressStr, encryptStr, passwordStr)
	}

	stats := map[string]interface{}{
		"mountPoint":         mountPoint,
		"id":                 fs.ID,
		"baseDir":            fs.BaseDir,
		"metaDir":            fs.MetaDir,
		"dataDir":            fs.DataDir,
		"chunkConfig":        chunkConfig,
		"blockConfig":        blockConfig,
		"fileCount":          float64(0),
		"dirCount":           float64(0),
		"spaceUsed":          float64(0),
		"realSize":           float64(0),
		"chunkCount":         float64(0),
		"blockCount":         float64(0),
		"refChunkCount":      float64(0),
		"deduplicationRatio": 1.0,
		"lastUpdated":        time.Now().Format(time.RFC3339),
	}

	if fs.RootNode != nil {
		fileCount, dirCount, totalSize, realSize, chunkCount, blockCount, refChunkCount := calculateNodeStats(fs)
		stats["fileCount"] = float64(fileCount)
		stats["dirCount"] = float64(dirCount)
		stats["spaceUsed"] = float64(totalSize)
		stats["realSize"] = float64(realSize)
		stats["chunkCount"] = float64(chunkCount)
		stats["blockCount"] = float64(blockCount)
		stats["refChunkCount"] = float64(refChunkCount)
		// 计算真实的重复数据删除率
		var dedupRatio float64
		if totalSize > 0 && realSize > 0 {
			dedupRatio = (float64(totalSize)) / float64(realSize)
		} else {
			dedupRatio = 1.0
		}
		stats["deduplicationRatio"] = dedupRatio
	}

	return stats
}

func calculateNodeStats(fs *dfs.DedupFS) (fileCount, dirCount int, totalSize uint64, realSize int64, chunkCount int, blockCount int, refChunkCount int) {
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
	queue := []uint64{1}
	visited[1] = true
	dirCount++

	workers := 4
	nodeChan := make(chan uint64, 100)
	resultChan := make(chan struct{}, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ino := range nodeChan {
				inode, err := dfs.GetINode(fs, ino)
				if err != nil || inode == nil {
					resultChan <- struct{}{}
					logger.Errorf("failed to get inode %d: %v", ino, err)
					continue
				}

				mu.Lock()
				chunkCount += len(inode.Chunks)
				for _, chunk := range inode.Chunks {
					chunkMap[chunk.Hash] = struct{}{}
					if c, err := dfs.GetChunkMeta(chunk.Hash, fs); err == nil && c != nil {
						if _, ok := blockMap[c.BlockID]; !ok {
							if b, err := dfs.ReadBlockMeta(c.BlockID, fs.DataDir); err == nil && b != nil {
								blockMap[c.BlockID] = b.Header.RealSize
							} else {
								logger.Errorf("failed to read block %s: %v", c.BlockID, err)
							}
						}
					} else {
						logger.Errorf("failed to get chunk %s meta: %v", chunk.Hash, err)
					}
				}
				if inode.Kind == dfs.FileTypeFile || inode.Kind == dfs.FileTypeSymlink {
					fileCount++
					totalSize += inode.Size
				} else if inode.Kind == dfs.FileTypeDir && ino != 1 {
					dirCount++
				}
				mu.Unlock()

				resultChan <- struct{}{}
			}
		}()
	}

	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		nodeChan <- current
		<-resultChan

		if node, exists := fs.RootNode.Get(current); exists {
			for _, child := range node.Children {
				if !visited[child.ID] {
					visited[child.ID] = true
					queue = append(queue, child.ID)
				}
			}
		}
	}

	close(nodeChan)
	wg.Wait()

	for _, size := range blockMap {
		realSize += size
	}
	blockCount = len(blockMap)
	refChunkCount = chunkCount - len(chunkMap)

	return fileCount, dirCount, totalSize, realSize, chunkCount, blockCount, refChunkCount
}

func HandleStatsCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
	logger.Infof("Collecting statistics for all mounted filesystems")

	filesystems := make(map[string]map[string]interface{})

	for mp, fs := range mount.MountMap {
		if mp == "" || fs == nil {
			continue
		}
		fsStats := collectFilesystemStats(mp, fs)
		filesystems[mp] = fsStats
	}

	logger.Debugf("get filesystems %#v", filesystems)

	resp := &ipc.Response{
		Ok:  true,
		Msg: "success",
	}
	if len(filesystems) > 0 {
		resp.Data = filesystems
	} else {
		resp.Msg = "no filesystems mounted"
	}
	return resp
}
