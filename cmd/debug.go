package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var debugCmd = &cobra.Command{
	Use:   "debug [mountpoint] block|inode [block_id|inode_name]",
	Short: "Debug block or inode",
	Long:  `Debug block or inode in the dedupfs filesystem.`,
	Args:  cobra.MinimumNArgs(3),
	RunE: func(cmd *cobra.Command, args []string) error {
		mountPoint := args[0]
		debugType := args[1]
		id := args[2]

		switch debugType {
		case "block":
			debugBlockAction(mountPoint, id)
			return nil
		case "inode":
			debugINodeAction(mountPoint, id)
			return nil
		default:
			return fmt.Errorf("invalid debug type: %s, must be 'block' or 'inode'", debugType)
		}
	},
}

// initDebug initializes the debug command
func initDebug() {}

// formatSize formats file size with commas for readability
func formatSize(n int64) string {
	if n < 0 {
		return "-" + formatSize(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result string
	for i := len(s); i > 0; i -= 3 {
		if i-3 > 0 {
			result = "," + s[i-3:i] + result
		} else {
			result = s[:i] + result
		}
	}
	return result
}
