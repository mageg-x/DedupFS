package utils

import (
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

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
