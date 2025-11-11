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

func HandleMountCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
	logger.Info("received mount request")
	// Parse request data
	data, ok := req.Data.(map[string]interface{})
	if !ok {
		return &ipc.Response{Ok: false, Msg: "invalid request data format"}
	}

	// Extract required parameters
	mountPoint, ok1 := data["mountPoint"].(string)
	dataDir, ok2 := data["dataDir"].(string)
	if !ok1 || !ok2 {
		return &ipc.Response{Ok: false, Msg: "missing required parameters: mountPoint and dataDir"}
	}

	// Extract options
	options := &mount.MountOptions{}
	if chunkConf, ok := data["chunkConf"].(map[string]interface{}); ok {
		options.ChunkConf = &mount.ChunkConfig{}
		if fixedSize, ok := chunkConf["fixedSize"].(bool); ok {
			options.ChunkConf.FixedSize = fixedSize
		}
		if minSize, ok := chunkConf["minSize"].(float64); ok {
			options.ChunkConf.MinSize = int64(minSize)
		}
		if avgSize, ok := chunkConf["avgSize"].(float64); ok {
			options.ChunkConf.AvgSize = int64(avgSize)
		}
		if maxSize, ok := chunkConf["maxSize"].(float64); ok {
			options.ChunkConf.MaxSize = int64(maxSize)
		}
	}

	if blockConf, ok := data["blockConf"].(map[string]interface{}); ok {
		options.BlockConf = &mount.BlockConfig{}
		if size, ok := blockConf["size"].(float64); ok {
			options.BlockConf.Size = int64(size)
		}
		if compress, ok := blockConf["compress"].(bool); ok {
			options.BlockConf.Compress = compress
		}
		if encrypt, ok := blockConf["encrypt"].(bool); ok {
			options.BlockConf.Encrypt = encrypt
		}
		if password, ok := blockConf["password"].(string); ok {
			options.BlockConf.Password = password
		}
	}

	// Execute mount in a goroutine to avoid blocking the server
	go func() {
		logger.Infof("Handling mount request: %s to %s", dataDir, mountPoint)
		if err := mount.Mount(mountPoint, dataDir, options); err != nil {
			logger.Errorf("Mount failed: %v", err)
		} else {
			logger.Info("Mount successful")
		}
	}()

	return &ipc.Response{Ok: true, Msg: "mount operation started"}
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
func HandleXAttrCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
	logger.Info("received xattr request")

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

	path, ok := data["path"].(string)
	if !ok {
		logger.Errorf("missing required parameter: path")
		return &ipc.Response{Ok: false, Msg: "missing required parameter: path"}
	}

	attrName, ok := data["attrName"].(string)
	if !ok {
		logger.Errorf("missing required parameter: attrName")
		return &ipc.Response{Ok: false, Msg: "missing required parameter: attrName"}
	}

	fs := mount.GetDedupFS(mountPoint)
	if fs == nil {
		logger.Errorf("mount point %s not found", mountPoint)
		return &ipc.Response{Ok: false, Msg: "mount dfs  not found"}
	}

	ec, val := fs.Xattr(path, attrName)
	if ec != 0 {
		logger.Errorf("Failed to get %s xattr: %s", path, attrName)
		return &ipc.Response{Ok: false, Msg: fmt.Sprintf("Failed to get xattr: %s", attrName)}
	}
	return &ipc.Response{Ok: true, Msg: "success", Data: val}
}
