//go:build windows

package utils

import (
	"os/exec"
	"regexp"
	"strconv"
	"syscall"
	"unsafe"
)

const MAX_PATH = 260

// 定义Windows API函数
var (
	modkernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetDiskFreeSpaceExW   = modkernel32.NewProc("GetDiskFreeSpaceExW")
	procGetVolumeInformationW = modkernel32.NewProc("GetVolumeInformationW")
)

// GetDiskFreeSpaceEx 使用Windows API获取磁盘空间信息
func GetDiskFreeSpaceEx(path string) (totalBytes, freeBytes, totalAvailableBytes uint64, err error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, 0, err
	}

	ret, _, err := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&totalAvailableBytes)),
		uintptr(unsafe.Pointer(&totalBytes)),
		uintptr(unsafe.Pointer(&freeBytes)),
	)
	if ret == 0 {
		return 0, 0, 0, err
	}
	return totalBytes, freeBytes, totalAvailableBytes, nil
}

// GetVolumeInformation 获取卷信息，例如文件系统名和最大文件名长度
func GetVolumeInformation(path string) (fileSystemName string, maxComponentLength uint32, err error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", 0, err
	}

	var volumeNameBuffer [MAX_PATH + 1]uint16
	var fileSystemNameBuffer [MAX_PATH + 1]uint16
	var volumeSerialNumber, maximumComponentLength, fileSystemFlags uint32

	ret, _, err := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&volumeNameBuffer[0])),
		uintptr(MAX_PATH+1),
		uintptr(unsafe.Pointer(&volumeSerialNumber)),
		uintptr(unsafe.Pointer(&maximumComponentLength)),
		uintptr(unsafe.Pointer(&fileSystemFlags)),
		uintptr(unsafe.Pointer(&fileSystemNameBuffer[0])),
		uintptr(MAX_PATH+1),
	)
	if ret == 0 {
		return "", 0, err
	}

	fileSystemName = syscall.UTF16ToString(fileSystemNameBuffer[:])
	return fileSystemName, maximumComponentLength, nil
}

// GetFsutilInfo 通过fsutil获取NTFS详细信息，如总簇数、可用簇数、每簇字节数[citation:4]
func GetFsutilInfo(mountPoint string) (totalClusters, freeClusters, clustersPerMftRecord uint64, bytesPerCluster uint64, err error) {
	cmd := exec.Command("fsutil", "fsinfo", "ntfsinfo", mountPoint)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return 0, 0, 0, 0, err
	}

	outputStr := string(output)
	// 使用正则表达式提取关键信息
	reTotalClusters := regexp.MustCompile(`Total Clusters\s*:\s*(\d+)`)
	reFreeClusters := regexp.MustCompile(`Free Clusters\s*:\s*(\d+)`)
	reBytesPerCluster := regexp.MustCompile(`Bytes Per Cluster\s*:\s*(\d+)`)
	reClustersPerMftRecord := regexp.MustCompile(`Clusters Per MFT Record\s*:\s*(\d+)`)

	if match := reTotalClusters.FindStringSubmatch(outputStr); match != nil {
		totalClusters, _ = strconv.ParseUint(match[1], 10, 64)
	}
	if match := reFreeClusters.FindStringSubmatch(outputStr); match != nil {
		freeClusters, _ = strconv.ParseUint(match[1], 10, 64)
	}
	if match := reBytesPerCluster.FindStringSubmatch(outputStr); match != nil {
		bytesPerCluster, _ = strconv.ParseUint(match[1], 10, 64)
	}
	if match := reClustersPerMftRecord.FindStringSubmatch(outputStr); match != nil {
		clustersPerMftRecord, _ = strconv.ParseUint(match[1], 10, 64)
	}

	return totalClusters, freeClusters, clustersPerMftRecord, bytesPerCluster, nil
}
