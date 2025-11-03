package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mageg-x/dedupfs/common/ipc"
	"github.com/mageg-x/dedupfs/common/mount"
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
	logger.Infof("Sending unmount request for %s", mountPoint)

	// Prepare request data
	reqData := map[string]interface{}{
		"mountPoint": mountPoint,
	}

	// Create IPC client and send unmount command
	client := ipc.NewClient(SocketPath)
	resp, err := client.Call("unmount", reqData)
	if err != nil {
		logger.Errorf("Failed to send unmount command: %v", err)
		return err
	}

	if !resp.Ok {
		logger.Errorf("Unmount command rejected: %s", resp.Msg)
		return fmt.Errorf("unmount failed: %s", resp.Msg)
	}

	logger.Info("Unmount request sent successfully")
	return nil
}

// handleUnmountCommand handles unmount requests
func HandleUnmountCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
	// Parse request data
	data, ok := req.Data.(map[string]interface{})
	if !ok {
		return &ipc.Response{Ok: false, Msg: "invalid request data format"}
	}

	// Extract mount point
	mountPoint, ok := data["mountPoint"].(string)
	if !ok {
		return &ipc.Response{Ok: false, Msg: "missing required parameter: mountPoint"}
	}

	// Execute unmount in a goroutine
	go func() {
		logger.Infof("Handling unmount request for: %s", mountPoint)
		if err := mount.Unmount(mountPoint); err != nil {
			logger.Errorf("Unmount failed: %v", err)
		} else {
			logger.Info("Unmount successful")
		}
	}()

	return &ipc.Response{Ok: true, Msg: "unmount operation started"}
}
