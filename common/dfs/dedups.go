package dfs

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mageg-x/dedupfs/common/kvstore"
	"github.com/mageg-x/dedupfs/common/log"
	"github.com/mageg-x/dedupfs/common/utils"
	"github.com/vmihailenco/msgpack/v5"
)

var (
	// 获取logger实例用于输出日志，与mount包保持一致的名称
	logger = log.GetLogger("dedupfs")
)

// 权限掩码枚举定义
type PermissionMask uint32

const (
	// 读权限
	ReadPermission PermissionMask = 04
	// 写权限
	WritePermission PermissionMask = 02
	// 执行权限
	ExecPermission PermissionMask = 01
	// 所有权限
	AllPermission PermissionMask = 07
	//读写访问权限
	ReadWritePermission PermissionMask = ReadPermission | WritePermission
)

// 切片配置
type ChunkConfig struct {
	FixedSize bool  `json:"fixedSize"` // 固定长度切片
	MinSize   int64 `json:"minSize"`   // 最小切片大小
	AvgSize   int64 `json:"avgSize"`   // 平均切片大小
	MaxSize   int64 `json:"maxSize"`   // 最大切片大小
}

// 块配置
type BlockConfig struct {
	Size     int64  `json:"size"`     // 块大小
	Compress bool   `json:"compress"` // 是否压缩
	Encrypt  bool   `json:"encrypt"`  // 是否加密
	Password string `json:"password"` // 密码（可选字段，为空时在JSON中省略）
}

type DedupFS struct {
	MountPoint string
	ID         string
	BaseDir    string
	MetaDir    string
	DataDir    string
	NextNodeID atomic.Int64
	NextHandle atomic.Int64
	ChunkConf  *ChunkConfig
	BlockConf  *BlockConfig
	RootNode   *Tree
	KVStore    kvstore.KVStore
	Dirty      bool
	Timer      *time.Timer
	Host       any // 只有windows下使用
	openmap    map[uint64]uint64
	mutex      sync.RWMutex
}

type BackUpINode struct {
	Ino           uint64   `msgpack:"i"`
	Parent        uint64   `msgpack:"p"`
	Kind          FileType `msgpack:"k"`
	Name          string   `msgpack:"n"`
	Chunks        []string `msgpack:"c"`
	SymlinkTarget *string  `msgpack:"l"`
}

// BuildNodeTree 通过扫描kvstore重建目录树
func (fs *DedupFS) BuildNodeTree() (*Tree, error) {
	logger.Infof("reconstructing directory tree from kvstore")

	// 使用迭代替代递归，避免栈溢出
	type stackItem struct {
		parent uint64
		child  uint64
	}

	maxIno := uint64(1)
	relation := make(map[uint64][]uint64) // 使用slice而不是map[uint64]struct{}，内存更友好
	nodeSet := make(map[uint64]string)    // 记录所有存在的节点

	// 第一阶段：扫描所有inode并构建关系
	prefix := "inode:"
	startKey := ""
	scanCount := 0

	for {
		keys, nextKey, err := fs.KVStore.Scan(prefix, startKey, 10000)
		if err != nil {
			return nil, fmt.Errorf("failed to scan keys with prefix %s: %w", prefix, err)
		}

		for _, key := range keys {
			logger.Debugf("scanned key: %s", key)
			var ino uint64
			if _, err := fmt.Sscanf(key, prefix+"%d", &ino); err != nil {
				logger.Warnf("failed to parse key %s: %v", key, err)
				continue
			}
			maxIno = max(maxIno, ino)

			inode, err := GetINode(fs, ino)
			if err != nil {
				logger.Warnf("failed to get inode %d: %v", ino, err)
				continue
			}

			if inode != nil {
				maxIno = max(maxIno, inode.Parent)
				relation[inode.Parent] = append(relation[inode.Parent], ino)
				nodeSet[ino] = inode.Name
				scanCount++
			}
		}

		if nextKey == "" {
			break
		}
		startKey = nextKey
	}

	logger.Debugf("scanned %d inodes, building tree structure", scanCount)

	tree := NewTree()
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
			if _, err := tree.Insert(parent, current, nodeSet[current]); err != nil {
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

	// 正确的孤儿节点检测：所有在nodeSet中但未被visited的节点
	for node := range nodeSet {
		if !visited[node] {
			orphanNodes = append(orphanNodes, node)
			logger.Warnf("orphan node detected: inode %d", node)
		}
	}

	if len(orphanNodes) > 0 {
		logger.Warnf("found %d orphan nodes, attempting to attach to root", len(orphanNodes))
	}

	fs.RootNode = tree
	fs.NextNodeID.Store(int64(maxIno))
	logger.Debugf("tree reconstruction completed: total nodes=%d, visited=%d, orphans=%d", len(nodeSet), len(visited), len(orphanNodes))

	return tree, nil
}

// CreateNode creates a new inode with the specified type
func (fs *DedupFS) CreateNode(parentID uint64, uid, gid uint32, name string, kind FileType, mode os.FileMode, symlinkTarget *string) (*INode, error) {
	logger.Debugf("creating %s node: %s uid: %d gid: %d  with mode: %d", kind, name, uid, gid, mode)

	// Check if node already exists
	parentNode, exists := fs.RootNode.Get(parentID)
	if !exists || parentNode == nil {
		logger.Errorf("parent node not found: %d", parentID)
		return nil, fmt.Errorf("parent node not found")
	}

	for _, child := range parentNode.Children {
		if child != nil {
			if inode, _ := GetINode(fs, child.ID); inode != nil && inode.Name == name {
				logger.Errorf("node already exists: %s", name)
				return nil, fmt.Errorf("node already exists: %s", name)
			}
		}
	}

	// Create new inode
	ino := uint64(fs.NextNodeID.Add(1))

	inode := CreateINode(ino, parentID, uid, gid, kind, name, uint32(mode))
	inode.Uid = uid
	inode.Gid = gid
	inode.SymlinkTarget = symlinkTarget

	// For directories, set correct mode and nlink
	if kind == FileTypeDir {
		inode.Mode |= 0111 // Ensure executable bits are set for directories
	}

	// Insert into tree and save
	if _, err := fs.RootNode.Insert(parentID, ino, name); err != nil {
		logger.Errorf("failed to insert node into tree: %v", err)
		return nil, err
	}

	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("failed to save inode: %v", err)
		return nil, err
	}

	logger.Debugf("created %s node: %s with ino %d", kind, name, ino)
	return inode, nil
}

func (fs *DedupFS) BackupINode(d time.Duration) error {
	logger.Debugf("backing up filesystem node meta")

	go func() {
		defer func() {
			fs.Timer = time.AfterFunc(d, func() {
				fs.BackupINode(d)
			})
		}()

		if fs.Dirty {
			fs.Dirty = false

			prefix := "inode:"
			startKey := ""

			blockIdx := 0
			bakINodes := []BackUpINode{}

			// 计算备份一次耗时
			start := time.Now()
			for {
				keys, nextKey, err := fs.KVStore.Scan(prefix, startKey, 10000)
				if err != nil {
					logger.Errorf("failed to scan inodes: %v", err)
					return
				}
				for _, key := range keys {
					var inode *INode
					if err := fs.KVStore.Get(key, &inode); err != nil {
						logger.Errorf("failed to get inode %s: %v", key, err)
						continue
					}
					backupINode := BackUpINode{
						Ino:           inode.Ino,
						Parent:        inode.Parent,
						Kind:          inode.Kind,
						Name:          inode.Name,
						Chunks:        []string{},
						SymlinkTarget: inode.SymlinkTarget,
					}
					for _, chunk := range inode.Chunks {
						backupINode.Chunks = append(backupINode.Chunks, chunk.Hash)
					}
					bakINodes = append(bakINodes, backupINode)
				}

				if data, err := msgpack.Marshal(bakINodes); err != nil {
					logger.Errorf("failed to marshal inodes: %v", err)
					return
				} else {
					block := &Block{
						Header: BlockHeader{
							ID:        fmt.Sprintf("%03d00000000000000000000000000000", blockIdx),
							Ver:       1,
							Etag:      md5.Sum(data),
							TotalSize: int64(len(data)),
							CreatedAt: uint64(time.Now().UnixNano()),
							ChunkList: []*BlockChunk{
								{
									Hash: utils.CalcHash(data),
									Size: int32(len(data)),
								},
							},
						},
						Data: data,
					}

					if err := SaveBlock(block, fs); err != nil {
						logger.Errorf("failed to save block: %v", err)
						return
					} else {
						logger.Infof("success saved block: %s", block.Header.ID)
						blockIdx++
						bakINodes = []BackUpINode{}
					}
				}

				if nextKey == "" {
					break
				}
				startKey = nextKey
			}

			logger.Infof("backup inodes done, cost: %s", time.Since(start))
		}
	}()
	return nil
}

func (fs *DedupFS) ClearAll() error {
	// fs.mutex.Lock()
	// defer fs.mutex.Unlock()

	logger.Debugf("clear all store for fs %s", fs.MountPoint)
	ClearINodeCache(fs)
	ClearChunkCache(fs)
	ClearBlockCache(fs)

	if fs.KVStore != nil {
		if err := fs.KVStore.ClearAll(); err != nil {
			return err
		}
	}
	fs.Dirty = true
	// 重置根节点
	fs.RootNode = NewTree()
	fs.NextNodeID.Store(1) // 1已经用于根目录
	fs.NextHandle.Store(1)
	root := CreateINode(1, 1, 1, 1, FileTypeDir, "/", 0777)

	// 初始化 一些属性
	root.SetXattr("user.dedupfs.version", []byte("0.1.0"))
	root.SetXattr("user.dedupfs.id", []byte(fs.ID))
	root.SetXattr("user.dedupfs.datadir", []byte(fs.DataDir))
	root.SetXattr("user.dedupfs.metadir", []byte(fs.MetaDir))
	cc, _ := json.Marshal(fs.ChunkConf)
	root.SetXattr("user.dedupfs.chunkconf", cc)
	bc, _ := json.Marshal(fs.BlockConf)
	root.SetXattr("user.dedupfs.blockconf", bc)
	if err := SaveINode(fs, root); err != nil {
		logger.Errorf("failed to save root inode: %v", err)
		return err
	}

	return nil
}
