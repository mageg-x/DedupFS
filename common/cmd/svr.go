package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/mageg-x/dedupfs/common/ipc"
	"github.com/mageg-x/dedupfs/common/mount"
)

func HandleStatCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
	logger.Info("received stat request")
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

	dfs := mount.GetDedupFS(mountPoint)
	if dfs == nil {
		logger.Errorf("mount point %s not found", mountPoint)
		return &ipc.Response{Ok: false, Msg: "mount dfs  not found"}
	}

	return &ipc.Response{Ok: true, Msg: "success", Data: []byte("ok")}
}

// handleUnmountCommand handles unmount requests
func HandleUnmountCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
	logger.Info("received unmount request")
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
	logger.Infof("handling unmount request for: %s", mountPoint)
	if err := mount.Unmount(mountPoint); err != nil {
		logger.Errorf("unmount failed: %v", err)
		time.AfterFunc(2*time.Second, func() {
			// 强制退出
			logger.Errorf("force exiting...")
			os.Exit(1)
		})
		return &ipc.Response{Ok: false, Msg: fmt.Sprintf("Unmount failed: %v", err)}
	} else {
		logger.Info("unmount successful")
		return &ipc.Response{Ok: true, Msg: "unmount operation successful"}
	}
}

func HandleStatsCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
	logger.Infof("Collecting statistics for a mounted filesystems")

	// Parse request data
	data, ok := req.Data.(map[string]interface{})
	if !ok {
		logger.Errorf("invalid request data format")
		return &ipc.Response{Ok: false, Msg: "invalid request data format"}
	}

	// Extract mount point
	mountPoint, ok := data["mountPoint"].(string)
	if !ok {
		logger.Errorf("missing required parameter: mountPoint")
		return &ipc.Response{Ok: false, Msg: "missing required parameter: mountPoint"}
	}

	dfs := mount.GetDedupFS(mountPoint)
	if dfs == nil {
		logger.Errorf("mount point %s not found", mountPoint)
		return &ipc.Response{Ok: false, Msg: "mount dfs  not found"}
	}

	dfs.Stats()

	if stats, err := dfs.GetStats(); err != nil {
		logger.Errorf("Failed to collect statistics: %v", err)
		return &ipc.Response{Ok: false, Msg: fmt.Sprintf("Failed to collect statistics: %v", err)}
	} else {
		logger.Infof("statistics collected successfully")
		if data, err := json.Marshal(stats); err != nil {
			logger.Errorf("Failed to marshal statistics: %v", err)
			return &ipc.Response{Ok: false, Msg: fmt.Sprintf("Failed to marshal statistics: %v", err)}
		} else {
			return &ipc.Response{Ok: true, Msg: "success", Data: data}
		}
	}
}
