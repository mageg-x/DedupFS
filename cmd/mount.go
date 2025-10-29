package cmd

import (
	"github.com/spf13/cobra"

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
	mountCmd.Flags().Int64Var(&minSize, "min-size", 4096, "Minimum chunk size in bytes")
	mountCmd.Flags().Int64Var(&avgSize, "avg-size", 8*1024, "Average chunk size in bytes")
	mountCmd.Flags().Int64Var(&maxSize, "max-size", 16*1024, "Maximum chunk size in bytes")
	mountCmd.Flags().Int64Var(&blockSize, "block-size", 64*1024*1024, "Block size in bytes")
	mountCmd.Flags().BoolVar(&compress, "compress", true, "Enable compression")
	mountCmd.Flags().BoolVar(&encrypt, "encrypt", false, "Enable encryption")
	mountCmd.Flags().StringVar(&password, "password", "", "Password for encryption")
}

// mountAction is the cobra action for mount command
func mountAction(cmd *cobra.Command, args []string) error {
	mountPoint := args[0]
	dataDir := args[1]

	logger.Infof("mounting %s to %s", dataDir, mountPoint)

	// 调试信息
	logger.Debugf("Arguments: mountPoint=%s, dataDir=%s", mountPoint, dataDir)
	logger.Debugf("Flags: fixed-size=%v, min-size=%d, avg-size=%d, max-size=%d, block-size=%d, compress=%v, encrypt=%v, password_set=%v",
		fixedSize, minSize, avgSize, maxSize, blockSize, compress, encrypt, password != "")

	opts := &mount.MountOptions{
		ChunkConf: &mount.ChunkConfig{
			FixedSize: fixedSize,
			MinSize:   minSize,
			AvgSize:   avgSize,
			MaxSize:   maxSize,
		},
		BlockConf: &mount.BlockConfig{
			Size:     blockSize,
			Compress: compress,
			Encrypt:  encrypt,
			Password: password,
		},
	}

	logger.Infof("Chunk config: fixed=%v, min=%d, avg=%d, max=%d",
		opts.ChunkConf.FixedSize, opts.ChunkConf.MinSize, opts.ChunkConf.AvgSize, opts.ChunkConf.MaxSize)
	logger.Infof("Block config: size=%d, compress=%v, encrypt=%v",
		opts.BlockConf.Size, opts.BlockConf.Compress, opts.BlockConf.Encrypt)

	if err := mount.Mount(mountPoint, dataDir, opts); err != nil {
		logger.Errorf("mount failed: %v", err)
		return err
	}

	logger.Info("Mount successful")
	return nil
}
