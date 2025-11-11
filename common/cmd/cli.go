package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mageg-x/dedupfs/common/dfs"
	"github.com/mageg-x/dedupfs/common/ipc"
	"github.com/mageg-x/dedupfs/common/log"
)

var (
	logger = log.GetLogger("dedupfs")
)

// 探测服务是否活着
func InvokeStat(mountPoint string) bool {
	// 根据 mountPoint 生成 IPC pipe 名称
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Create IPC client and send stat command
	ipcpath := ipc.GetPath(mountPoint)
	client := ipc.NewClient(ipcpath)

	// Prepare request data
	reqData := map[string]interface{}{
		"mountPoint": mountPoint,
	}

	resp, err := client.Call(ctx, "stat", reqData, false)
	if err != nil {
		logger.Errorf("failed to send stat command: %v", err)
		return false
	}

	if !resp.Ok {
		logger.Errorf("stat command rejected: %s", resp.Msg)
		return false
	}

	logger.Info("stat request sent successfully")
	return true
}
func InvokeMount(mountPoint string, dataDir string, chunkConf, blockConf map[string]interface{}) error {
	ipcpath := ipc.GetPath(mountPoint)

	// Prepare request data
	reqData := map[string]interface{}{
		"mountPoint": mountPoint,
		"dataDir":    dataDir,
		"chunkConf":  chunkConf,
		"blockConf":  blockConf,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := ipc.NewClient(ipcpath)
	resp, err := client.Call(ctx, "mount", reqData, false)
	if err != nil {
		logger.Errorf("failed to send mount command: %v", err)
		return err
	}
	if !resp.Ok {
		logger.Errorf("mount command rejected: %s", resp.Msg)
		return fmt.Errorf("mount failed: %s", resp.Msg)
	}
	logger.Info("mount request sent successfully")
	return nil
}

func InvokeUnmount(mountPoint string) error {
	// 根据 mountPoint 生成 IPC pipe 名称
	ipcpath := ipc.GetPath(mountPoint)

	// Prepare request data
	reqData := map[string]interface{}{
		"mountPoint": mountPoint,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Create IPC client and send unmount command
	client := ipc.NewClient(ipcpath)
	resp, err := client.Call(ctx, "unmount", reqData, false)
	if err != nil {
		logger.Errorf("failed to send unmount command: %v", err)
		return err
	}

	if !resp.Ok {
		logger.Errorf("unmount command rejected: %s", resp.Msg)
		return fmt.Errorf("unmount failed: %s", resp.Msg)
	}

	logger.Info("unmount request sent successfully")
	return nil
}

func InvokeStats(mountPoint string) (*dfs.FSStats, error) {
	logger.Infof("collecting statistics for  %s mounted filesystems", mountPoint)

	// 根据 mountPoint 生成 IPC pipe 名称
	ipcpath := ipc.GetPath(mountPoint)
	reqData := map[string]interface{}{
		"mountPoint": mountPoint,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := ipc.NewClient(ipcpath)
	resp, err := client.Call(ctx, "stats", reqData, false)
	if err != nil {
		logger.Errorf("failed to send stats command: %v", err)
		return nil, err
	}
	if !resp.Ok {
		logger.Errorf("stats command rejected: %s", resp.Msg)
		return nil, fmt.Errorf("stats failed: %s", resp.Msg)
	}

	// 解析响应数据
	if len(resp.Data) == 0 {
		logger.Errorf("no data returned from stats command %#v", resp)
		return nil, fmt.Errorf("no data returned from stats command")
	}
	var stats dfs.FSStats
	if err := json.Unmarshal(resp.Data, &stats); err != nil {
		logger.Errorf("unmarshal failed: %v", err)
		return nil, fmt.Errorf("unmarshal failed: %v", err)
	}
	return &stats, nil
}

func InvokeXattr(mountPoint string, path string, attrName string) ([]byte, error) {
	ipcpath := ipc.GetPath(mountPoint)
	reqData := map[string]interface{}{
		"mountPoint": mountPoint,
		"path":       path,
		"attrName":   attrName,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := ipc.NewClient(ipcpath)
	resp, err := client.Call(ctx, "xattr", reqData, false)
	if err != nil {
		logger.Errorf("failed to send xattr command: %v", err)
		return nil, err
	}
	if !resp.Ok {
		logger.Errorf("xattr command rejected: %s", resp.Msg)
		return nil, fmt.Errorf("xattr failed: %s", resp.Msg)
	}
	return resp.Data, nil
}

func InvokeStopSvr(mountPoint string) error {
	ipcpath := ipc.GetPath(mountPoint)
	reqData := map[string]interface{}{
		"mountPoint": mountPoint,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := ipc.NewClient(ipcpath)
	resp, err := client.Call(ctx, "stop", reqData, true)
	if err != nil {
		logger.Errorf("failed to send stop command: %v", err)
		return err
	}
	if !resp.Ok {
		logger.Errorf("stop command rejected: %s", resp.Msg)
		return fmt.Errorf("stop failed: %s", resp.Msg)
	}
	logger.Info("stop request sent successfully")
	return nil
}
