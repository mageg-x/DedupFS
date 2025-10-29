package cmd

import (
	"github.com/spf13/cobra"

	"github.com/mageg-x/dedupfs/internal/mount"
)

// unmountCmd represents the unmount command
var unmountCmd = &cobra.Command{
	Use:     "unmount [mountPoint]",
	Short:   "Unmount a dedupfs filesystem",
	Aliases: []string{"umount"},
	Args:    cobra.ExactArgs(1),
	RunE:    unmountAction,
	Example: `  dedupfs unmount /data/dfs
  dedupfs umount /data/dfs`,
}

// initUnmount initializes unmount command
func initUnmount() {
	// Unmount command doesn't need additional flags
}

// unmountAction is the cobra action for unmount command
func unmountAction(cmd *cobra.Command, args []string) error {
	mountPoint := args[0]
	logger.Infof("unmounting %s", mountPoint)

	if err := mount.Unmount(mountPoint); err != nil {
		logger.Errorf("unmount failed: %v", err)
		return err
	}

	logger.Info("Unmount successful")
	return nil
}
