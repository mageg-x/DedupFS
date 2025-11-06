//go:build windows

package dfs

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	_path "path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/winfsp/cgofuse/fuse"

	"github.com/mageg-x/dedupfs/common/kvstore"
	"github.com/mageg-x/dedupfs/common/utils"
)

// CheckPermission 统一的权限校验方法
func CheckPermission(inode *INode, uid, gid uint32, mask uint32) error {
	logger.Debugf("checking permission: uid=%d, gid=%d, mask=%d, inodeUid=%d, inodeGid=%d, perm=%o, name=%s",
		uid, gid, mask, inode.Uid, inode.Gid, inode.Perm, inode.Name)

	// Windows 下权限处理简化，假设所有用户都有访问权限
	// 实际生产环境应根据 Windows ACL 进行权限检查
	allowed := false

	if uid == inode.Uid {
		// 所有者权限
		if uint32(ReadPermission)&mask != 0 && (inode.Perm&0400) != 0 {
			allowed = true
		}
		if uint32(WritePermission)&mask != 0 && (inode.Perm&0200) != 0 {
			allowed = true
		}
		if uint32(ExecPermission)&mask != 0 && (inode.Perm&0100) != 0 {
			allowed = true
		}
	} else if gid == inode.Gid {
		// 组权限
		if uint32(ReadPermission)&mask != 0 && (inode.Perm&0040) != 0 {
			allowed = true
		}
		if uint32(WritePermission)&mask != 0 && (inode.Perm&0020) != 0 {
			allowed = true
		}
		if uint32(ExecPermission)&mask != 0 && (inode.Perm&0010) != 0 {
			allowed = true
		}
	} else {
		// 其他用户权限
		if uint32(ReadPermission)&mask != 0 && (inode.Perm&0004) != 0 {
			allowed = true
		}
		if uint32(WritePermission)&mask != 0 && (inode.Perm&0002) != 0 {
			allowed = true
		}
		if uint32(ExecPermission)&mask != 0 && (inode.Perm&0001) != 0 {
			allowed = true
		}
	}

	if !allowed {
		logger.Errorf("permission denied: uid=%d, gid=%d, mask=%d, inodeUid=%d, inodeGid=%d, perm=%o, name=%s",
			uid, gid, mask, inode.Uid, inode.Gid, inode.Perm, inode.Name)
		return syscall.EACCES
	}

	return nil
}

// NewDedupFS 创建 Windows 版本的 DedupFS
func NewDedupFS(mountPoint, baseDir string, chunkConf *ChunkConfig, blockConf *BlockConfig) (*DedupFS, error) {
	// 创建基础 DedupFS 实例
	fs := &DedupFS{
		MountPoint: mountPoint,
		ID:         fmt.Sprintf("%x", md5.Sum([]byte(mountPoint))),
		BaseDir:    baseDir,
		MetaDir:    filepath.Join(baseDir, "meta"),
		DataDir:    filepath.Join(baseDir, "data"),
		ChunkConf:  chunkConf,
		BlockConf:  blockConf,
		mutex:      sync.RWMutex{},
	}

	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	fs.NextNodeID.Store(1)

	var err error
	// 创建必要的目录
	if err = os.MkdirAll(fs.MetaDir, 0755); err != nil {
		logger.Errorf("failed to create meta directory: %w", err)
		return nil, fmt.Errorf("failed to create meta directory: %w", err)
	}
	if err = os.MkdirAll(fs.DataDir, 0755); err != nil {
		logger.Errorf("failed to create data directory: %w", err)
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
		logger.Errorf("failed to create kv store: %w", err)
		return nil, fmt.Errorf("failed to create kv store: %w", err)
	}

	// 初始化根目录节点
	root, _ := GetINode(fs, 1)
	if root != nil {
		// 从存储中重建目录树
		fs.RootNode, err = fs.BuildNodeTree()
	} else {
		fs.RootNode = NewTree()
		root = CreateINode(1, 1, FileTypeDir, "/", 0777)
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

// 实现 FileSystemInterface  接口
// Init is called when the file system is created.
func (fs *DedupFS) Init() {
	logger.Debugf("dedupfs.init")
}

// Destroy is called when the file system is destroyed.
func (fs *DedupFS) Destroy() {
	logger.Debugf("dedupfs.destroy")
	ClearINodeCache(fs)
	ClearChunkCache(fs)
	ClearBlockCache(fs)
	if fs.KVStore != nil {
		fs.KVStore.Close()
		fs.KVStore = nil
	}
	if fs.Timer != nil {
		fs.Timer.Stop()
		fs.Timer = nil
	}
	fs.Host = nil
}

func (fs *DedupFS) GetUidGid() (uid uint32, gid uint32, pid int) {
	uid, gid, pid = fuse.Getcontext()
	uid = 0
	gid = 0
	return uid, gid, pid
}

// Statfs gets file system statistics.
func (fs *DedupFS) Statfs(path string, stat *fuse.Statfs_t) int {
	logger.Debugf("dedupfs.statfs")
	path = _path.Clean(path)

	// 缺省值

	stat.Blocks = 1000000 // 总块数
	stat.Bfree = 900000   // 空闲块数（包括 root 保留）
	stat.Bavail = 900000  // 对普通用户可用的空闲块
	stat.Files = 100000   // 总 inode 数（可选）
	stat.Ffree = 90000    // 空闲 inode 数（可选）
	stat.Bsize = 4096     // 块大小（字节）
	stat.Namemax = 255    // 文件名最大长度
	stat.Frsize = 4096    // 基本块大小（通常 = Bsize）

	// 1. 使用GetDiskFreeSpaceEx获取总空间、可用空间等
	totalBytes, freeBytes, _, err := utils.GetDiskFreeSpaceEx(fs.DataDir)
	if err != nil {
		logger.Errorf("failed to get disk free space: %v", err)
		// return -fuse.EIO
	} else {
		logger.Debugf("totalBytes: %d, freeBytes: %d", totalBytes, freeBytes)
	}

	// 2. 获取卷信息以取得文件系统名和最大文件名长度
	fsName, maxNameLen, err := utils.GetVolumeInformation(path)
	if err != nil {
		logger.Errorf("failed to get volume information: %v", err)
		// return -fuse.EIO
	} else {
		logger.Debugf("fsName: %s, maxNameLen: %d", fsName, maxNameLen)
	}
	// 3. (可选) 对于NTFS，使用fsutil获取更精确的块信息[citation:4]
	var totalClusters, freeClusters, bytesPerCluster uint64
	if strings.ToUpper(fsName) == "NTFS" {
		totalClusters, freeClusters, _, bytesPerCluster, _ = utils.GetFsutilInfo(path)
		// 如果fsutil调用失败，可以回退到使用GetDiskFreeSpaceEx计算出的值
	}

	// 4. 填充stat结构
	// 块大小 (f_bsize)
	if bytesPerCluster > 0 {
		stat.Bsize = uint64(bytesPerCluster)
	}
	// 总数据块数 (f_blocks)
	if totalClusters > 0 {
		stat.Blocks = totalClusters // 注意：这里假设fsutil返回的"簇"对应stat的"块"
	}

	// 可用块数 (f_bfree)
	if freeClusters > 0 {
		stat.Bfree = freeClusters
	}

	stat.Bavail = stat.Bfree
	stat.Files = uint64(len(fs.RootNode.Nodes)) // 总文件节点数
	stat.Namemax = uint64(maxNameLen)
	stat.Ffree = 1000000 // 空闲文件节点数
	stat.Fsid = xxhash.Sum64([]byte(fs.ID))

	logger.Debugf("dedupfs.statfs: %#v", *stat)
	return 0
}

// Mknod creates a file node.
func (fs *DedupFS) Mknod(path string, mode uint32, dev uint64) int {
	logger.Errorf("mknod: path=%s, mode=0x%x, dev=%d - not supported", path, mode, dev)

	// DedupFS 不支持创建特殊文件节点
	// 普通文件用 Create，目录用 Mkdir，链接用 Symlink
	return -fuse.ENOSYS
}

// Mkdir creates a directory.
func (fs *DedupFS) Mkdir(path string, mode uint32) int {
	logger.Debugf("mkdir: path=%s, mode=0x%x", path, mode)
	path = _path.Clean(path)
	parentPath := _path.Dir(path)
	name := _path.Base(path)

	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	parentID := Path2Ino(fs, parentPath)
	if parentID == 0 {
		logger.Errorf("mkdir: parent directory not found: %s", parentPath)
		return -fuse.ENOENT
	}

	// 获取当前用户 ID
	uid, gid, _ := fs.GetUidGid()

	// 检查父目录权限
	parentInode, err := GetINode(fs, parentID)
	if err != nil || parentInode == nil {
		logger.Errorf("mkdir: parent directory not found: %s", parentPath)
		return -fuse.ENOENT
	}

	if err := CheckPermission(parentInode, uid, gid, uint32(WritePermission)); err != nil {
		logger.Errorf("mkdir: permission denied: %v", err)
		return -fuse.EACCES
	}

	// 检查是否已存在
	parentNode, exists := fs.RootNode.Get(parentID)
	if !exists || parentNode == nil {
		logger.Errorf("mkdir: parent directory not found: %s", parentPath)
		return -fuse.ENOENT
	}

	for _, child := range parentNode.Children {
		if child != nil {
			childInode, _ := GetINode(fs, child.ID)
			if childInode != nil && childInode.Name == name {
				logger.Debugf("mkdir: file already exists: %s", path)
				return -fuse.EEXIST
			}
		}
	}

	// 创建目录节点
	inode, err := fs.CreateNode(parentID, uid, gid, name, FileTypeDir, os.FileMode(mode), nil)
	if err != nil || inode == nil {
		logger.Errorf("mkdir: failed to create directory: %v", err)
		return -fuse.EIO
	}

	return 0
}

func (fs *DedupFS) removeInode(path string) int {
	logger.Debugf("remove: path=%s", path)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("remove: file not found: %s", path)
		return -fuse.EEXIST
	}

	// 获取当前用户 ID
	uid, gid, _ := fs.GetUidGid()

	// 查找要删除的
	node, exists := fs.RootNode.Get(ino)
	if !exists || node == nil {
		logger.Errorf("remove: file not found: %s", path)
		return -fuse.ENOENT
	}

	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("remove: failed to get inode: %v", err)
		return -fuse.EIO
	}

	// 检查权限
	if err := CheckPermission(inode, uid, gid, uint32(WritePermission)); err != nil {
		logger.Errorf("remove: permission denied: %v", err)
		return -fuse.EACCES
	}

	// 检查目录是否为空
	if inode.Kind == FileTypeDir && len(node.Children) > 0 {
		logger.Errorf("remove: directory not empty: %s, has %d childrens", path, len(node.Children))
		return -fuse.ENOTEMPTY
	}

	// 从存储中删除
	if err := DelINode(fs, ino); err != nil {
		logger.Errorf("remove: failed to delete inode: %v", err)
		return -fuse.EIO
	}

	return 0
}

// Unlink removes a file.
func (fs *DedupFS) Unlink(path string) int {
	logger.Debugf("remove: path=%s", path)
	path = _path.Clean(path)
	return fs.removeInode(path)
}

// Rmdir removes a directory.
func (fs *DedupFS) Rmdir(path string) int {
	logger.Debugf("rmdir: path=%s", path)
	path = _path.Clean(path)
	return fs.removeInode(path)
}

// Link creates a hard link to a file.
func (fs *DedupFS) Link(oldpath string, newpath string) int {
	logger.Errorf("link: oldpath=%s, newpath=%s - not supported", oldpath, newpath)

	return -fuse.ENOSYS
}

// Symlink creates a symbolic link.
func (fs *DedupFS) Symlink(target string, newpath string) int {
	logger.Debugf("symlink: target=%s, newpath=%s", target, newpath)
	newpath = _path.Clean(newpath)

	parentPath := _path.Dir(newpath)
	name := _path.Base(newpath)

	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	parentID := Path2Ino(fs, parentPath)
	if parentID == 0 {
		logger.Errorf("symlink: parent directory not found: %s", parentPath)
		return -fuse.ENOENT
	}

	// 获取当前用户 ID
	uid, gid, _ := fs.GetUidGid()

	// 检查父目录权限
	parentInode, err := GetINode(fs, parentID)
	if err != nil || parentInode == nil {
		logger.Errorf("symlink: parent directory not found: %s", parentPath)
		return -fuse.ENOENT
	}

	if err := CheckPermission(parentInode, uid, gid, uint32(WritePermission)); err != nil {
		logger.Errorf("symlink: permission denied: %v", err)
		return -fuse.EACCES
	}

	// 检查是否已存在
	parentNode, exists := fs.RootNode.Get(parentID)
	if !exists || parentNode == nil {
		logger.Errorf("symlink: parent directory not found: %s", parentPath)
		return -fuse.ENOENT
	}

	for _, child := range parentNode.Children {
		if child != nil {
			childInode, _ := GetINode(fs, child.ID)
			if childInode != nil && childInode.Name == name {
				logger.Errorf("symlink: file already exists: %s", newpath)
				return -fuse.EEXIST
			}
		}
	}

	// 创建符号链接节点
	symlinkTarget := target
	inode, err := fs.CreateNode(parentID, uid, gid, name, FileTypeSymlink, 0777, &symlinkTarget)
	if err != nil || inode == nil {
		logger.Errorf("symlink: failed to create directory: %v", err)
		return -fuse.EIO
	}

	return 0
}

// Readlink reads the target of a symbolic link.
func (fs *DedupFS) Readlink(path string) (int, string) {
	logger.Debugf("readlink: path=%s", path)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("readlink: link not found: %s", path)
		return -fuse.ENOENT, ""
	}

	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("readlink: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT, ""
	}

	if inode.Kind != FileTypeSymlink {
		logger.Errorf("file.readlink: not a symlink: %s", path)
		return -fuse.EINVAL, ""
	}

	if inode.SymlinkTarget == nil {
		logger.Errorf("file.readlink: invalid symlink: %s", path)
		return -fuse.EINVAL, ""
	}

	return 0, *inode.SymlinkTarget
}

// Rename renames a file.
func (fs *DedupFS) Rename(oldpath string, newpath string) int {
	logger.Debugf("rename: oldpath=%s, newpath=%s", oldpath, newpath)
	oldpath = _path.Clean(oldpath)
	newpath = _path.Clean(newpath)
	if oldpath == newpath {
		return 0
	}
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	newIno := Path2Ino(fs, newpath)
	if newIno != 0 {
		// 目标已经存在， windows下不支持覆盖
		logger.Errorf("rename: target already exists: %s", newpath)
		return -fuse.EEXIST
	}

	oldIno := Path2Ino(fs, oldpath)
	if oldIno == 0 {
		logger.Errorf("rename: path not found: %s", oldpath)
		return -fuse.ENOENT
	}

	newParent := _path.Dir(newpath)
	newParentIno := Path2Ino(fs, newParent)
	if newParentIno == 0 {
		logger.Errorf("rename: parent directory not found: %s", newParent)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, oldIno)
	if err != nil || inode == nil {
		logger.Errorf("rename: failed to get inode: %v", err)
		return -fuse.ENOENT
	}
	// 如果不是同一个目录，需要更改父子关系
	if inode.Parent != newParentIno {
		if err := fs.RootNode.Move(inode.Ino, newParentIno); err != nil {
			logger.Errorf("rename: failed to move node: %v", err)
			return -fuse.EIO
		}
	}
	inode.Name = _path.Base(newpath)
	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("rename: failed to save inode: %v", err)
		return -fuse.EIO
	}
	return 0
}

// Chmod changes the permission bits of a file.
func (fs *DedupFS) Chmod(path string, mode uint32) int {
	logger.Debugf("chmod: path=%s, mode=%o", path, mode)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("chmod: path not found: %s", path)
		return -fuse.ENOENT
	}

	uid, gid, _ := fs.GetUidGid()
	if err := CheckPermission(inode, uid, gid, uint32(WritePermission)); err != nil {
		logger.Errorf("chmod: permission denied: %v", err)
		return -fuse.EACCES
	}

	inode.Perm = uint16(mode & 0777)

	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("chmod: failed to save inode: %v", err)
		return -fuse.EIO
	}

	return 0
}

// Chown changes the owner and group of a file.
func (fs *DedupFS) Chown(path string, uid uint32, gid uint32) int {
	logger.Debugf("chown: path=%s, uid=%d, gid=%d", path, uid, gid)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("chown: path not found: %s", path)
		return -fuse.ENOENT
	}

	// 检查权限：只有 root 或文件所有者可以修改
	_uid, _gid, _ := fs.GetUidGid()
	if err := CheckPermission(inode, _uid, _gid, uint32(WritePermission)); err != nil {
		logger.Errorf("chown: permission denied: %v", err)
		return -fuse.EACCES
	}

	inode.Uid = uid
	inode.Gid = gid

	if err := SaveINode(fs, inode); err != nil {
		return -fuse.EIO
	}

	return 0
}

// Utimens changes the access and modification times of a file.
func (fs *DedupFS) Utimens(path string, tmsp []fuse.Timespec) int {
	logger.Debugf("utimens: path=%s, tmsp=%v", path, tmsp)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("utimens: path not found: %s", path)
		return -fuse.ENOENT
	}

	uid, gid, _ := fs.GetUidGid()
	if err := CheckPermission(inode, uid, gid, uint32(WritePermission)); err != nil {
		return -fuse.EACCES
	}

	now := time.Now().UTC()

	if len(tmsp) >= 2 {
		inode.Atime = tmsp[0].Time()
		inode.Mtime = tmsp[1].Time()
	} else {
		inode.Atime = now
		inode.Mtime = now
	}

	inode.Ctime = now

	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("utimens: failed to save inode: %v", err)
		return -fuse.EIO
	}

	return 0
}

// Access checks file access permissions.
func (fs *DedupFS) Access(path string, mask uint32) int {
	logger.Debugf("access: path=%s, mask=%o", path, mask)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("access: path not found: %s", path)
		return -fuse.ENOENT
	}

	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("access: failed to get inode: %v", err)
		return -fuse.ENOENT
	}
	uid, gid, _ := fs.GetUidGid()
	if err := CheckPermission(inode, uid, gid, mask); err != nil {
		return -fuse.EACCES
	}

	return 0
}

// Create creates and opens a file.
// The flags are a combination of the fuse.O_* constants.
func (fs *DedupFS) Create(path string, flags int, mode uint32) (int, uint64) {
	logger.Debugf("create: path=%s, flags=%d, mode=%o", path, flags, mode)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	parent := _path.Dir(path)
	name := _path.Base(path)
	parentIno := Path2Ino(fs, parent)
	if parentIno == 0 {
		logger.Errorf("create: parent directory not found: %s", parent)
		return -fuse.ENOENT, 0
	}
	ino := Path2Ino(fs, path)
	if ino != 0 {
		// 文件已存在，根据 flags 决定行为
		if flags&fuse.O_EXCL != 0 {
			// O_EXCL 标志要求文件必须不存在
			logger.Debugf("create with O_EXCL: file already exists: %s", path)
			return -fuse.EEXIST, 0
		} else {
			// 文件存在但没有 O_EXCL，正常打开
			logger.Debugf("create: opening existing file: %s", path)
			return fs.Open(path, flags)
		}
	}

	// 检查父目录权限
	parentInode, err := GetINode(fs, parentIno)
	if err != nil || parentInode == nil {
		logger.Errorf("create: failed to get parent inode: %v", err)
		return -fuse.ENOENT, 0
	}
	if err := CheckPermission(parentInode, uint32(parentInode.Uid), uint32(parentInode.Gid), uint32(WritePermission)); err != nil {
		logger.Errorf("create: permission denied: %v", err)
		return -fuse.EACCES, 0
	}

	uid, gid, _ := fs.GetUidGid()
	// 创建文件节点
	inode, err := fs.CreateNode(parentIno, uid, gid, name, FileTypeFile, os.FileMode(mode), nil)
	if err != nil {
		logger.Errorf("create: failed to create node: %v", err)
		return -fuse.EIO, 0
	}

	return 0, uint64(inode.Ino)
}

// Open opens a file.
// The flags are a combination of the fuse.O_* constants.
func (fs *DedupFS) Open(path string, flags int) (int, uint64) {
	logger.Debugf("open: path=%s, flags=%d", path, flags)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("open: path not found: %s", path)
		return -fuse.ENOENT, 0
	}
	if inode.Kind != FileTypeFile {
		logger.Errorf("open: not a file: %s", path)
		return -fuse.EINVAL, 0
	}
	mask := uint32(0)
	mode := flags & fuse.O_ACCMODE
	switch mode {
	case fuse.O_RDONLY:
		mask = uint32(ReadPermission)
	case fuse.O_WRONLY:
		mask = uint32(WritePermission)
	case fuse.O_RDWR:
		mask = uint32(ReadWritePermission)
	}
	uid, gid, _ := fs.GetUidGid()
	if err := CheckPermission(inode, uid, gid, mask); err != nil {
		logger.Errorf("open: permission denied: %v", err)
		return -fuse.EACCES, 0
	}

	//处理 O_TRUNC
	if flags&fuse.O_TRUNC != 0 {
		if mode == fuse.O_RDONLY {
			logger.Errorf("open: O_TRUNC not allowed for read-only file: %s", path)
			return -fuse.EACCES, 0
		}
		if err := inode.Truncate(fs, 0); err != nil {
			logger.Errorf("open: failed to truncate file: %v", err)
			return -fuse.EIO, 0
		}
	}
	return 0, uint64(inode.Ino)
}

// Getattr gets file attributes.
func (fs *DedupFS) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	logger.Debugf("getattr: path=%s", path)
	path = _path.Clean(path)

	// 忽略常见探测路径的日志
	if path == "/autorun.inf" || path == "/AutoRun.inf" || path == "/desktop.ini" {
		return fuse.ENOENT
	}

	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Debugf("getattr: path not found: %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Debugf("getattr: path not found: %s", path)
		return -fuse.ENOENT
	}

	// 设置 stat 结构
	stat.Ino = inode.Ino
	stat.Size = int64(inode.Size)
	stat.Blksize = 4096
	stat.Blocks = (stat.Size + int64(stat.Blksize) - 1) / int64(stat.Blksize)
	stat.Nlink = uint32(inode.Nlink)
	stat.Uid = inode.Uid
	stat.Gid = inode.Gid
	stat.Mode = uint32(0777)
	stat.Rdev = 0

	// 设置时间
	stat.Atim = fuse.NewTimespec(inode.Atime)
	stat.Mtim = fuse.NewTimespec(inode.Mtime)
	stat.Ctim = fuse.NewTimespec(inode.Ctime)

	// 设置文件类型
	switch inode.Kind {
	case FileTypeDir:
		stat.Mode |= 0040000 // S_IFDIR
	case FileTypeFile:
		stat.Mode |= 0100000 // S_IFREG
	case FileTypeSymlink:
		stat.Mode |= 0120000 // S_IFLNK
	}

	return 0
}

// Truncate changes the size of a file.
func (fs *DedupFS) Truncate(path string, size int64, fh uint64) int {
	logger.Debugf("truncate: path=%s, size=%d", path, size)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("truncate: path not found: %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("truncate: path not found: %s", path)
		return -fuse.ENOENT
	}

	uid, gid, _ := fs.GetUidGid()
	if err := CheckPermission(inode, uid, gid, uint32(WritePermission)); err != nil {
		logger.Errorf("truncate: permission denied: %v", err)
		return -fuse.EACCES
	}
	if err := inode.Truncate(fs, uint64(size)); err != nil {
		logger.Errorf("truncate: failed to truncate file: %v", err)
		return -fuse.EIO
	}
	return 0
}

// Read reads data from a file.
func (fs *DedupFS) Read(path string, buff []byte, ofst int64, fh uint64) int {
	logger.Debugf("read: path=%s, offset=%d, size=%d", path, ofst, len(buff))
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("read: path not found: %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("read: path not found: %s", path)
		return -fuse.ENOENT
	}

	uid, gid, _ := fs.GetUidGid()
	if err := CheckPermission(inode, uid, gid, uint32(ReadPermission)); err != nil {
		logger.Errorf("read: permission denied: %v", err)
		return -fuse.EACCES
	}

	data := inode.Read(ofst, len(buff), fs)
	if data == nil {
		return -fuse.EIO
	}

	copy(buff, data)
	return len(data)
}

// Write writes data to a file.
func (fs *DedupFS) Write(path string, buff []byte, ofst int64, fh uint64) int {
	logger.Debugf("write: path=%s, offset=%d, size=%d", path, ofst, len(buff))
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("write: path not found: %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("write: path not found: %s", path)
		return -fuse.ENOENT
	}
	uid, gid, _ := fs.GetUidGid()
	if err := CheckPermission(inode, uid, gid, uint32(WritePermission)); err != nil {
		logger.Errorf("write: permission denied: %v", err)
		return -fuse.EACCES
	}
	if err := inode.Write(fs, ofst, buff); err != nil {
		logger.Errorf("write: failed to write file: %v", err)
		return -fuse.EIO
	}
	return len(buff)
}

// Flush flushes cached file data.
func (fs *DedupFS) Flush(path string, fh uint64) int {
	logger.Debugf("flush: path=%s", path)
	return 0
}

// Release closes an open file.
func (fs *DedupFS) Release(path string, fh uint64) int {
	logger.Debugf("release: path=%s", path)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		return 0
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		return 0
	}

	if err := FlushINode(fs, inode); err != nil {
		logger.Errorf("file.release: failed to save inode: %v", err)
		return fuse.EIO
	}
	return 0
}

// Fsync synchronizes file contents.
func (fs *DedupFS) Fsync(path string, datasync bool, fh uint64) int {
	logger.Debugf("fsync: path=%s", path)
	return 0
}

// Opendir opens a directory.
func (fs *DedupFS) Opendir(path string) (int, uint64) {
	logger.Debugf("opendir: path=%s", path)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("opendir: path not found: %s", path)
		return -fuse.ENOENT, 0
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("opendir: path not found: %s", path)
		return -fuse.ENOENT, 0
	}
	return 0, uint64(inode.Ino)
}

// Readdir reads a directory.
func (fs *DedupFS) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	logger.Debugf("readdir: path=%s", path)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("readdir: path not found: %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("readdir: path not found: %s", path)
		return -fuse.ENOENT
	}
	node, ok := fs.RootNode.Get(inode.Ino)
	if !ok || node == nil || node.ID != inode.Ino {
		logger.Errorf("readdir: path not found: %s", path)
		return -fuse.ENOENT
	}

	// 添加 . 和 ..
	if !fill(".", nil, 0) {
		return 0
	}
	if !fill("..", nil, 1) {
		return 0
	}

	// 添加子节点
	for i, child := range node.Children {
		if child != nil {
			if cn, err := GetINode(fs, child.ID); err != nil || cn == nil {
				logger.Errorf("readdir: path not found: %s", path)
				return -fuse.ENOENT
			} else {
				if !fill(cn.Name, nil, int64(i+2)) {
					break
				}
			}
		}
	}

	return 0
}

// Releasedir closes an open directory.
func (fs *DedupFS) Releasedir(path string, fh uint64) int {
	logger.Debugf("releasedir: path=%s", path)
	return 0
}

// Fsyncdir synchronizes directory contents.
func (fs *DedupFS) Fsyncdir(path string, datasync bool, fh uint64) int {
	logger.Debugf("fsyncdir: path=%s", path)
	return 0
}

// Setxattr sets extended attributes.
func (fs *DedupFS) Setxattr(path string, name string, value []byte, flags int) int {
	logger.Debugf("setxattr: path=%s, name=%s, flags=%d", path, name, flags)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("setxattr: path not found: %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("setxattr: path not found: %s", path)
		return -fuse.ENOENT
	}
	if err := inode.SetXattr(name, value); err != nil {
		logger.Errorf("setxattr: failed to set xattr: %v", err)
		return -fuse.EIO
	}
	return 0
}

// Getxattr gets extended attributes.
func (fs *DedupFS) Getxattr(path string, name string) (int, []byte) {
	logger.Debugf("getxattr: path=%s, name=%s", path, name)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("getxattr: path not found: %s", path)
		return -fuse.ENOENT, nil
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("getxattr: path not found: %s", path)
		return -fuse.ENOENT, nil
	}
	value, err := inode.GetXattr(name)
	if err != nil {
		logger.Errorf("getxattr: failed to get xattr: %v", err)
		return -fuse.EIO, nil
	}
	return len(value), value
}

// Removexattr removes extended attributes.
func (fs *DedupFS) Removexattr(path string, name string) int {
	logger.Debugf("removexattr: path=%s, name=%s", path, name)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("removexattr: path not found: %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("removexattr: path not found: %s", path)
		return -fuse.ENOENT
	}
	if err := inode.RemoveXattr(name); err != nil {
		logger.Errorf("removexattr: failed to remove xattr: %v", err)
		return -fuse.EIO
	}
	return 0
}

// Listxattr lists extended attributes.
func (fs *DedupFS) Listxattr(path string, fill func(name string) bool) int {
	logger.Debugf("listxattr: path=%s", path)
	path = _path.Clean(path)
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	ino := Path2Ino(fs, path)
	if ino == 0 {
		logger.Errorf("listxattr: path not found: %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, ino)
	if err != nil || inode == nil {
		logger.Errorf("listxattr: path not found: %s", path)
		return -fuse.ENOENT
	}
	names, err := inode.ListXattr()
	if err != nil {
		logger.Errorf("listxattr: failed to list xattr: %v", err)
		return -fuse.EIO
	}
	for _, name := range names {
		if !fill(name) {
			break
		}
	}
	return 0
}
