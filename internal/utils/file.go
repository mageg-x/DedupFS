//go:build linux || darwin

package utils

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// FSStats 包含文件系统统计信息
type FSStats struct {
	Bsize   uint64 // 文件系统块大小
	Blocks  uint64 // 总数据块数
	Bfree   uint64 // 空闲块数
	Bavail  uint64 // 可用块数（非超级用户）
	Ffree   uint64 // 空闲文件节点数
	Frsize  uint64 // 基本块大小
	Namelen uint64 // 最大文件名长度
}

// FileStat 包含跨平台的文件统计信息
type FileStat struct {
	Ino     uint64      // 文件的inode编号
	Size    int64       // 文件大小
	Mode    os.FileMode // 文件模式
	ModTime int64       // 修改时间
	UID     uint32      // 用户ID
	GID     uint32      // 组ID
}

func IsDirEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	_, err = f.Readdirnames(1) // 尝试读取一个条目
	if err == io.EOF {
		return true, nil // 空
	}
	return false, nil // 非空（或错误，但通常视为非空）
}

func ListAllFiles(root string) ([]string, error) {
	var files []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err // 跳过无法访问的路径（或返回 err 中止）
		}
		if !d.IsDir() {
			files = append(files, path)
		}
		return nil
	})

	return files, err
}

// 使用Linux特定的系统调用获取文件统计信息
func FsStat(path string) (*FileStat, error) {
	var stat unix.Stat_t
	if err := unix.Stat(path, &stat); err != nil {
		return nil, fmt.Errorf("failed to stat %s: %w", path, err)
	}

	return &FileStat{
		Ino:     stat.Ino,
		Size:    int64(stat.Size),
		Mode:    os.FileMode(stat.Mode),
		ModTime: int64(stat.Mtim.Sec),
		UID:     stat.Uid,
		GID:     stat.Gid,
	}, nil
}

// FsStats 使用Linux特定的系统调用获取文件系统统计
func FsStats(path string) (*FSStats, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return nil, fmt.Errorf("failed to statfs %s: %w", path, err)
	}

	return &FSStats{
		Bsize:   uint64(stat.Bsize),
		Blocks:  stat.Blocks,
		Bfree:   stat.Bfree,
		Bavail:  stat.Bavail,
		Ffree:   stat.Ffree,
		Frsize:  uint64(stat.Bsize),
		Namelen: 255, // Linux通常支持255字符的文件名长度
	}, nil
}

func GetXAttr(path, attrName string) ([]byte, error) {
	// 第一次调用：获取所需 buffer 大小
	size, err := unix.Getxattr(path, attrName, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get xattr size: %w", err)
	}

	// 分配 buffer
	buf := make([]byte, size)

	// 第二次调用：实际读取值
	n, err := unix.Getxattr(path, attrName, buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read xattr: %w", err)
	}

	return buf[:n], nil
}
