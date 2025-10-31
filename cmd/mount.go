package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/mageg-x/dedupfs/internal/ipc"
	"github.com/mageg-x/dedupfs/internal/mount"
)

var (
	// Mount command flags
	fixedSize bool
	minSize   int64
	avgSize   int64
	maxSize   int64
	blockSize int64
	compress  bool
	encrypt   bool
	password  string
)

// mountCmd represents the mount command
var mountCmd = &cobra.Command{
	Use:     "mount [mountPoint] [dataDir]",
	Short:   "Mount a dedupfs filesystem",
	Aliases: []string{"m"},
	Args:    cobra.ExactArgs(2),
	RunE:    mountAction,
	Example: `  dedupfs mount /data/dfs data/ --min-size=1048576 --compress=false
  dedupfs mount /data/dfs data/ --fixed-size --block-size=134217728`,
}

// initMount initializes mount command flags
func initMount() {
	// Mount command flags
	mountCmd.Flags().BoolVar(&fixedSize, "fixed-size", false, "Use fixed size chunks")
	mountCmd.Flags().Int64Var(&minSize, "min-size", 1024*1024, "Minimum chunk size in bytes")
	mountCmd.Flags().Int64Var(&avgSize, "avg-size", 2*1024*1024, "Average chunk size in bytes")
	mountCmd.Flags().Int64Var(&maxSize, "max-size", 4*1024*1024, "Maximum chunk size in bytes")
	mountCmd.Flags().Int64Var(&blockSize, "block-size", 64*1024*1024, "Block size in bytes")
	mountCmd.Flags().BoolVar(&compress, "compress", true, "Enable compression")
	mountCmd.Flags().BoolVar(&encrypt, "encrypt", false, "Enable encryption")
	mountCmd.Flags().StringVar(&password, "password", "", "Password for encryption")
}

// mountAction is the cobra action for mount command
func mountAction(cmd *cobra.Command, args []string) error {
	mountPoint := args[0]
	dataDir := args[1]

	logger.Infof("Sending mount request: %s to %s", dataDir, mountPoint)
	// 调试信息
	logger.Debugf("Arguments: mountPoint=%s, dataDir=%s", mountPoint, dataDir)
	logger.Debugf("Flags: fixed-size=%v, min-size=%d, avg-size=%d, max-size=%d, block-size=%d, compress=%v, encrypt=%v, password_set=%v",
		fixedSize, minSize, avgSize, maxSize, blockSize, compress, encrypt, password != "")

	// Prepare request data
	reqData := map[string]interface{}{
		"mountPoint": mountPoint,
		"dataDir":    dataDir,
		"chunkConf": map[string]interface{}{
			"fixedSize": fixedSize,
			"minSize":   minSize,
			"avgSize":   avgSize,
			"maxSize":   maxSize,
		},
		"blockConf": map[string]interface{}{
			"size":     blockSize,
			"compress": compress,
			"encrypt":  encrypt,
			"password": password,
		},
	}

	// Create IPC client and send mount command
	client := ipc.NewClient(SocketPath)
	resp, err := client.Call("mount", reqData)
	if err != nil {
		logger.Errorf("Failed to send mount command: %v", err)
		return err
	}

	if !resp.Ok {
		logger.Errorf("Mount command rejected: %s", resp.Msg)
		return fmt.Errorf("mount failed: %s", resp.Msg)
	}

	logger.Info("Mount request sent successfully")
	return nil
}

// handleMountCommand handles mount requests
func HandleMountCommand(ctx context.Context, req *ipc.Request) *ipc.Response {
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
