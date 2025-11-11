package console

import (
	"fmt"
	"strings"

	"github.com/mageg-x/dedupfs/common/dfs"
)

func RenderStats(mountPoint string, stats *dfs.FSStats) error {
	// 固定终端宽度，Windows下不支持动态获取
	contentWidth := 116 // 120 - 4

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

	keyWidth := contentWidth / 3
	if keyWidth > 30 {
		keyWidth = 30
	}
	if keyWidth < 20 {
		keyWidth = 20
	}

	// 格式化字节数的内部函数
	formatBytes := func(bytes float64) string {
		units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
		size := bytes
		unitIndex := 0
		for size >= 1024 && unitIndex < len(units)-1 {
			size /= 1024
			unitIndex++
		}
		return fmt.Sprintf("%.2f %s", size, units[unitIndex])
	}

	// 处理ChunkConfig和BlockConfig的格式化
	formatChunkConfig := func(config *dfs.ChunkConfig) string {
		if config == nil {
			return "N/A"
		}
		if config.FixedSize {
			return fmt.Sprintf("Fixed: %s", formatBytes(float64(config.AvgSize)))
		}
		return fmt.Sprintf("min: %s, avg: %s, max: %s",
			formatBytes(float64(config.MinSize)),
			formatBytes(float64(config.AvgSize)),
			formatBytes(float64(config.MaxSize)))
	}

	formatBlockConfig := func(config *dfs.BlockConfig) string {
		if config == nil {
			return "N/A"
		}
		compress := "no"
		if config.Compress {
			compress = "yes"
		}
		encrypt := "no"
		passwordSet := ""
		if config.Encrypt {
			encrypt = "yes"
			if config.Password != "" {
				passwordSet = "yes (masked)"
			}
		}
		return fmt.Sprintf("Size: %s, Compress: %s, Encrypt: %s, Password: %s",
			formatBytes(float64(config.Size)), compress, encrypt, passwordSet)
	}

	// 输出挂载点信息
	fmt.Printf("\n  \x1b[1;96mMount Point: %s\x1b[0m\n", mountPoint)
	fmt.Printf("  %s\n", strings.Repeat("─", contentWidth))

	// 定义字段列表
	fields := []struct {
		Name  string
		Value string
	}{
		{"Filesystem ID", stats.ID},
		{"Base Directory", stats.BaseDir},
		{"Metadata Directory", stats.MetaDir},
		{"Data Directory", stats.DataDir},
		{"Chunk Config", formatChunkConfig(stats.ChunkConfig)},
		{"Block Config", formatBlockConfig(stats.BlockConfig)},
		{"Files", fmt.Sprintf("%d", stats.FileCount)},
		{"Directories", fmt.Sprintf("%d", stats.DirCount)},
		{"Space Used", formatBytes(float64(stats.SpaceUsed))},
		{"Real Size", formatBytes(float64(stats.RealSize))},
		{"Total Chunks", fmt.Sprintf("%d", stats.ChunkCount)},
		{"Blocks", fmt.Sprintf("%d", stats.BlockCount)},
		{"Referenced Chunks", fmt.Sprintf("%d", stats.RefChunkCount)},
	}

	// 计算去重率
	deduplicationRatio := "0.00 X"
	if stats.RealSize > 0 {
		ratio := float64(stats.SpaceUsed) / float64(stats.RealSize)
		deduplicationRatio = fmt.Sprintf("%.2f X", ratio)
	}
	fields = append(fields,
		struct {
			Name  string
			Value string
		}{"Compression Ratio", deduplicationRatio},
	)

	// 添加最后更新时间
	lastUpdated := "unknown"
	if !stats.LastUpdated.IsZero() {
		lastUpdated = stats.LastUpdated.Format("2006-01-02 15:04:05")
	}
	fields = append(fields,
		struct {
			Name  string
			Value string
		}{"Last Updated", lastUpdated},
	)

	// 输出所有字段
	for _, f := range fields {
		fmt.Printf("  %-*s : %s\n", keyWidth, f.Name, f.Value)
	}
	return nil
}
