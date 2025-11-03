package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mageg-x/dedupfs/dfs"
	"github.com/mageg-x/dedupfs/internal/utils"
	"github.com/schollz/progressbar/v3"
	"github.com/spf13/cobra"
	"github.com/vmihailenco/msgpack/v5"
)

// restoreCmd represents the restore command
var restoreCmd = &cobra.Command{
	Use:   "restore [dataPath] [toPath]",
	Short: "Restore data from dedupfs to target path",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		restoreAction(cmd, args)
		return nil
	},
	Example: `  dedupfs restore data/ /restore/target`,
}

// initRestore initializes restore command
func initRestore() {
	// 可以在这里添加restore命令特有的标志
}

// restoreAction is the cobra action for restore command
func restoreAction(_ *cobra.Command, args []string) error {
	dataDir := args[0]
	toPath := args[1]

	logger.Infof("Restoring data from %s to %s", dataDir, toPath)
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		logger.Errorf("Failed to get absolute path for blocks directory: %v", err)
		return fmt.Errorf("failed to get absolute path for blocks directory: %w", err)
	}

	// 检查数据路径是否存在
	if _, err := os.Stat(dataDir); os.IsNotExist(err) {
		logger.Errorf("Data path does not exist: %s", dataDir)
		return fmt.Errorf("data path does not exist: %s", dataDir)
	}

	toPath, err = filepath.Abs(toPath)
	if err != nil {
		logger.Errorf("Failed to get absolute path for target path: %v", err)
		return fmt.Errorf("failed to get absolute path for target path: %w", err)
	}
	// 确保目标路径存在
	if err := os.MkdirAll(toPath, 0755); err != nil {
		logger.Errorf("Failed to create target directory: %v", err)
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// 执行实际的恢复逻辑
	if err := performRestore(dataDir+"/data", toPath); err != nil {
		logger.Error("Restore failed")
		logger.Errorf("Restore failed: %v", err)
		return err
	}

	logger.Info("Restore completed successfully")
	return nil
}

// performRestore 执行实际的数据恢复操作
func performRestore(dataDir, toPath string) error {

	logger.Infof("Scanning blocks directory to build chunk mapping...")
	// 扫描 dataDir
	blockFiles, err := utils.ListAllFiles(dataDir + "/blocks/")
	if err != nil {
		logger.Errorf("Failed to list all files in blocks directory: %v", err)
		return fmt.Errorf("failed to list all files in blocks directory: %w", err)
	}

	// 以 000000000 结尾的block 比较特殊 是 inode 备份 block，分离出来
	inodeBlockFiles := []string{}
	dataBlockFiles := []string{}
	for _, blockFile := range blockFiles {
		blockID := filepath.Base(blockFile)
		if strings.HasSuffix(blockID, "000000000") {
			inodeBlockFiles = append(inodeBlockFiles, blockFile)
		} else {
			dataBlockFiles = append(dataBlockFiles, blockFile)
		}
	}

	logger.Infof("Found  block files %#v  and  %#v", inodeBlockFiles, dataBlockFiles)

	// 开始读取 dataBlockFiles 中的数据， 建立 chunk -> block id 的映射
	chunkToBlock := make(map[string]string)
	for _, blockFile := range dataBlockFiles {
		blockID := filepath.Base(blockFile)
		if block, err := dfs.ReadBlockMeta(blockID, dataDir); err != nil {
			logger.Errorf("Failed to read block %s: %v", blockID, err)
			continue
		} else {
			for _, chunk := range block.Header.ChunkList {
				chunkToBlock[chunk.Hash] = blockID
			}
		}
	}
	logger.Infof("Successfully built chunk mapping, found %d chunks", len(chunkToBlock))

	// 开始读取 inodeBlockFiles 中的数据， 恢复inode 数据到文件
	inodeMap := make(map[uint64]*dfs.BackUpINode)
	fs := &dfs.DedupFS{ID: "restore", DataDir: dataDir}
	for _, blockFile := range inodeBlockFiles {
		blockID := filepath.Base(blockFile)

		if block, err := dfs.ReadBlock(blockID, fs); err != nil {
			logger.Errorf("Failed to read block %s: %v", blockID, err)
			continue
		} else {
			var inodes []dfs.BackUpINode
			if err := msgpack.Unmarshal(block.Data, &inodes); err != nil {
				logger.Errorf("Failed to unmarshal inodes from block %s: %v", blockID, err)
				continue
			}
			for _, inode := range inodes {
				inodeMap[inode.Ino] = &inode
			}
		}
	}
	logger.Infof("get %d inodes", len(inodeMap))

	// 重建目录树
	root, err := buildNodeTree(inodeMap)
	if err != nil {
		logger.Errorf("Failed to build node tree: %v", err)
		return fmt.Errorf("failed to build node tree: %w", err)
	}

	// 从根节点开始，使用栈进行非递归遍历，恢复文件
	logger.Infof("Starting non-recursive tree traversal to restore files")

	// 定义栈元素，包含inode ID和对应的目录路径
	type stackItem struct {
		inodeID uint64
		dirPath string
	}

	// 创建栈并添加根节点（ID为1）
	stack := []stackItem{{inodeID: 1, dirPath: toPath}}

	// 统计恢复的文件数量
	successCount := 0
	errorCount := 0

	// 总inode数量
	totalInodes := len(inodeMap)
	processedInodes := 0

	// 创建进度条
	bar := progressbar.Default(int64(totalInodes), "Processing inodes")
	bar.Describe("Restoring files...")

	// 计算初始栈中的节点数量，避免重复计数
	stackedNodes := make(map[uint64]bool)
	stackedNodes[1] = true

	// 非递归深度优先遍历
	for len(stack) > 0 {
		// 弹出栈顶元素
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		// 获取inode信息
		inode, exists := inodeMap[item.inodeID]
		if !exists {
			logger.Errorf("INode not found for ID %d", item.inodeID)
			errorCount++
			processedInodes++
			bar.Add(1)
			continue
		}

		// 调用restoreINode恢复文件/目录
		if err := restoreINode(item.dirPath, inode, chunkToBlock, fs); err != nil {
			logger.Errorf("Failed to restore inode %s (ID: %d): %v", inode.Name, inode.Ino, err)
			errorCount++
		} else {
			logger.Infof("Successfully restored: %s", filepath.Join(item.dirPath, inode.Name))
			successCount++
		}

		// 更新进度
		processedInodes++
		bar.Add(1)

		// 如果是目录，将其子节点加入栈中（反向添加以保持正确的处理顺序）
		if inode.Kind == dfs.FileTypeDir {
			// 获取节点的子节点
			node, exists := root.Get(item.inodeID)
			if exists {
				// 计算子节点的完整路径
				subDirPath := filepath.Join(item.dirPath, inode.Name)

				// 反向遍历子节点以保持正确的顺序
				for i := len(node.Children) - 1; i >= 0; i-- {
					childID := node.Children[i].ID
					// 避免重复添加节点
					if !stackedNodes[childID] {
						stack = append(stack, stackItem{
							inodeID: childID,
							dirPath: subDirPath,
						})
						stackedNodes[childID] = true
					}
				}
			}
		}
	}

	// 确保进度条完成
	bar.Finish()

	logger.Infof("File restoration completed: %d successful, %d failed", successCount, errorCount)
	logger.Infof("Successfully restored data from %s to %s", dataDir, toPath)
	return nil
}

func buildNodeTree(inodeMap map[uint64]*dfs.BackUpINode) (*dfs.Tree, error) {
	logger.Infof("reconstructing directory tree from inode map")

	// 使用迭代替代递归，避免栈溢出
	type stackItem struct {
		parent uint64
		child  uint64
	}

	maxIno := uint64(1)
	relation := make(map[uint64][]uint64) // 使用slice而不是map[uint64]struct{}，内存更友好
	scanCount := 0

	// 第一阶段：扫描所有inode并构建关系
	for ino, inode := range inodeMap {
		maxIno = max(maxIno, ino)
		maxIno = max(maxIno, inode.Parent)
		relation[inode.Parent] = append(relation[inode.Parent], ino)
		scanCount++
	}

	logger.Infof("scanned %d inodes, building tree structure", scanCount)

	tree := dfs.NewTree()
	visited := make(map[uint64]bool)
	var orphanNodes []uint64

	// 使用栈进行迭代构建，避免递归深度问题
	stack := []stackItem{{parent: 1, child: 1}} // 从根节点开始

	for len(stack) > 0 {
		item := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		parent := item.parent
		current := item.child

		if visited[current] {
			continue
		}
		visited[current] = true

		// 插入到树中
		if current != 1 {
			if _, err := tree.Insert(parent, current); err != nil {
				return nil, fmt.Errorf("failed to insert node %d under parent %d: %w", current, parent, err)
			}
		}

		// 将子节点加入栈
		children := relation[current]
		for i := len(children) - 1; i >= 0; i-- { // 反向遍历以保持顺序
			child := children[i]
			if !visited[child] {
				stack = append(stack, stackItem{parent: current, child: child})
			}
		}
	}

	// 正确的孤儿节点检测：所有在inodeMap中但未被visited的节点
	for node := range inodeMap {
		if !visited[node] {
			orphanNodes = append(orphanNodes, node)
			logger.Warnf("orphan node detected: inode %d", node)
		}
	}

	if len(orphanNodes) > 0 {
		logger.Warnf("found %d orphan nodes, attempting to attach to root", len(orphanNodes))
		// 尝试将孤儿节点附加到根节点
		for _, node := range orphanNodes {
			if _, err := tree.Insert(1, node); err != nil {
				logger.Errorf("failed to attach orphan node %d to root: %v", node, err)
			} else {
				logger.Infof("successfully attached orphan node %d to root", node)
			}
		}
	}

	logger.Infof("tree reconstruction completed: total nodes=%d, visited=%d, orphans=%d", len(inodeMap), len(visited), len(orphanNodes))
	return tree, nil
}

func restoreINode(inodeDir string, inode *dfs.BackUpINode, chunkToBlock map[string]string, fs *dfs.DedupFS) error {
	inodePath := filepath.Join(inodeDir, inode.Name)

	// 处理符号链接
	if inode.SymlinkTarget != nil {
		if err := os.Symlink(*inode.SymlinkTarget, inodePath); err != nil {
			logger.Errorf("Failed to create symlink %s: %v", inodePath, err)
			return fmt.Errorf("failed to create symlink: %w", err)
		}
		return nil
	}

	// 根据文件类型处理
	switch inode.Kind {
	case dfs.FileTypeDir:
		if err := os.MkdirAll(inodePath, 0755); err != nil {
			logger.Errorf("Failed to create directory %s: %v", inodePath, err)
			return fmt.Errorf("failed to create directory: %w", err)
		}
		return nil
	case dfs.FileTypeFile:
		// 普通文件，将在下面处理
	default:
		logger.Warnf("Unknown file type %d for inode %s", inode.Kind, inode.Name)
	}

	inodeData := make([]byte, 0)
	for _, chunkHash := range inode.Chunks {
		blockID, ok := chunkToBlock[chunkHash]
		if !ok {
			logger.Errorf("Failed to find block for chunk %s of inode %s", chunkHash, inode.Name)
			return fmt.Errorf("failed to find block for chunk %s of inode %s", chunkHash, inode.Name)
		}
		block, err := dfs.ReadBlock(blockID, fs)
		if err != nil || block == nil {
			logger.Errorf("Failed to read block %s for inode %s: %v", blockID, inode.Name, err)
			return fmt.Errorf("failed to read block %s: %w", blockID, err)
		}

		// 在 block 中找 chunk
		offset := 0
		var chunkData []byte
		for _, c := range block.Header.ChunkList {
			if c.Hash == chunkHash {
				end := offset + int(c.Size)
				if end > len(block.Data) {
					logger.Errorf("chunk %s out of bounds in block %s for inode %s", chunkHash, blockID, inode.Name)
					return fmt.Errorf("chunk out of bounds")
				}
				chunkData = block.Data[offset:end]
				break
			}
			offset += int(c.Size)
		}

		if chunkData == nil {
			logger.Errorf("failed to read chunk %s data from block %s for inode %s", chunkHash, blockID, inode.Name)
			return fmt.Errorf("failed to read chunk %s data from block %s for inode %s", chunkHash, blockID, inode.Name)
		}

		inodeData = append(inodeData, chunkData...)
	}

	if err := os.MkdirAll(inodeDir, 0755); err != nil {
		logger.Errorf("Failed to create directory %s for inode %s: %v", inodeDir, inode.Name, err)
		return fmt.Errorf("failed to create directory: %w", err)
	}

	if err := os.WriteFile(inodePath, inodeData, 0644); err != nil {
		logger.Errorf("Failed to write inode %s to %s: %v", inode.Name, inodePath, err)
		return fmt.Errorf("failed to write inode: %w", err)
	}
	return nil
}
