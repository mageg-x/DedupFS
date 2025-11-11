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
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/winfsp/cgofuse/fuse"

	"github.com/mageg-x/dedupfs/common/kvstore"
	"github.com/mageg-x/dedupfs/common/utils"
)

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
		openmap:    make(map[uint64]uint64),
	}

	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	fs.NextNodeID.Store(1)
	fs.NextHandle.Store(1)

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
		root = CreateINode(1, 1, 1, 1, FileTypeDir, "/", 0777|fuse.S_IFDIR)
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

func (fs *DedupFS) Inode2Stat(inode *INode) *fuse.Stat_t {
	var stat fuse.Stat_t
	// 设置 stat 结构
	stat.Ino = inode.Ino
	stat.Size = int64(inode.Size)
	stat.Blksize = 4096
	stat.Blocks = (stat.Size + int64(stat.Blksize) - 1) / int64(stat.Blksize)
	stat.Nlink = inode.Nlink
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
		stat.Mode = fuse.S_IFDIR | 0777
	case FileTypeFile:
		stat.Mode = fuse.S_IFREG | 0666
	case FileTypeSymlink:
		stat.Mode = fuse.S_IFLNK | 0777
	default:
		logger.Errorf("getattr: unknown file type  %v of %s", inode.Kind, inode.Name)
	}
	return &stat
}

func (fs *DedupFS) Mknod(path string, mode uint32, dev uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Mknod: %s, mode: %d, dev: %d", path, mode, dev)
	return fs.makeNode(path, mode, nil)
}

func (fs *DedupFS) Mkdir(path string, mode uint32) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Mkdir: %s, mode: %d", path, mode)
	return fs.makeNode(path, fuse.S_IFDIR|(mode&07777), nil)
}

func (fs *DedupFS) Unlink(path string) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Unlink: %s", path)
	return fs.removeNode(path)
}

func (fs *DedupFS) Rmdir(path string) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Rmdir: %s", path)
	return fs.removeNode(path)
}

func (fs *DedupFS) Link(oldpath string, newpath string) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Link: %s, %s", oldpath, newpath)
	_, _, oldnode := fs.lookup(oldpath)
	if nil == oldnode {
		logger.Errorf("file.lookup: %s not found", oldpath)
		return -fuse.ENOENT
	}
	newparent, _, newnode := fs.lookup(newpath)
	if nil == newparent {
		logger.Errorf("dir.lookup: parent directory node not found: %d", newpath)
		return -fuse.ENOENT
	}
	if nil != newnode {
		logger.Errorf("file.lookup: file node already exists: %s", newpath)
		return -fuse.EEXIST
	}
	// 这里需要 copy 一份
	return 0
}

func (fs *DedupFS) Symlink(target string, newpath string) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Symlink: %s, %s", target, newpath)
	return fs.makeNode(newpath, fuse.S_IFLNK|00777, []byte(target))
}

func (fs *DedupFS) Readlink(path string) (int, string) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Readlink: %s", path)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("file.lookup: %s not found", path)
		return -fuse.ENOENT, ""
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("readlink: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT, ""
	}
	if inode.Kind != FileTypeSymlink {
		logger.Errorf("file.readlink: %sis not a symlink", path)
		return -fuse.EINVAL, ""
	}
	if inode.SymlinkTarget == nil {
		logger.Errorf("file.readlink: invalid symlink: %s", path)
		return -fuse.EINVAL, ""
	}
	return 0, *inode.SymlinkTarget
}

func (fs *DedupFS) Rename(oldpath string, newpath string) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("Rename: %s, %s", oldpath, newpath)

	oldparent, oldname, oldnode := fs.lookup(oldpath)
	if nil == oldnode {
		logger.Errorf("file.lookup: %s not found", oldpath)
		return -fuse.ENOENT
	}
	newparent, newname, newnode := fs.lookup(newpath)
	if nil == newparent {
		logger.Errorf("dir.lookup: parent directory node not found: %d", newpath)
		return -fuse.ENOENT
	}
	if "" == newname {
		logger.Errorf("file.rename: invalid new path: %s", newpath)
		return -fuse.EINVAL
	}
	if oldparent.ID == newparent.ID && oldname == newname {
		return 0
	}

	inode, err := GetINode(fs, oldnode.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.rename: failed to get inode %s : %v", oldpath, err)
		return -fuse.ENOENT
	}

	// 如果目标存在，先删除
	if nil != newnode {
		if 0 != fs.removeNode(newpath) {
			logger.Errorf("file.rename: failed to remove node %s", newpath)
			return -fuse.EIO
		}
	}

	// 如果不是同一个目录，需要更改父子关系
	if oldparent.ID != newparent.ID {
		if err := fs.RootNode.Move(oldnode.ID, newparent.ID); err != nil {
			logger.Errorf("dir.rename: failed to move node: %v", err)
			return -fuse.EIO
		}
	}

	inode.Name = newname
	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("rename: failed to save inode: %v", err)
		return -fuse.EIO
	}

	oldnode.Name = inode.Name
	fs.RootNode.Set(oldnode)
	return 0
}

func (fs *DedupFS) Chmod(path string, mode uint32) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Chmod: %s, mode: %d", path, mode)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("file.chmod: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.chmod: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}

	inode.Mode = (inode.Mode & fuse.S_IFMT) | mode&07777
	inode.Ctime = time.Now()

	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("chmod: failed to save inode: %v", err)
		return -fuse.EIO
	}
	return 0
}

func (fs *DedupFS) Chown(path string, uid uint32, gid uint32) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Chown: %s, uid: %d, gid: %d", path, uid, gid)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("file.chown: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.chown: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}

	if ^uint32(0) != uid {
		inode.Uid = uid
	}
	if ^uint32(0) != gid {
		inode.Gid = gid
	}
	inode.Ctime = time.Now()
	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("chown: failed to save inode: %v", err)
		return -fuse.EIO
	}
	return 0
}

func (fs *DedupFS) Utimens(path string, tmsp []fuse.Timespec) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Utimens: %s, tmsp: %v", path, tmsp)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("file.utimens: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.utimens: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}

	now := time.Now().UTC()

	if len(tmsp) >= 2 {
		inode.Atime = tmsp[0].Time()
		inode.Mtime = tmsp[1].Time()
	} else {
		inode.Atime = now
		inode.Mtime = now
	}
	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("utimens: failed to save inode: %v", err)
		return -fuse.EIO
	}

	return 0
}

func (fs *DedupFS) Open(path string, flags int) (int, uint64) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Open: %s, flags: %d", path, flags)
	return fs.openNode(path, flags)
}

func (fs *DedupFS) Getattr(path string, stat *fuse.Stat_t, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("Getattr: path  %s, fh %d", path, fh)
	node := fs.getNode(path, fh)
	if nil == node {
		logger.Errorf("getattr: path %s, fh :%d  not found", path, fh)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.getattr: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	s := fs.Inode2Stat(inode)
	*stat = *s
	logger.Debugf("Getattr: %s, stat: %#v", path, *s)
	return 0
}

func (fs *DedupFS) Truncate(path string, size int64, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("Truncate: %s, size: %d", path, size)
	node := fs.getNode(path, fh)
	if nil == node {
		logger.Errorf("file.truncate: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.truncate: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	if err := inode.Truncate(fs, uint64(size)); err != nil {
		logger.Errorf("truncate: failed to truncate file: %v", err)
		return -fuse.EIO
	}
	return 0
}

func (fs *DedupFS) Read(path string, buff []byte, ofst int64, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("Read: %s, offset: %d, size: %d", path, ofst, len(buff))
	node := fs.getNode(path, fh)
	if nil == node {
		logger.Errorf("file.read: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.read: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}

	data, err := inode.Read(ofst, len(buff), fs)
	if err != nil {
		logger.Errorf("file.read: failed to read file %s, ofst %d, fh %d, error %v", path, ofst, fh, err)
		return -fuse.EIO
	}

	copy(buff, data)
	return len(data)
}

func (fs *DedupFS) Write(path string, buff []byte, ofst int64, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("Write: %s, offset: %d, size: %d", path, ofst, len(buff))
	node := fs.getNode(path, fh)
	if nil == node {
		logger.Errorf("file.write: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.write: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}

	if err := inode.Write(fs, ofst, buff); err != nil {
		logger.Errorf("write: failed to write file: %v", err)
		return -fuse.EIO
	}
	return len(buff)
}

func (fs *DedupFS) Release(path string, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("Release: %s", path)
	node := fs.getNode(path, fh)
	if nil == node {
		logger.Errorf("file.write: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.write: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	if err := FlushINode(fs, inode); err != nil {
		logger.Errorf("file.release: failed to save inode: %v", err)
		return fuse.EIO
	}
	fs.closeNode(fh)
	return 0
}

func (fs *DedupFS) Opendir(path string) (int, uint64) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("Opendir: %s", path)
	return fs.openNode(path, 0)
}

func (fs *DedupFS) Readdir(path string, fill func(name string, stat *fuse.Stat_t, ofst int64) bool, ofst int64, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("Readdir: %s", path)
	node := fs.getNode(path, fh)
	if nil == node {
		logger.Errorf("file.write: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.write: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	// 添加 . 和 ..
	stat := fs.Inode2Stat(inode)
	if !fill(".", stat, 0) {
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
				cstat := fs.Inode2Stat(cn)
				if !fill(cn.Name, cstat, int64(i+2)) {
					break
				}
			}
		}
	}

	return 0
}

func (fs *DedupFS) Releasedir(path string, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("releasedir: %s", path)
	fs.closeNode(fh)
	return 0
}

func (fs *DedupFS) Setxattr(path string, name string, value []byte, flags int) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("setxattr: %s, name: %s, value: %s, flags: %d", path, name, value, flags)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("setxattr: %s not found", path)
		return -fuse.ENOENT
	}
	if "com.apple.ResourceFork" == name {
		logger.Errorf("setxattr: com.apple.ResourceFork not supported")
		return -fuse.ENOTSUP
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("setxattr: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	if err := inode.SetXattr(name, value); err != nil {
		logger.Errorf("setxattr: failed to set xattr: %v", err)
		return -fuse.EIO
	}

	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("setxattr: failed to save inode: %v", err)
		return -fuse.EIO
	}

	return 0
}

func (fs *DedupFS) Getxattr(path string, name string) (int, []byte) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("getxattr: %s, name: %s", path, name)

	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("getxattr: %s not found", path)
		return -fuse.ENOENT, nil
	}
	if "com.apple.ResourceFork" == name {
		logger.Errorf("getxattr: com.apple.ResourceFork not supported")
		return -fuse.ENOTSUP, nil
	}
	return fs.Xattr(path, name)
}

func (fs *DedupFS) Removexattr(path string, name string) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("removexattr: %s, name: %s", path, name)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("removexattr: %s not found", path)
		return -fuse.ENOENT
	}
	if "com.apple.ResourceFork" == name {
		logger.Errorf("removexattr: com.apple.ResourceFork not supported")
		return -fuse.ENOTSUP
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("file.removexattr: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	if err := inode.RemoveXattr(name); err != nil {
		logger.Errorf("removexattr: failed to remove xattr: %v", err)
		return -fuse.EIO
	}
	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("file.removexattr: failed to save inode: %v", err)
		return -fuse.EIO
	}
	return 0
}

func (fs *DedupFS) Listxattr(path string, fill func(name string) bool) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("listxattr: %s", path)

	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("listxattr: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("listxattr: failed to get inode %s : %v", path, err)
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

func (fs *DedupFS) Chflags(path string, flags uint32) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("chflags: %s, flags: %d", path, flags)

	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("chflags: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("chflags: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	inode.Flags = flags
	inode.Ctime = time.Now()
	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("chflags: failed to save inode: %v", err)
		return -fuse.EIO
	}
	return 0
}

func (fs *DedupFS) Setcrtime(path string, tmsp fuse.Timespec) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("setcrtime: %s, tmsp: %v", path, tmsp)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("setcrtime: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("setcrtime: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	inode.Crtime = tmsp.Time()
	inode.Ctime = time.Now()
	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("chflags: failed to save inode: %v", err)
		return -fuse.EIO
	}
	return 0
}

func (fs *DedupFS) Setchgtime(path string, tmsp fuse.Timespec) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()

	logger.Debugf("setchgtime: %s, tmsp: %v", path, tmsp)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("setchgtime: %s not found", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("setchgtime: failed to get inode %s : %v", path, err)
		return -fuse.ENOENT
	}
	inode.Ctime = time.Now()

	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("chflags: failed to save inode: %v", err)
		return -fuse.EIO
	}
	return 0
}

func (fs *DedupFS) lookup(path string) (parent *Node, name string, node *Node) {
	newpath := _path.Clean(path)
	parentPath := _path.Dir(path)
	name = _path.Base(newpath)
	pino := Path2Ino(fs, parentPath)
	ino := Path2Ino(fs, newpath)
	if pino != 0 {
		parent, _ = fs.RootNode.Get(pino)
	}
	if ino != 0 {
		node, _ = fs.RootNode.Get(ino)
	}
	return parent, name, node
}

func (fs *DedupFS) makeNode(path string, mode uint32, data []byte) int {
	logger.Errorf("makeNode= %s, mode = %d", path, mode)
	pnode, name, node := fs.lookup(path)
	if nil == pnode {
		logger.Errorf("parent node not found for %s", path)
		return -fuse.ENOENT
	}
	if nil != node {
		logger.Errorf("node already exists for %s, %#v", path, *node)
		return -fuse.EEXIST
	}

	var symlinkTarget *string
	uid, gid, _ := fuse.Getcontext()
	ftype := FileTypeFile
	if fuse.S_IFDIR == mode&fuse.S_IFMT {
		ftype = FileTypeDir
	} else if fuse.S_IFLNK == mode&fuse.S_IFMT {
		ftype = FileTypeSymlink
		if len(data) > 0 {
			s := string(data)
			symlinkTarget = &s
		}
	}

	// 创建节点
	inode, err := fs.CreateNode(pnode.ID, uid, gid, name, ftype, os.FileMode(mode), symlinkTarget)
	if err != nil || inode == nil {
		logger.Errorf("mkdir: failed to create directory: %v", err)
		return -fuse.EIO
	}

	return 0
}

func (fs *DedupFS) removeNode(path string) int {
	logger.Debugf("removeNode= %s", path)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("node not found for %s", path)
		return -fuse.ENOENT
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("failed to get inode: %v", err)
		return -fuse.EIO
	}
	if inode.Kind == FileTypeDir && len(node.Children) > 0 {
		logger.Errorf("directory not empty: %s", path)
		return -fuse.ENOTEMPTY
	}

	// 从存储中删除
	if err := DelINode(fs, node.ID); err != nil {
		logger.Errorf("remove: failed to delete inode: %v", err)
		return -fuse.EIO
	}
	return 0
}

func (fs *DedupFS) openNode(path string, flags int) (int, uint64) {
	logger.Debugf("openNode= %s, flags = %d", path, flags)
	_, _, node := fs.lookup(path)
	if nil == node {
		logger.Errorf("node not found for %s", path)
		return -fuse.ENOENT, ^uint64(0)
	}
	inode, err := GetINode(fs, node.ID)
	if err != nil || inode == nil {
		logger.Errorf("failed to get inode: %v", err)
		return -fuse.EIO, ^uint64(0)
	}

	h := fs.NextHandle.Add(1)
	fs.openmap[uint64(h)] = node.ID

	//处理 O_TRUNC
	if flags&fuse.O_TRUNC != 0 {
		if err := inode.Truncate(fs, 0); err != nil {
			delete(fs.openmap, uint64(h))
			logger.Errorf("truncate: failed to truncate inode: %v", err)
			return -fuse.EIO, 0
		}
	}
	return 0, uint64(h)
}

func (fs *DedupFS) closeNode(fh uint64) int {
	logger.Debugf("closeNode= %d", fh)
	delete(fs.openmap, fh)
	return 0
}

func (fs *DedupFS) getNode(path string, fh uint64) *Node {
	if ^uint64(0) == fh {
		_, _, node := fs.lookup(path)
		return node
	} else {
		ino := fs.openmap[fh]
		if ino == 0 {
			logger.Errorf("node not found for fh %d", fh)
			return nil
		}
		node, _ := fs.RootNode.Get(ino)
		return node
	}
}

func (fs *DedupFS) Statfs(path string, stat *fuse.Statfs_t) int {
	logger.Debugf("dedupfs.statfs")
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

func (fs *DedupFS) Access(path string, mask uint32) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("access= %s, mask = %d", path, mask)
	return 0
}
func (fs *DedupFS) Create(path string, flags int, mode uint32) (int, uint64) {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Errorf("create= %s, flags = %d, mode = %d", path, flags, mode)

	_, _, node := fs.lookup(path)
	if node == nil {
		if (flags & fuse.O_CREAT) != 0 {
			ret := fs.makeNode(path, mode, nil)
			if ret != 0 {
				logger.Errorf("create: failed to create file: %v", ret)
				return ret, ^uint64(0)
			}
		} else {
			logger.Errorf("node not found for %s", path)
			return -fuse.ENOENT, ^uint64(0)
		}
	} else {
		if (flags & (fuse.O_CREAT | fuse.O_EXCL)) == (fuse.O_CREAT | fuse.O_EXCL) {
			logger.Errorf("file already exists: %s", path)
			return -fuse.EEXIST, ^uint64(0)
		}
	}

	return fs.openNode(path, flags)
}
func (fs *DedupFS) Flush(path string, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("flush= %s, fh = %d", path, fh)
	return 0
}
func (fs *DedupFS) Fsync(path string, datasync bool, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("fsync= %s, datasync = %t, fh = %d", path, datasync, fh)
	return 0
}

func (fs *DedupFS) Fsyncdir(path string, datasync bool, fh uint64) int {
	fs.mutex.Lock()
	defer fs.mutex.Unlock()
	logger.Debugf("fsyncdir= %s, datasync = %t, fh = %d", path, datasync, fh)
	return 0
}

func (fs *DedupFS) Init() {
	logger.Debugf("init")
}
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
