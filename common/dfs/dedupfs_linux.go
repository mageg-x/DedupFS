//go:build linux || darwin

package dfs

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/mageg-x/dedupfs/common/kvstore"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"

	"github.com/mageg-x/dedupfs/common/utils"
)

// CheckPermission 统一的权限校验方法
func CheckPermission(inode *INode, uid, gid uint32, mask uint32) error {
	logger.Debugf("checking permission: uid=%d, gid=%d, mask=%d, inodeUid=%d, inodeGid=%d, perm=%o",
		uid, gid, mask, inode.Uid, inode.Gid, inode.Mode)

	// 检查权限
	allowed := false

	if uid == inode.Uid {
		// 所有者权限
		if uint32(ReadPermission)&mask != 0 && (inode.Mode&0400) != 0 {
			allowed = true
		}
		if uint32(WritePermission)&mask != 0 && (inode.Mode&0200) != 0 {
			allowed = true
		}
		if uint32(ExecPermission)&mask != 0 && (inode.Mode&0100) != 0 {
			allowed = true
		}
	} else if gid == inode.Gid {
		// 组权限
		if uint32(ReadPermission)&mask != 0 && (inode.Mode&0040) != 0 {
			allowed = true
		}
		if uint32(WritePermission)&mask != 0 && (inode.Mode&0020) != 0 {
			allowed = true
		}
		if uint32(ExecPermission)&mask != 0 && (inode.Mode&0010) != 0 {
			allowed = true
		}
	} else {
		// 其他用户权限
		if uint32(ReadPermission)&mask != 0 && (inode.Mode&0004) != 0 {
			allowed = true
		}
		if uint32(WritePermission)&mask != 0 && (inode.Mode&0002) != 0 {
			allowed = true
		}
		if uint32(ExecPermission)&mask != 0 && (inode.Mode&0001) != 0 {
			allowed = true
		}
	}

	if !allowed {
		logger.Debugf("permission denied: uid=%d, gid=%d, mask=%d, inodeUid=%d, inodeGid=%d, perm=%o",
			uid, gid, mask, inode.Uid, inode.Gid, inode.Mode)
		return syscall.EACCES
	}

	return nil
}

// BaseNode represents a base node for files and directories with common fields and methods
type BaseNode struct {
	fs  *DedupFS
	ino uint64
}

type FileNode struct {
	BaseNode
}

type DirNode struct {
	BaseNode
}

// NodeHandle represents a file handle
type NodeHandle struct {
	fs  *DedupFS
	ino uint64
}

// 初始化DedupFS
func NewDedupFS(mountPoint, baseDir string, chunkConf *ChunkConfig, blockConf *BlockConfig) (*DedupFS, error) {
	fs := &DedupFS{
		MountPoint: mountPoint,
		ID:         fmt.Sprintf("%x", md5.Sum([]byte(mountPoint))),
		BaseDir:    baseDir,
		MetaDir:    filepath.Join(baseDir, "meta"),
		DataDir:    filepath.Join(baseDir, "data"),
		ChunkConf:  chunkConf,
		BlockConf:  blockConf,
		mutex:      sync.RWMutex{},
		openmap:    make(map[uint64]uint64),
	}

	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	fs.NextNodeID.Store(1)
	fs.NextHandle.Store(1)
	var err error
	// 创建必要的目录
	if err = os.MkdirAll(fs.MetaDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create meta directory: %w", err)
	}
	if err = os.MkdirAll(fs.DataDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create data directory: %w", err)
	}

	fs.KVStore, err = kvstore.NewKVStore(filepath.Join(fs.MetaDir, "dedupfs.db"), false, func(op, key string, value interface{}) {
		if op == "set" || op == "del" {
			if strings.HasPrefix(key, "inode:") {
				fs.Dirty = true
			}
		}
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create kv store: %w", err)
	}

	// 初始化根目录节点
	root, _ := GetINode(fs, 1)
	if root != nil {
		// 从存储中重建目录树
		fs.RootNode, err = fs.BuildNodeTree()
	} else {
		fs.RootNode = NewTree()
		root = CreateINode(1, 1, 1, 1, FileTypeDir, "/", 0777)
		fs.NextNodeID.Store(1) // 1已经用于根目录
	}

	// 定时备份任务
	d := 5*time.Minute + time.Duration(rand.Int63n(int64(30*time.Second)))
	fs.Timer = time.AfterFunc(d, func() {
		fs.BackupINode(d)
	})

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
		return nil, err
	}

	return fs, nil
}

func (fs *DedupFS) Root() (fs.Node, error) {
	logger.Debugf("dedupfs.root called")
	return DirNode{BaseNode: BaseNode{fs: fs, ino: 1}}, nil
}

// Statfs 方法 - 使用跨平台函数获取磁盘统计，失败时使用默认值
func (fs *DedupFS) Statfs(ctx context.Context, req *fuse.StatfsRequest, resp *fuse.StatfsResponse) error {
	// 总节点个数
	totalFiles := len(fs.RootNode.Nodes)
	// 使用跨平台函数获取数据目录的磁盘统计
	fsStats, err := utils.FsStats(fs.DataDir)
	if err != nil {
		logger.Errorf("failed to get filesystem stats for %s: %v, using default values", fs.DataDir, err)

		// 使用默认值
		resp.Bsize = 4096
		resp.Blocks = 1000000
		resp.Bfree = 500000
		resp.Bavail = 500000
		resp.Files = uint64(totalFiles)
		resp.Ffree = 1000000
		resp.Frsize = 4096
		resp.Namelen = 255

		logger.Debugf("Using default Statfs: blocks=%d, bfree=%d, bsize=%d", resp.Blocks, resp.Bfree, resp.Bsize)
	} else {
		// 使用跨平台函数获取的真实统计信息
		resp.Bsize = uint32(fsStats.Bsize)     // 文件系统块大小
		resp.Blocks = fsStats.Blocks           // 总数据块数
		resp.Bfree = fsStats.Bfree             // 空闲块数
		resp.Bavail = fsStats.Bavail           // 可用块数（非超级用户）
		resp.Files = uint64(totalFiles)        // 总文件节点数
		resp.Ffree = fsStats.Ffree             // 空闲文件节点数
		resp.Frsize = uint32(fsStats.Frsize)   // 基本块大小
		resp.Namelen = uint32(fsStats.Namelen) // 最大文件名长度

		logger.Debugf("Statfs from cross-platform function: blocks=%d, bfree=%d, bavail=%d, bsize=%d", resp.Blocks, resp.Bfree, resp.Bavail, resp.Bsize)
	}

	return nil
}

// Attr retrieves the attributes of a node
func (fn BaseNode) Attr(ctx context.Context, a *fuse.Attr) error {
	logger.Debugf("base.attr called for ino: %d", fn.ino)
	fn.fs.mutex.RLock()
	defer fn.fs.mutex.RUnlock()

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil {
		logger.Errorf("base.attr: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	a.Inode = inode.Ino
	a.Size = inode.Size
	blockSize := uint32(512) // 传统块大小
	a.Blocks = (a.Size + uint64(blockSize) - 1) / uint64(blockSize)
	a.Atime = inode.Atime
	a.Mtime = inode.Mtime
	a.Ctime = inode.Ctime
	a.Mode = os.FileMode(inode.Mode)
	a.Nlink = uint32(inode.Nlink)
	a.Uid = inode.Uid
	a.Gid = inode.Gid
	a.Rdev = inode.Rdev
	a.Flags = fuse.AttrFlags(inode.Flags)

	switch inode.Kind {
	case FileTypeSymlink:
		a.Mode |= os.ModeSymlink
	case FileTypeDir:
		a.Mode |= os.ModeDir | 0o555
	}
	return nil
}

// Lookup looks up a file or directory in a directory
func (fn BaseNode) Lookup(ctx context.Context, name string) (fs.Node, error) {
	logger.Debugf("dir.lookup called for ino: %d, name: %s", fn.ino, name)
	fn.fs.mutex.RLock()
	defer fn.fs.mutex.RUnlock()

	// Check if directory node exists
	dirNode, exists := fn.fs.RootNode.Get(fn.ino)
	if !exists || dirNode == nil {
		logger.Errorf("dir.lookup: directory node not found: %d", fn.ino)
		return nil, syscall.ENOENT
	}

	// Look through children
	for _, child := range dirNode.Children {
		if child != nil {
			if inode, _ := GetINode(fn.fs, child.ID); inode != nil && inode.Name == name {
				if inode.Kind == FileTypeDir {
					logger.Debugf("dir.lookup: found directory %s with ino %d", name, child.ID)
					return DirNode{BaseNode: BaseNode{fs: fn.fs, ino: child.ID}}, nil
				} else {
					logger.Debugf("dir.lookup: found file %s with ino %d", name, child.ID)
					return FileNode{BaseNode: BaseNode{fs: fn.fs, ino: child.ID}}, nil
				}
			}
		}
	}
	return nil, syscall.ENOENT
}

// Remove removes a file or directory
func (fn BaseNode) Remove(ctx context.Context, req *fuse.RemoveRequest) error {
	logger.Debugf("dir.remove called for ino: %d %d, name: %s", fn.ino, req.Node, req.Name)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	// Check if directory node exists
	node, exists := fn.fs.RootNode.Get(fn.ino)
	if !exists || node == nil || node.ID != fn.ino {
		logger.Errorf("dir.remove: directory node not found: %d", fn.ino)
		return syscall.ENOENT
	}

	// 根据 req.Name 获取 inode
	var inode *INode
	var err error
	for _, child := range node.Children {
		if child != nil {
			if inode, err = GetINode(fn.fs, child.ID); inode != nil && inode.Name == req.Name {
				break
			}
		}
	}

	if err != nil || inode == nil {
		logger.Errorf("dir.rename: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	node, exists = fn.fs.RootNode.Get(inode.Ino)
	if !exists || node == nil || node.ID != inode.Ino {
		logger.Errorf("dir.remove: directory node not found: %d", inode.Ino)
		return syscall.ENOENT
	}

	// 检查权限
	if err := CheckPermission(inode, uint32(os.Getuid()), uint32(os.Getgid()), uint32(ReadWritePermission)); err != nil {
		logger.Errorf("dir.remove: permission denied: %v", err)
		return syscall.EACCES
	}

	// 检查目录是否为空
	if inode.Kind == FileTypeDir && len(node.Children) > 0 {
		logger.Errorf("dir.remove: directory not empty: %s, has %d childrens", req.Name, len(node.Children))
		return syscall.ENOTEMPTY
	}

	if err := DelINode(fn.fs, node.ID); err != nil {
		logger.Errorf("dir.remove: failed to delete inode: %v", err)
		return syscall.EIO
	}

	logger.Debugf("dir.remove: removed %s with ino %d", req.Name, node.ID)
	return nil
}

// Rename renames a file or directory

// 源类型 (oldpath)	目标类型 (newpath)	目标是否存在	结果
// 任何类型	任何类型	否	成功重命名
// 非目录	非目录	是	成功替换（原子性）
// 非目录	目录	是	失败，返回ENOTDIR（因为不能用非目录替换目录）
// 目录	非目录	是	失败，返回EISDIR（因为不能用目录替换非目录）
// 目录	目录	是	如果目标目录为空，则成功替换；如果目标目录非空，则失败，返回ENOTEMPTY
func (fn BaseNode) Rename(ctx context.Context, req *fuse.RenameRequest, newDir fs.Node) error {
	logger.Debugf("dir.rename called for ino: %d, name: %s, newDir: %s", fn.ino, req.OldName, req.NewName)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	newDirNode, ok := fn.fs.RootNode.Get(uint64(req.NewDir))
	if !ok || newDirNode == nil {
		logger.Errorf("dir.rename: new directory node not found: %d", req.NewDir)
		return syscall.ENOENT
	}

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("dir.rename: failed to get inode: %v", err)
		return syscall.ENOENT
	}
	// 对源目标有读权限
	if err := CheckPermission(inode, uint32(os.Getuid()), uint32(os.Getgid()), uint32(ReadPermission)); err != nil {
		logger.Errorf("dir.rename: permission denied: %v", err)
		return syscall.EACCES
	}
	newDirINode, err := GetINode(fn.fs, uint64(req.NewDir))
	if err != nil || newDirINode == nil {
		logger.Errorf("dir.rename: failed to get new directory inode: %v", err)
		return syscall.ENOENT
	}
	// 对目标目录有写权限
	if err := CheckPermission(newDirINode, uint32(os.Getuid()), uint32(os.Getgid()), uint32(AllPermission)); err != nil {
		logger.Errorf("dir.rename: permission denied: %v", err)
		return syscall.EACCES
	}

	// 如果目标目录下已经存在同名文件或目录，就非常复杂的逻辑
	for _, child := range newDirNode.Children {
		if child != nil {
			if target, _ := GetINode(fn.fs, child.ID); target != nil && target.Name == req.NewName {
				// 源是目录，目标不是目录，失败返回
				if inode.Kind == FileTypeDir && target.Kind != FileTypeDir {
					logger.Errorf("dir.rename: target is not dir: %s", req.NewName)
					return syscall.ENOTDIR
				}
				// 源是文件，目标是目录，失败返回
				if inode.Kind != FileTypeDir && target.Kind == FileTypeDir {
					logger.Errorf("dir.rename: target is dir: %s", req.NewName)
					return syscall.EISDIR
				}
				// 目标目录非空，失败返回
				if target.Kind == FileTypeDir && len(child.Children) > 0 {
					logger.Errorf("dir.rename: target directory not empty: %s, has %d childrens", req.NewName, len(child.Children))
					return syscall.ENOTEMPTY
				}
				// 都是文件， 或者  都是目录，目标目录为空---- 就替换，先删除旧的
				if (inode.Kind == FileTypeFile && target.Kind == FileTypeFile) ||
					(inode.Kind == FileTypeDir && target.Kind == FileTypeDir) {
					if err := DelINode(fn.fs, child.ID); err != nil {
						logger.Errorf("dir.rename: failed to delete inode: %v", err)
						return syscall.EIO
					}
					break
				}
				// 走到这儿就是非预期情况，失败返回
				logger.Errorf("dir.rename: unexpected condition: %s", req.NewName)
				return syscall.EIO
			}
		}
	}

	// 如果不是同一个目录，需要更改父子关系
	if inode.Parent != newDirINode.Ino {
		if err := fn.fs.RootNode.Move(inode.Ino, newDirINode.Ino); err != nil {
			logger.Errorf("dir.rename: failed to move node: %v", err)
			return syscall.EIO
		}
	}

	inode.Name = req.NewName
	if err := SaveINode(fn.fs, inode); err != nil {
		logger.Errorf("dir.rename: failed to save inode: %v", err)
		return syscall.EIO
	}
	logger.Debugf("dir.rename: renamed %s to %s in ino: %d", req.OldName, req.NewName, fn.ino)
	return nil
}

// Access checks access permissions
func (fn BaseNode) Access(ctx context.Context, req *fuse.AccessRequest) error {
	logger.Debugf("base.access called for ino: %d, mask: %d", fn.ino, req.Mask)
	fn.fs.mutex.RLock()
	defer fn.fs.mutex.RUnlock()

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("base.access: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	// 使用统一的权限校验方法
	if err := CheckPermission(inode, req.Header.Uid, req.Header.Gid, req.Mask); err != nil {
		return err
	}

	return nil
}

// Getattr retrieves attributes and sets them to the response
func (fn BaseNode) Getattr(ctx context.Context, req *fuse.GetattrRequest, resp *fuse.GetattrResponse) error {
	return fn.Attr(ctx, &resp.Attr)
}

// Setattr sets attributes of a node
func (fn BaseNode) Setattr(ctx context.Context, req *fuse.SetattrRequest, resp *fuse.SetattrResponse) error {
	logger.Debugf("base.setattr called for ino: %d, mode: %o, size: %d", fn.ino, req.Mode, req.Size)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("getxattr: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	if err := CheckPermission(inode, req.Header.Uid, req.Header.Gid, uint32(AllPermission)); err != nil {
		logger.Errorf("base.setattr: permission denied: %v", err)
		return syscall.EACCES
	}

	// 更新文件属性
	now := time.Now().UTC()

	if req.Valid&fuse.SetattrMode != 0 {
		inode.Mode = (inode.Mode & syscall.S_IFMT) | uint32(req.Mode)&07777
	}

	if req.Valid&fuse.SetattrUid != 0 {
		inode.Uid = req.Uid
	}

	if req.Valid&fuse.SetattrGid != 0 {
		inode.Gid = req.Gid
	}

	if req.Valid&fuse.SetattrSize != 0 {
		// inode.Size = req.Size
		if err := inode.Truncate(fn.fs, req.Size); err != nil {
			logger.Errorf("base.setattr: failed to truncate file: %v", err)
			return syscall.EIO
		}
	}

	if req.Valid&fuse.SetattrAtime != 0 {
		inode.Atime = req.Atime
	} else if req.Valid&fuse.SetattrAtimeNow != 0 {
		inode.Atime = now
	}

	if req.Valid&fuse.SetattrMtime != 0 {
		inode.Mtime = req.Mtime
	} else if req.Valid&fuse.SetattrMtimeNow != 0 {
		inode.Mtime = now
	}

	// 每次修改都更新ctime
	inode.Ctime = now

	// 保存更新后的inode
	if err := SaveINode(fn.fs, inode); err != nil {
		logger.Errorf("base.setattr: failed to save inode: %v", err)
		return syscall.EIO
	}

	// 设置响应属性
	resp.Attr.Inode = inode.Ino
	resp.Attr.Size = inode.Size
	resp.Attr.Blocks = inode.Blocks
	resp.Attr.Atime = inode.Atime
	resp.Attr.Mtime = inode.Mtime
	resp.Attr.Ctime = inode.Ctime
	resp.Attr.Mode = os.FileMode(inode.Mode)
	resp.Attr.Nlink = uint32(inode.Nlink)
	resp.Attr.Uid = inode.Uid
	resp.Attr.Gid = inode.Gid
	resp.Attr.Rdev = inode.Rdev
	resp.Attr.Flags = fuse.AttrFlags(inode.Flags)

	switch inode.Kind {
	case FileTypeSymlink:
		resp.Attr.Mode |= os.ModeSymlink
	case FileTypeDir:
		resp.Attr.Mode |= os.ModeDir
	}
	return nil
}

// Getxattr retrieves an extended attribute
func (fn BaseNode) Getxattr(ctx context.Context, req *fuse.GetxattrRequest, resp *fuse.GetxattrResponse) error {
	logger.Debugf("getxattr called for ino: %d, name: %s", fn.ino, req.Name)
	fn.fs.mutex.RLock()
	defer fn.fs.mutex.RUnlock()

	// 一些扩展特殊的属性, user.dedupfs.inode.xx 提前 xx 为 ino
	if strings.HasPrefix(req.Name, "user.dedupfs.inode.") {
		// 提取ino
		ino, err := strconv.ParseUint(req.Name[len("user.dedupfs.inode."):], 10, 64)
		if err != nil {
			logger.Errorf("failed to parse ino from xattr name: %v", err)
			return fmt.Errorf("failed to parse ino from xattr name")
		}
		if inode, err := GetINode(fn.fs, ino); err != nil || inode == nil {
			logger.Errorf("getxattr: failed to get inode: %v", err)
			return syscall.ENOENT
		} else {
			value, err := json.Marshal(inode)
			if err != nil {
				logger.Errorf("failed to marshal inode: %v", err)
				return syscall.EIO
			}
			resp.Xattr = value
			return nil
		}
	} else if strings.HasPrefix(req.Name, "user.dedupfs.chunk.data.") {
		// 提取chunk hash
		chunkHash := req.Name[len("user.dedupfs.chunk.data."):]
		chunk, err := GetChunkData(chunkHash, fn.fs)
		if err != nil || chunk == nil || chunk.Data == nil {
			logger.Errorf("getxattr: failed to get chunk data: %v", err)
			return syscall.ENOENT
		}
		value := chunk.Data
		// 长度不能超过 64K
		if len(value) > 65536 {
			value = value[:65536]
		}
		resp.Xattr = value
		return nil
	} else if strings.HasPrefix(req.Name, "user.dedupfs.chunk.meta.") {
		// 提取chunk hash
		chunkHash := req.Name[len("user.dedupfs.chunk.meta."):]
		chunk, err := GetChunkMeta(chunkHash, fn.fs)
		if err != nil || chunk == nil {
			logger.Errorf("getxattr: failed to get chunk %s meta: %v", chunkHash, err)
			return syscall.ENOENT
		}
		value, err := json.Marshal(chunk)
		if err != nil {
			logger.Errorf("failed to marshal chunk %s: %v", chunkHash, err)
			return syscall.EIO
		}
		resp.Xattr = value
		return nil
	}

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("getxattr: failed to get inode %d: %v", fn.ino, err)
		return syscall.ENOENT
	}

	value, err := inode.GetXattr(req.Name)
	if err != nil {
		logger.Infof("getxattr: failed to get attribute %s: %v", req.Name, err)
		return syscall.ENODATA
	}

	resp.Xattr = value
	return nil
}

// Listxattr lists all extended attributes
func (fn BaseNode) Listxattr(ctx context.Context, req *fuse.ListxattrRequest, resp *fuse.ListxattrResponse) error {
	logger.Debugf("listxattr called for ino: %d", fn.ino)
	fn.fs.mutex.RLock()
	defer fn.fs.mutex.RUnlock()

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("listxattr: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	attrs, _ := inode.ListXattr()
	for _, name := range attrs {
		resp.Append(name)
	}

	return nil
}

// Setxattr sets an extended attribute
func (fn BaseNode) Setxattr(ctx context.Context, req *fuse.SetxattrRequest) error {
	logger.Debugf("setxattr called for ino: %d, name: %s", fn.ino, req.Name)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("setxattr: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	if err := CheckPermission(inode, req.Header.Uid, req.Header.Gid, uint32(WritePermission)); err != nil {
		logger.Errorf("setxattr: permission denied: %v", err)
		return syscall.EACCES
	}

	// 调用INode的SetXattr方法
	if err := inode.SetXattr(req.Name, req.Xattr); err != nil {
		logger.Errorf("setxattr: failed to set attribute %s: %v", req.Name, err)
		return syscall.EIO
	}

	// 保存更新后的inode
	if err := SaveINode(fn.fs, inode); err != nil {
		logger.Errorf("setxattr: failed to save inode: %v", err)
		return syscall.EIO
	}

	return nil
}

// Removexattr removes an extended attribute
func (fn BaseNode) Removexattr(ctx context.Context, req *fuse.RemovexattrRequest) error {
	logger.Debugf("removexattr called for ino: %d, name: %s", fn.ino, req.Name)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("removexattr: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	if err := CheckPermission(inode, req.Header.Uid, req.Header.Gid, uint32(WritePermission)); err != nil {
		logger.Errorf("removexattr: permission denied: %v", err)
		return syscall.EACCES
	}

	// 调用INode的RemoveXattr方法
	if err := inode.RemoveXattr(req.Name); err != nil {
		logger.Errorf("removexattr: failed to remove attribute %s: %v", req.Name, err)
		return syscall.ENOENT
	}

	// 保存更新后的inode
	if err := SaveINode(fn.fs, inode); err != nil {
		logger.Errorf("removexattr: failed to save inode: %v", err)
		return syscall.EIO
	}

	return nil
}

// Open opens a file
func (fn FileNode) Open(ctx context.Context, req *fuse.OpenRequest, resp *fuse.OpenResponse) (fs.Handle, error) {
	logger.Debugf("file.open called for ino: %d, flags: %d", fn.ino, req.Flags)

	// 检查文件是否存在
	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("file.open: failed to get inode: %v", err)
		return nil, syscall.ENOENT
	}

	// 检查权限
	mask := uint32(0)
	if req.Flags.IsReadOnly() {
		mask = uint32(ReadPermission)
	} else if req.Flags.IsWriteOnly() {
		mask = uint32(WritePermission)
	} else if req.Flags.IsReadWrite() {
		mask = uint32(ReadPermission | WritePermission)
	}
	if err := CheckPermission(inode, req.Header.Uid, req.Header.Gid, uint32(mask)); err != nil {
		logger.Errorf("file.open: permission denied: %v", err)
		return nil, syscall.EACCES
	}

	// 设置响应标志
	resp.Flags |= fuse.OpenKeepCache
	resp.Handle = fuse.HandleID(fn.ino) // 使用 inode 号作为句柄ID

	logger.Debugf("file.open: successfully opened ino %d with flags %d", fn.ino, req.Flags)
	return NodeHandle{fs: fn.fs, ino: fn.ino}, nil
}

// Create creates a new file in a directory
func (fn DirNode) Create(ctx context.Context, req *fuse.CreateRequest, resp *fuse.CreateResponse) (fs.Node, fs.Handle, error) {
	logger.Debugf("dir.create called for ino: %d, name: %s, flags: %d", fn.ino, req.Name, req.Flags)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	// Check if directory node exists
	node, exists := fn.fs.RootNode.Get(fn.ino)
	if !exists || node == nil {
		logger.Errorf("dir.create: directory node not found: %d", fn.ino)
		return nil, nil, syscall.ENOENT
	}

	_inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || _inode == nil {
		logger.Errorf("dir.create: failed to get directory node: %v", err)
		return nil, nil, syscall.ENOENT
	}

	// 检查权限
	mask := uint32(0)
	if req.Flags.IsReadOnly() {
		mask = uint32(ReadPermission)
	} else if req.Flags.IsWriteOnly() {
		mask = uint32(WritePermission)
	} else if req.Flags.IsReadWrite() {
		mask = uint32(ReadPermission | WritePermission)
	}
	if err := CheckPermission(_inode, req.Header.Uid, req.Header.Gid, mask); err != nil {
		logger.Errorf("dir.create: permission denied: %v", err)
		return nil, nil, syscall.EACCES
	}

	// 检查是否已经存在
	for _, child := range node.Children {
		if child != nil {
			n, err := GetINode(fn.fs, child.ID)
			if err != nil || n == nil {
				logger.Errorf("dir.create: failed to get child node: %v", err)
				return nil, nil, syscall.EIO
			}
			if n.Name == req.Name {
				logger.Errorf("dir.create: file already exists: %s", req.Name)
				return nil, nil, syscall.EEXIST
			}
		}
	}

	// 获取uid 和 gid
	uid := req.Header.Uid
	gid := req.Header.Gid

	// Create file node using the common function
	inode, err := fn.fs.CreateNode(fn.ino, uid, gid, req.Name, FileTypeFile, req.Mode, nil)
	if err != nil {
		logger.Errorf("dir.create: failed to create file node: %v", err)
		return nil, nil, syscall.EIO
	}

	// Set response attributes
	resp.EntryValid = time.Second
	resp.Attr.Inode = inode.Ino
	resp.Attr.Size = inode.Size
	resp.Attr.Atime = inode.Atime
	resp.Attr.Mtime = inode.Mtime
	resp.Attr.Ctime = inode.Ctime
	resp.Attr.Mode = os.FileMode(inode.Mode)
	resp.Attr.Nlink = uint32(inode.Nlink)
	resp.Attr.Uid = inode.Uid
	resp.Attr.Gid = inode.Gid
	resp.Attr.Rdev = inode.Rdev
	resp.Attr.Flags = fuse.AttrFlags(inode.Flags)
	logger.Debugf("dir.create: successfully created file %s with ino %d, mode: %o", req.Name, inode.Ino, inode.Mode)

	return FileNode{BaseNode: BaseNode{fs: fn.fs, ino: inode.Ino}}, NodeHandle{fs: fn.fs, ino: inode.Ino}, nil
}

// Mkdir creates a new directory in a directory
func (fn DirNode) Mkdir(ctx context.Context, req *fuse.MkdirRequest) (fs.Node, error) {
	logger.Debugf("dir.mkdir called for ino: %d, name: %s, mode: %d", fn.ino, req.Name, req.Mode)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	// Check if directory node exists
	node, exists := fn.fs.RootNode.Get(fn.ino)
	if !exists || node == nil {
		logger.Errorf("dir.mkdir: directory node not found: %d", fn.ino)
		return nil, syscall.ENOENT
	}

	_inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || _inode == nil {
		logger.Errorf("dir.mkdir: failed to get directory node: %v", err)
		return nil, syscall.ENOENT
	}

	// 检查权限
	if err := CheckPermission(_inode, req.Header.Uid, req.Header.Gid, uint32(WritePermission)); err != nil {
		logger.Errorf("dir.mkdir: permission denied: %v", err)
		return nil, syscall.EACCES
	}

	// 检查是否已经存在
	for _, child := range node.Children {
		if child != nil {
			n, err := GetINode(fn.fs, child.ID)
			if err != nil || n == nil {
				logger.Errorf("dir.mkdir: failed to get child node: %v", err)
				return nil, syscall.EIO
			}
			if n.Name == req.Name {
				logger.Errorf("dir.mkdir: file already exists: %s", req.Name)
				return nil, syscall.EEXIST
			}
		}
	}

	// 获取uid 和 gid
	uid := req.Header.Uid
	gid := req.Header.Gid

	// Create directory node using the common function
	inode, err := fn.fs.CreateNode(fn.ino, uid, gid, req.Name, FileTypeDir, req.Mode, nil)
	if err != nil {
		return nil, err
	}

	return DirNode{BaseNode: BaseNode{fs: fn.fs, ino: inode.Ino}}, nil
}

// Symlink creates a new symbolic link in a directory
func (fn BaseNode) Symlink(ctx context.Context, req *fuse.SymlinkRequest) (fs.Node, error) {
	logger.Debugf("dir.symlink called for ino: %d, name: %s, target: %s", fn.ino, req.NewName, req.Target)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	// Check if directory node exists
	node, exists := fn.fs.RootNode.Get(fn.ino)
	if !exists || node == nil {
		logger.Errorf("dir.symlink: directory node not found: %d", fn.ino)
		return nil, syscall.ENOENT
	}

	_inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || _inode == nil {
		logger.Errorf("dir.symlink: failed to get directory node: %v", err)
		return nil, syscall.ENOENT
	}

	// 检查权限
	if err := CheckPermission(_inode, req.Header.Uid, req.Header.Gid, uint32(WritePermission)); err != nil {
		logger.Errorf("dir.symlink: permission denied: %v", err)
		return nil, syscall.EACCES
	}

	// 检查是否已经存在
	for _, child := range node.Children {
		if child != nil {
			n, err := GetINode(fn.fs, child.ID)
			if err != nil || n == nil {
				logger.Errorf("dir.symlink: failed to get child node: %v", err)
				return nil, syscall.EIO
			}
			if n.Name == req.NewName {
				logger.Errorf("dir.symlink: file already exists: %s", req.NewName)
				return nil, syscall.EEXIST
			}
		}
	}

	// 获取uid 和 gid
	uid := req.Header.Uid
	gid := req.Header.Gid

	// Create symlink node using the common function
	target := req.Target
	inode, err := fn.fs.CreateNode(fn.ino, uid, gid, req.NewName, FileTypeSymlink, 0777, &target)
	if err != nil {
		return nil, err
	}

	return BaseNode{fs: fn.fs, ino: inode.Ino}, nil
}

// ReadDirAll reads the contents of a directory
func (fn DirNode) ReadDirAll(ctx context.Context) ([]fuse.Dirent, error) {
	logger.Debugf("dir.readdirall called for ino: %d", fn.ino)
	fn.fs.mutex.RLock()
	defer fn.fs.mutex.RUnlock()

	// Get directory node
	dirNode, exists := fn.fs.RootNode.Get(fn.ino)
	if !exists || dirNode == nil {
		logger.Errorf("dir.readdirall: directory node not found: %d", fn.ino)
		return nil, fmt.Errorf("directory node not found")
	}

	// Get directory inode to find parent
	dirInode, err := GetINode(fn.fs, fn.ino)
	if err != nil {
		logger.Errorf("dir.readdirall: failed to get directory inode: %v", err)
		return nil, err
	}

	// Prepare directory entries including . and ..
	dirents := make([]fuse.Dirent, 0, len(dirNode.Children)+2)

	// Add . entry
	dirents = append(dirents, fuse.Dirent{
		Inode: fn.ino,
		Name:  ".",
		Type:  fuse.DT_Dir,
	})

	// Add .. entry, parent is self for root directory
	parentIno := dirInode.Parent
	if parentIno == 0 {
		parentIno = fn.ino // Default to self if parent not set
	}
	dirents = append(dirents, fuse.Dirent{
		Inode: parentIno,
		Name:  "..",
		Type:  fuse.DT_Dir,
	})

	// Add all children
	for _, child := range dirNode.Children {
		if child != nil {
			if inode, _ := GetINode(fn.fs, child.ID); inode != nil {
				var entryType fuse.DirentType
				switch inode.Kind {
				case FileTypeDir:
					entryType = fuse.DT_Dir
				case FileTypeFile:
					entryType = fuse.DT_File
				case FileTypeSymlink:
					entryType = fuse.DT_Link
				}
				dirents = append(dirents, fuse.Dirent{
					Inode: child.ID,
					Name:  inode.Name,
					Type:  entryType,
				})
			}
		}
	}

	logger.Debugf("dir.readdirall: read %d entries from directory ino: %d", len(dirents), fn.ino)
	return dirents, nil
}

// Read reads the contents of a file
func (fn FileNode) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	logger.Debugf("file.read called for ino: %d, offset: %d, size: %d", fn.ino, req.Offset, req.Size)
	fn.fs.mutex.RLock()
	defer fn.fs.mutex.RUnlock()

	// 获取文件节点
	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("file.read: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	// 检查读权限 (04: 读权限)
	if err := CheckPermission(inode, req.Header.Uid, req.Header.Gid, uint32(ReadPermission)); err != nil {
		logger.Errorf("file.read: permission denied for ino: %d", fn.ino)
		return err
	}
	if data, err := inode.Read(req.Offset, req.Size, fn.fs); err != nil {
		logger.Errorf("file.read: read %d bytes from ino %d", len(data), fn.ino)
		return syscall.EIO
	} else {
		resp.Data = data
	}

	// 如果需要实际读取数据，需要实现数据块的读取逻辑
	logger.Debugf("file.read: read %d bytes from ino %d", len(resp.Data), fn.ino)
	return nil
}

// Write writes data to a file
func (fn FileNode) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	logger.Debugf("file.write called for ino: %d, offset: %d, size: %d", fn.ino, req.Offset, len(req.Data))
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()

	// 获取文件节点
	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("file.write: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	// 检查写权限 (02: 写权限)
	if err := CheckPermission(inode, req.Header.Uid, req.Header.Gid, uint32(WritePermission)); err != nil {
		logger.Errorf("file.write: permission denied for ino: %d", fn.ino)
		return err
	}

	if err := inode.Write(fn.fs, req.Offset, req.Data); err != nil {
		logger.Errorf("file.write: failed to write data: %v", err)
		return syscall.EIO
	}

	resp.Size = len(req.Data)

	logger.Debugf("file.write: wrote %d bytes to ino %d", len(req.Data), fn.ino)
	return nil
}

// Readlink reads the target of a symbolic link
func (fn FileNode) Readlink(ctx context.Context, req *fuse.ReadlinkRequest) (string, error) {
	logger.Debugf("file.readlink called for ino: %d", fn.ino)
	fn.fs.mutex.RLock()
	defer fn.fs.mutex.RUnlock()

	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("file.readlink: failed to get inode: %v", err)
		return "", syscall.ENOENT
	}

	if inode.Kind != FileTypeSymlink {
		logger.Errorf("file.readlink: inode %d is not a symlink", fn.ino)
		return "", syscall.EINVAL
	}

	if inode.SymlinkTarget == nil {
		logger.Errorf("file.readlink: symlink target is nil for inode %d", fn.ino)
		return "", syscall.EINVAL
	}

	logger.Debugf("file.readlink: returning target %s for ino %d", *inode.SymlinkTarget, fn.ino)
	return *inode.SymlinkTarget, nil
}

// Release implements the HandleReleaser interface
func (fn FileNode) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	logger.Debugf("BaseNode.release called for ino: %d", fn.ino)
	fn.fs.mutex.Lock()
	defer fn.fs.mutex.Unlock()
	inode, err := GetINode(fn.fs, fn.ino)
	if err != nil || inode == nil {
		logger.Errorf("file.release: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	logger.Debugf("file.release: saving inode %d size %d  %d chunks", fn.ino, inode.Size, len(inode.Chunks))

	if err := FlushINode(fn.fs, inode); err != nil {
		logger.Errorf("file.release: failed to save inode: %v", err)
		return syscall.EIO
	}
	return nil
}

// Handle methods

// Read reads data from a file handle
func (h NodeHandle) Read(ctx context.Context, req *fuse.ReadRequest, resp *fuse.ReadResponse) error {
	logger.Debugf("handle.read called for ino: %d, offset: %d, size: %d", h.ino, req.Offset, req.Size)

	// 获取文件节点
	inode, err := GetINode(h.fs, h.ino)
	if err != nil || inode == nil {
		logger.Errorf("nodehandle.read: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	fn := FileNode{BaseNode: BaseNode{fs: h.fs, ino: h.ino}}
	return fn.Read(ctx, req, resp)
}

// Write writes data to a file handle
func (h NodeHandle) Write(ctx context.Context, req *fuse.WriteRequest, resp *fuse.WriteResponse) error {
	logger.Infof("handle.write called for ino: %d, offset: %d, size: %d", h.ino, req.Offset, len(req.Data))

	// 获取文件节点
	inode, err := GetINode(h.fs, h.ino)
	if err != nil || inode == nil {
		logger.Errorf("nodehandle.write: failed to get inode: %v", err)
		return syscall.ENOENT
	}

	// 使用FileNode的Write方法，因为我们已经实现了它
	fn := FileNode{BaseNode: BaseNode{fs: h.fs, ino: h.ino}}
	return fn.Write(ctx, req, resp)
}

func (h NodeHandle) Release(ctx context.Context, req *fuse.ReleaseRequest) error {
	logger.Debugf("NodeHandle.release called for ino: %d", h.ino)
	fn := FileNode{BaseNode: BaseNode{fs: h.fs, ino: h.ino}}
	return fn.Release(ctx, req)
}

func (h NodeHandle) Flush(ctx context.Context, req *fuse.FlushRequest) error {
	logger.Debugf("NodeHandle.flush called for ino: %d", h.ino)
	return nil
}

func (h NodeHandle) Fsync(ctx context.Context, req *fuse.FsyncRequest) error {
	logger.Debugf("NodeHandle.fsync called for ino: %d", h.ino)
	return nil
}
