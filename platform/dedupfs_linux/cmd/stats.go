//go:build linux || darwin

package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/ipc"
	"github.com/mageg-x/dedupfs/common/mount"
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

	// start async stats collection
	fs.Stats()

	// try to get latest stats from kvstore
	stats, err := fs.GetStats()
	if err != nil {
		logger.Warnf("failed to retrieve statistics, using default values: %v", err)
		return map[string]interface{}{
			"mountPoint":         mountPoint,
			"id":                 fs.ID,
			"baseDir":            fs.BaseDir,
			"metaDir":            fs.MetaDir,
			"dataDir":            fs.DataDir,
			"chunkConfig":        "N/A",
			"blockConfig":        "N/A",
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
	}

	// convert structured data to map format
	result := map[string]interface{}{
		"mountPoint":         stats.MountPoint,
		"id":                 stats.ID,
		"baseDir":            stats.BaseDir,
		"metaDir":            stats.MetaDir,
		"dataDir":            stats.DataDir,
		"fileCount":          float64(stats.FileCount),
		"dirCount":           float64(stats.DirCount),
		"spaceUsed":          float64(stats.SpaceUsed),
		"realSize":           float64(stats.RealSize),
		"chunkCount":         float64(stats.ChunkCount),
		"blockCount":         float64(stats.BlockCount),
		"refChunkCount":      float64(stats.RefChunkCount),
		"deduplicationRatio": stats.DeduplicationRatio,
		"lastUpdated":        stats.LastUpdated.Format(time.RFC3339),
	}

	// process chunkconfig
	if stats.ChunkConfig != nil {
		result["chunkConfig"] = fmt.Sprintf("AvgSize: %d, Min: %d, Max: %d, Fixed: %v",
			stats.ChunkConfig.AvgSize, stats.ChunkConfig.MinSize, stats.ChunkConfig.MaxSize, stats.ChunkConfig.FixedSize)
	} else {
		result["chunkConfig"] = "N/A"
	}

	// process blockconfig
	if stats.BlockConfig != nil {
		compressStr := "false"
		encryptStr := "false"
		if stats.BlockConfig.Compress {
			compressStr = "true"
		}
		if stats.BlockConfig.Encrypt {
			encryptStr = "true"
		}
		passwordStr := ""
		if stats.BlockConfig.Password != "" {
			passwordStr = "******"
		}
		result["blockConfig"] = fmt.Sprintf("Size: %d, Compress: %s, Encrypt: %s, Password: %s",
			stats.BlockConfig.Size, compressStr, encryptStr, passwordStr)
	} else {
		result["blockConfig"] = "N/A"
	}

	return result
}

// calculateNodeStats is no longer needed, implemented in common/dfs/stats.go
// keep this function signature for compatibility

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
