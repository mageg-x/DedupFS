package dfs

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mageg-x/dedupfs/internal/cache"
	"github.com/mageg-x/dedupfs/internal/kvstore"
)

var (
	G_INODE_CACHE = cache.NewCache[string, *INode](1024*1024*1024, false)
)

// FileType 定义文件类型
type FileType string

const (
	FileTypeFile    FileType = "file"
	FileTypeDir     FileType = "dir"
	FileTypeSymlink FileType = "symlink"
)

// INodeChunk 表示数据块
type INodeChunk struct {
	Hash string `msgpack:"hash" json:"hash"`
	Data []byte `msgpack:"-" json:"-"` // 不序列化
}

// INode 是完整的 inode 元数据
type INode struct {
	Ino           uint64            `msgpack:"ino" json:"ino"`
	Size          uint64            `msgpack:"size" json:"size"`
	Blocks        uint64            `msgpack:"blocks" json:"blocks"`
	Atime         time.Time         `msgpack:"atime" json:"atime"`
	Mtime         time.Time         `msgpack:"mtime" json:"mtime"`
	Ctime         time.Time         `msgpack:"ctime" json:"ctime"`
	Crtime        time.Time         `msgpack:"crtime" json:"crtime"`
	Kind          FileType          `msgpack:"kind" json:"kind"`
	Perm          uint16            `msgpack:"perm" json:"perm"`
	Nlink         uint32            `msgpack:"nlink" json:"nlink"`
	Uid           uint32            `msgpack:"uid" json:"uid"`
	Gid           uint32            `msgpack:"gid" json:"gid"`
	Rdev          uint32            `msgpack:"rdev" json:"rdev"`
	Blksize       uint32            `msgpack:"blksize" json:"blksize"`
	Flags         uint32            `msgpack:"flags" json:"flags"`
	Name          string            `msgpack:"name" json:"name"`
	Parent        uint64            `msgpack:"parent" json:"parent"`
	Xattr         map[string][]byte `msgpack:"xattr" json:"xattr"`
	Chunks        []*INodeChunk     `msgpack:"chunks,omitempty" json:"chunks,omitempty"`
	DropChunks    []*INodeChunk     `msgpack:"drop_chunks,omitempty" json:"drop_chunks,omitempty"`
	SymlinkTarget *string           `msgpack:"symlink_target,omitempty" json:"symlink_target,omitempty"`
}

func (n *INode) Len() int {
	return int(n.Size) + 160
}

func (n *INode) Read(offset int64, size int, fs *DedupFS) []byte {
	logger.Debugf("reading data from inode %d: offset=%d, size=%d", n.Ino, offset, size)

	// 边界检查
	if offset >= int64(n.Size) || size == 0 || len(n.Chunks) == 0 {
		logger.Debugf("reading no data from inode %d: offset=%d, size=%d, file_size=%d",
			n.Ino, offset, size, n.Size)
		return nil // 或 return []byte{}
	}

	maxBytesToRead := int64(size)
	if remaining := (int64(n.Size) - offset); remaining < int64(size) {
		maxBytesToRead = remaining
	}
	if maxBytesToRead <= 0 {
		return nil
	}

	result := make([]byte, 0, maxBytesToRead)
	currentOffset := uint64(0)

	logger.Debugf("reading from %d chunks in inode %d", len(n.Chunks), n.Ino)

	readStart := offset
	readEnd := offset + maxBytesToRead

	for _, chunk := range n.Chunks {
		var chunkSize int
		if len(chunk.Data) > 0 {
			chunkSize = len(chunk.Data)
		} else {
			// 从元数据获取 chunk 大小
			c, err := GetChunkMeta(chunk.Hash, fs)
			if err != nil || c == nil {
				logger.Errorf("failed to get chunk metadata for hash %s: %v", chunk.Hash, err)
				return nil
			}

			if c.Size < 0 {
				logger.Errorf("invalid chunk size %d for hash %s", c.Size, chunk.Hash)
				return nil
			}
			chunkSize = int(c.Size)
		}

		chunkStart := currentOffset
		chunkEnd := currentOffset + uint64(chunkSize)

		// 检查是否与读取范围重叠：[chunkStart, chunkEnd) ∩ [readStart, readEnd)
		if chunkEnd <= uint64(readStart) || int64(chunkStart) >= readEnd {
			currentOffset = chunkEnd
			continue
		}

		// 计算在当前 chunk 中的读取范围
		startInChunk := int64(0)
		if chunkStart < uint64(readStart) {
			startInChunk = readStart - int64(chunkStart)
		}

		endInChunk := int64(chunkSize)
		if chunkEnd > uint64(readEnd) {
			endInChunk = readEnd - int64(chunkStart)
		}

		if startInChunk >= endInChunk {
			currentOffset = chunkEnd
			continue
		}

		// 获取 chunk 数据（优先使用缓存）
		var data []byte
		if len(chunk.Data) > 0 {
			data = chunk.Data
		} else {
			fullChunk, err := GetChunkData(chunk.Hash, fs)
			if err != nil || fullChunk == nil {
				logger.Errorf("failed to get chunk data for hash %s: %v", chunk.Hash, err)
				return nil
			}
			data = fullChunk.Data
			// 可选：验证 data 长度是否匹配 chunkSize
			if len(data) != chunkSize {
				logger.Errorf("chunk data length mismatch: expected %d, got %d for hash %s", chunkSize, len(data), chunk.Hash)
				return nil
			}
		}

		// 截取所需片段
		toAppend := data[startInChunk:endInChunk]
		result = append(result, toAppend...)

		// 提前退出：已读够
		if int64(len(result)) >= maxBytesToRead {
			break
		}

		currentOffset = uint64(chunkEnd)
		if currentOffset >= uint64(readEnd) {
			break
		}
	}

	logger.Debugf("completed reading data from inode %d, actual bytes read: %d", n.Ino, len(result))
	return result
}

func (n *INode) Write(fs *DedupFS, offset int64, data []byte) error {
	logger.Infof("writing %d bytes to inode %d at offset %d", len(data), n.Ino, offset)
	if len(data) == 0 {
		return nil
	}

	defer func() {
		logger.Infof("finished writing to inode %d, size %d, chunks %d", n.Ino, n.Size, len(n.Chunks))
	}()

	wEnd := offset + int64(len(data))

	// 情况1: 在文件末尾或超出文件末尾的写入（包括append和空洞写入）
	if offset >= int64(n.Size) {
		logger.Debugf("writing to inode %d: offset=%d, size=%d, file_size=%d, wEnd %d ", n.Ino, offset, len(data), n.Size, wEnd)
		var lastChunk *INodeChunk

		// 尝试获取最后一个 chunk
		if len(n.Chunks) > 0 {
			lastChunk = n.Chunks[len(n.Chunks)-1]
			if lastChunk != nil && len(lastChunk.Data) == 0 {
				// 需要从存储加载元数据
				c, err := GetChunkMeta(lastChunk.Hash, fs)
				if err != nil || c == nil {
					logger.Errorf("failed to get chunk meta for hash %s: %v", lastChunk.Hash, err)
					return fmt.Errorf("failed to get chunk meta")
				} else if int64(c.Size) > fs.BlockConf.Size {
					// chunk 大小超过 block size，视为无效，新建空 chunk
					logger.Debugf("chunk size %d exceeds block size %d, creating new chunk", c.Size, fs.BlockConf.Size)
					lastChunk = &INodeChunk{}
				} else {
					// 加载实际数据
					chunkData, err := GetChunkData(lastChunk.Hash, fs)
					if err != nil || chunkData == nil {
						logger.Errorf("failed to get chunk data for hash %s: %v", lastChunk.Hash, err)
						return fmt.Errorf("failed to get chunk data")
					} else {
						n.DropChunks = append(n.DropChunks, &INodeChunk{Hash: lastChunk.Hash})
						lastChunk.Hash = ""
						lastChunk.Data = chunkData.Data
					}
				}
			}
		}

		if lastChunk == nil {
			lastChunk = &INodeChunk{}
		}

		// 填充空洞（如果 offset > 当前文件大小）
		if offset > int64(n.Size) {
			holeSize := offset - int64(n.Size)
			lastChunk.Data = append(lastChunk.Data, make([]byte, holeSize)...)
			lastChunk.Hash = ""
		}

		// 追加新数据
		lastChunk.Data = append(lastChunk.Data, data...)
		lastChunk.Hash = ""

		// 替换或追加 chunk
		if len(n.Chunks) > 0 {
			n.Chunks[len(n.Chunks)-1] = lastChunk
		} else {
			n.Chunks = append(n.Chunks, lastChunk)
		}

		n.Size = uint64(wEnd)

		// 更新时间戳
		now := time.Now().UTC()
		n.Mtime = now
		n.Ctime = now
		goto UPDATA_CACHE_LABLE
	}

	// 情况2: 写入范围超出当前文件大小（部分覆盖 + 追加）
	if wEnd > int64(n.Size) {
		var (
			chunkStart          int64 = 0
			affectedStartIndex  *int
			affectedEndIndex    *int
			affectedStartOffset int64
		)
		logger.Errorf("writing to inode %d, offset %d, size %d, chunks %d, wEnd %d node size %d ", n.Ino, offset, len(data), len(n.Chunks), wEnd, n.Size)
		for i, chunk := range n.Chunks {
			var chunkSize int64
			if len(chunk.Data) > 0 {
				chunkSize = int64(len(chunk.Data))
			} else {
				c, err := GetChunkMeta(chunk.Hash, fs)
				if err != nil || c == nil {
					logger.Errorf("failed to get chunk meta for hash %s: %v", chunk.Hash, err)
					return fmt.Errorf("failed to get chunk meta")
				} else {
					chunkSize = int64(c.Size)
				}
			}

			chunkEnd := chunkStart + chunkSize

			// 找到包含 offset 的 chunk
			if affectedStartIndex == nil && chunkStart <= offset && offset < chunkEnd {
				idx := i
				affectedStartIndex = &idx
				affectedStartOffset = chunkStart
			}

			// 找到包含原文件末尾（n.Size）的 chunk
			if affectedEndIndex == nil && chunkStart <= int64(n.Size) && int64(n.Size) <= chunkEnd {
				idx := i
				affectedEndIndex = &idx
			}

			if affectedStartIndex != nil && affectedEndIndex != nil {
				break
			}

			chunkStart = chunkEnd
		}

		if affectedStartIndex == nil || affectedEndIndex == nil {
			return fmt.Errorf("failed to locate affected chunks for partial write")
		}

		startIdx := *affectedStartIndex
		endIdx := *affectedEndIndex

		// 合并受影响的 chunks
		var mergedData []byte
		for i := startIdx; i <= endIdx; i++ {
			chunk := n.Chunks[i]
			if len(chunk.Data) > 0 {
				mergedData = append(mergedData, chunk.Data...)
			} else {
				chunkData, err := GetChunkData(chunk.Hash, fs)
				if err != nil || chunkData == nil {
					logger.Errorf("failed to get chunk data for hash %s: %v", chunk.Hash, err)
					return fmt.Errorf("failed to get chunk data")
				} else {
					mergedData = append(mergedData, chunkData.Data...)
				}
			}
			n.DropChunks = append(n.DropChunks, &INodeChunk{Hash: chunk.Hash})
		}

		// 计算写入位置
		dataOffsetInMerged := offset - affectedStartOffset
		overlapSize := int64(n.Size) - offset // 覆盖部分长度

		// 覆盖已有部分
		if overlapSize > 0 {
			copyEnd := dataOffsetInMerged + overlapSize
			if copyEnd > int64(len(mergedData)) {
				copyEnd = int64(len(mergedData))
			}
			if copyEnd > dataOffsetInMerged {
				copy(mergedData[dataOffsetInMerged:copyEnd], data[:overlapSize])
			}
		}

		// 追加超出原文件大小的部分
		if wEnd > int64(n.Size) {
			appendData := data[overlapSize:]
			mergedData = append(mergedData, appendData...)
		}

		// 替换受影响的 chunks
		newChunk := &INodeChunk{Data: mergedData}
		n.Chunks = append(n.Chunks[:startIdx], append([]*INodeChunk{newChunk}, n.Chunks[endIdx+1:]...)...)

		n.Size = uint64(wEnd)
		// 更新时间戳
		now := time.Now().UTC()
		n.Mtime = now
		n.Ctime = now

		logger.Debugf("write completed for inode %d, file size: %d, chunks: %d", n.Ino, n.Size, len(n.Chunks))
		goto UPDATA_CACHE_LABLE
	}

	// 情况3: 纯覆盖写入（完全在文件范围内）
	if offset >= 0 && wEnd <= int64(n.Size) {
		var (
			chunkStart          int64 = 0
			affectedStartIndex  *int
			affectedEndIndex    *int
			affectedStartOffset int64
		)
		logger.Errorf("performing overwrite write for inode %d, offset: %d, end: %d", n.Ino, offset, wEnd)
		for i, chunk := range n.Chunks {
			var chunkSize int64
			if len(chunk.Data) > 0 {
				chunkSize = int64(len(chunk.Data))
			} else {
				c, err := GetChunkMeta(chunk.Hash, fs)
				if err != nil || c == nil {
					logger.Errorf("failed to get chunk meta for hash %s: %v", chunk.Hash, err)
					return fmt.Errorf("failed to get chunk meta")
				} else {
					chunkSize = int64(c.Size)
				}
			}

			chunkEnd := chunkStart + chunkSize

			if affectedStartIndex == nil && chunkStart <= offset && offset < chunkEnd {
				idx := i
				affectedStartIndex = &idx
				affectedStartOffset = chunkStart
			}

			if affectedEndIndex == nil && chunkStart <= wEnd && wEnd <= chunkEnd {
				idx := i
				affectedEndIndex = &idx
			}

			if affectedStartIndex != nil && affectedEndIndex != nil {
				break
			}

			chunkStart = chunkEnd
		}

		if affectedStartIndex == nil || affectedEndIndex == nil {
			return fmt.Errorf("failed to locate affected chunks for overwrite write")
		}

		startIdx := *affectedStartIndex
		endIdx := *affectedEndIndex

		// 合并受影响 chunks
		var mergedData []byte
		for i := startIdx; i <= endIdx; i++ {
			chunk := n.Chunks[i]
			if len(chunk.Data) > 0 {
				mergedData = append(mergedData, chunk.Data...)
			} else {
				chunkData, err := GetChunkData(chunk.Hash, fs)
				if err != nil || chunkData == nil {
					logger.Errorf("failed to get chunk data for hash %s: %v", chunk.Hash, err)
					return fmt.Errorf("failed to get chunk data")
				} else {
					mergedData = append(mergedData, chunkData.Data...)
				}
			}
			n.DropChunks = append(n.DropChunks, &INodeChunk{Hash: chunk.Hash})
		}

		// 执行覆盖写入
		dataOffsetInMerged := offset - affectedStartOffset
		writeEndInMerged := dataOffsetInMerged + int64(len(data))
		if writeEndInMerged > int64(len(mergedData)) {
			return fmt.Errorf("write range exceeds merged chunk data")
		}
		copy(mergedData[dataOffsetInMerged:writeEndInMerged], data)

		// 替换 chunks
		newChunk := &INodeChunk{Data: mergedData}
		n.Chunks = append(n.Chunks[:startIdx], append([]*INodeChunk{newChunk}, n.Chunks[endIdx+1:]...)...)
		// 更新时间戳
		now := time.Now().UTC()
		n.Mtime = now
		n.Ctime = now
		n.Size = uint64(wEnd)
		goto UPDATA_CACHE_LABLE
	}

UPDATA_CACHE_LABLE:
	if err := CacheINode(fs, n); err != nil {
		logger.Errorf("file.write: failed to cache inode: %v", err)
		return fmt.Errorf("file.write: failed to cache inode: %v", err)
	}
	logger.Debugf("write completed for inode %d, file size: %d, chunks: %d", n.Ino, n.Size, len(n.Chunks))
	return nil
}

func (n *INode) Truncate(fs *DedupFS, size uint64) error {
	logger.Debugf("truncating inode %d to size %d (current size: %d)", n.Ino, size, n.Size)

	if size == n.Size {
		logger.Debugf("truncate skipped for inode %d: size unchanged", n.Ino)
		return nil
	}

	if size < n.Size {
		// 情况1: 截断文件（缩小）
		var (
			currentOffset uint64
			chunksToKeep  []*INodeChunk
		)

		for _, chunk := range n.Chunks {
			// 获取 chunk 元数据以确定其实际大小
			c, err := GetChunkMeta(chunk.Hash, fs)
			if err != nil || c == nil {
				logger.Errorf("failed to get chunk meta for hash %s: %v", chunk.Hash, err)
				return fmt.Errorf("failed to get chunk meta")
			}

			chunkEnd := currentOffset + uint64(c.Size)

			if chunkEnd <= size {
				// 整个 chunk 保留
				chunksToKeep = append(chunksToKeep, chunk)
				currentOffset = chunkEnd
			} else if currentOffset < size {
				// 部分保留：需要加载数据并截断
				bytesToKeep := size - currentOffset

				chunkData, err := GetChunkData(chunk.Hash, fs)
				if err != nil || chunkData == nil {
					logger.Errorf("failed to get chunk data for hash %s: %v", chunk.Hash, err)
					return fmt.Errorf("failed to get chunk data")
				}

				if uint64(len(chunkData.Data)) < bytesToKeep {
					logger.Warnf("chunk data shorter than expected: got %d, need %d", len(chunkData.Data), bytesToKeep)
					bytesToKeep = uint64(len(chunkData.Data))
				}

				truncatedData := chunkData.Data[:bytesToKeep]
				chunksToKeep = append(chunksToKeep, &INodeChunk{
					Data: truncatedData, // 或 []byte{}，根据你的设计
				})
				currentOffset = size
				break
			} else {
				// chunk 完全在截断点之后，停止处理
				break
			}
		}

		n.Chunks = chunksToKeep
	} else {
		// 情况2: 扩展文件（增大）
		zeroBytesNeeded := size - n.Size

		if len(n.Chunks) > 0 {
			lastChunk := n.Chunks[len(n.Chunks)-1]

			// 优先尝试扩展最后一个 chunk（如果它已有内存数据）
			if len(lastChunk.Data) > 0 {
				// 在内存中扩展（避免重新加载）
				lastChunk.Data = append(lastChunk.Data, make([]byte, zeroBytesNeeded)...)
				lastChunk.Hash = ""
			} else {
				// 最后一个 chunk 无内存数据，需新建一个全零 chunk
				zeroData := make([]byte, zeroBytesNeeded)
				n.Chunks = append(n.Chunks, &INodeChunk{
					Data: zeroData,
				})
			}
		} else {
			// 文件为空，直接创建新 chunk
			zeroData := make([]byte, zeroBytesNeeded)
			n.Chunks = append(n.Chunks, &INodeChunk{
				Data: zeroData,
			})
		}
	}

	// 更新文件大小和时间戳
	n.Size = size
	now := time.Now().UTC()
	n.Mtime = now
	n.Ctime = now

	if err := CacheINode(fs, n); err != nil {
		logger.Errorf("file.write: failed to cache inode: %v", err)
		return fmt.Errorf("file.write: failed to cache inode: %v", err)
	}

	logger.Debugf("truncate completed for inode %d, new size: %d, chunks count: %d", n.Ino, n.Size, len(n.Chunks))
	return nil
}

// SetXattr 设置扩展属性
func (n *INode) SetXattr(name string, value []byte) error {
	logger.Debugf("setting xattr '%s' on inode %d, value size: %d bytes", name, n.Ino, len(value))
	// 确保Xattr映射已初始化
	if n.Xattr == nil {
		n.Xattr = make(map[string][]byte)
	}
	// 设置扩展属性
	n.Xattr[name] = value
	logger.Debugf("xattr '%s' set successfully on inode %d", name, n.Ino)
	return nil
}

// GetXattr 获取扩展属性
func (n *INode) GetXattr(name string) ([]byte, error) {
	logger.Debugf("getting xattr '%s' from inode %d", name, n.Ino)
	// 检查Xattr映射是否已初始化
	if n.Xattr == nil {
		logger.Errorf("no extended attributes for inode %d", n.Ino)
		return nil, fmt.Errorf("no extended attributes")
	}
	// 获取扩展属性值
	value, exists := n.Xattr[name]
	if !exists {
		logger.Warnf("attribute '%s' not found on inode %d", name, n.Ino)
		return nil, fmt.Errorf("attribute not found: %s", name)
	}
	// 返回值的副本以避免外部修改
	result := make([]byte, len(value))
	copy(result, value)
	logger.Debugf("retrieved xattr '%s' from inode %d, value size: %d bytes", name, n.Ino, len(result))
	return result, nil
}

// ListXattr 列出所有扩展属性名称
func (n *INode) ListXattr() ([]string, error) {
	logger.Debugf("listing all xattrs for inode %d", n.Ino)
	// 检查Xattr映射是否已初始化
	if n.Xattr == nil {
		logger.Debugf("no xattrs found for inode %d", n.Ino)
		return []string{}, nil
	}
	// 创建属性名称列表
	attrs := make([]string, 0, len(n.Xattr))
	for name := range n.Xattr {
		attrs = append(attrs, name)
	}
	logger.Debugf("found %d xattrs for inode %d", len(attrs), n.Ino)
	return attrs, nil
}

// RemoveXattr 删除扩展属性
func (n *INode) RemoveXattr(name string) error {
	logger.Debugf("removing xattr '%s' from inode %d", name, n.Ino)
	// 检查Xattr映射是否已初始化
	if n.Xattr == nil {
		logger.Errorf("no extended attributes for inode %d", n.Ino)
		return fmt.Errorf("no extended attributes")
	}
	// 检查属性是否存在
	if _, exists := n.Xattr[name]; !exists {
		logger.Warnf("attribute '%s' not found on inode %d", name, n.Ino)
		return fmt.Errorf("attribute not found: %s", name)
	}
	// 删除属性
	delete(n.Xattr, name)
	logger.Debugf("xattr '%s' removed successfully from inode %d", name, n.Ino)
	return nil
}

func CreateINode(ino, pino uint64, kind FileType, name string, mode uint16) *INode {
	logger.Debugf("creating new inode: ino=%d, parent=%d, kind=%s, name=%s, mode=%o",
		ino, pino, kind, name, mode)
	now := time.Now().UTC()
	inode := &INode{
		Ino:        ino,
		Size:       0,
		Blocks:     0,
		Atime:      now,
		Mtime:      now,
		Ctime:      now,
		Crtime:     now,
		Kind:       kind,
		Perm:       uint16(mode & 0777),
		Nlink:      1,
		Uid:        uint32(os.Getuid()),
		Gid:        uint32(os.Getgid()),
		Name:       name,
		Parent:     pino,
		Xattr:      make(map[string][]byte),
		Chunks:     []*INodeChunk{},
		DropChunks: []*INodeChunk{},
	}
	logger.Debugf("inode %d created successfully", inode.Ino)
	return inode
}

func CacheINode(fs *DedupFS, inode *INode) error {
	if fs == nil || inode == nil {
		return fmt.Errorf("invalid param for cache node")
	}

	key := fmt.Sprintf("inode:%s:%d", fs.ID, inode.Ino)
	G_INODE_CACHE.Put(key, inode)

	if len(inode.Chunks) > 0 {
		// 如果chunks 的数据 大于 64M， 则将chunks数据缓存到磁盘中
		cacheSize := 0
		for _, chunk := range inode.Chunks {
			if len(chunk.Data) > 0 {
				cacheSize += len(chunk.Data)
			}
		}
		if cacheSize < int(fs.BlockConf.Size) {
			return nil
		}

		return SaveINode(fs, inode)
	}
	return nil
}

func SaveINode(fs *DedupFS, inode *INode) error {
	logger.Debugf("saving inode %d (name=%s, kind=%s, chunks=%d, size=%d) to filesystem %s", inode.Ino, inode.Name, inode.Kind, len(inode.Chunks), inode.Size, fs.ID)
	key := fmt.Sprintf("inode:%s:%d", fs.ID, inode.Ino)
	if len(inode.Chunks) > 0 {
		savaChunks := make([]*Chunk, 0)
		newChunks := make([]*INodeChunk, 0, len(inode.Chunks))
		for _, chunk := range inode.Chunks {
			if len(chunk.Data) > 0 {
				if len(chunk.Data) > int(fs.ChunkConf.MaxSize) {
					if chunks, err := DoChunking(chunk.Data, fs); err != nil {
						logger.Errorf("failed to chunk data: %v", err)
						return fmt.Errorf("failed to fastcdc chunk data: %w", err)
					} else {
						for _, c := range chunks {
							newChunks = append(newChunks, &INodeChunk{
								Hash: c.Hash,
							})
						}
						savaChunks = append(savaChunks, chunks...)
					}
				} else {
					chunk.Hash = calcHash(chunk.Data)
					newChunks = append(newChunks, &INodeChunk{
						Hash: chunk.Hash,
					})
					savaChunks = append(savaChunks, &Chunk{
						Data: chunk.Data,
						Hash: chunk.Hash,
						Size: int32(len(chunk.Data)),
					})
				}
			} else {
				if chunk.Hash == "" {
					logger.Errorf("chunk hash is empty")
					return fmt.Errorf("chunk hash is empty")
				}
				newChunks = append(newChunks, chunk)
			}
		}

		inode.Chunks = newChunks
		logger.Debugf("inode %d chunks %d:%d", inode.Ino, len(inode.Chunks), len(savaChunks))
		if len(savaChunks) > 0 {
			logger.Debugf("saving %d chunks to filesystem %s", len(savaChunks), fs.ID)
			if err := PutChunks(savaChunks, fs); err != nil {
				logger.Errorf("failed to put chunks: %v", err)
				return fmt.Errorf("failed to put chunks: %w", err)
			}
		}
	}

	// 从kv_store 获取已有的 inode ， 放到 old_inode 中
	var old_inode INode
	if err := fs.KVStore.Get(key, &old_inode); err != nil {
		if !errors.Is(err, kvstore.ErrKeyNotFound) {
			logger.Errorf("failed to get inode %d: %v", inode.Ino, err)
			return err
		}
	} else {
		logger.Debugf("inode %d chunks %d:%d", inode.Ino, len(old_inode.Chunks), len(inode.Chunks))

		// 统计新分片中每个哈希的出现次数
		newHashCount := make(map[string]int, len(inode.Chunks))
		for _, c := range inode.Chunks {
			if c.Hash != "" {
				newHashCount[c.Hash]++
			} else {
				logger.Errorf("chunk data is empty of node %s  %d", inode.Name, inode.Ino)
			}
		}
		// 废弃的chunks
		for _, dropChunk := range inode.DropChunks {
			if dropChunk.Hash != "" && newHashCount[dropChunk.Hash] > 0 {
				newHashCount[dropChunk.Hash]--
			} else {
				logger.Errorf("chunk %#v is empty of node %s  %d", dropChunk, inode.Name, inode.Ino)
			}
		}

		// 统计旧分片中每个哈希的出现次数
		oldHashCount := make(map[string]int, len(old_inode.Chunks))
		for _, oldChunk := range old_inode.Chunks {
			if oldChunk.Hash != "" {
				oldHashCount[oldChunk.Hash]++
			} else {
				logger.Errorf("chunk data is empty of node %s  %d", inode.Name, inode.Ino)
			}
		}

		// 找出需要删除的分块：旧计数 - 新计数 > 0 的部分
		for hash, oldCount := range oldHashCount {
			newCount := newHashCount[hash] // 如果不存在，newCount为0
			deleteCount := oldCount - newCount

			if deleteCount > 0 {
				logger.Debugf("removing %d chunk %s from filesystem %s", deleteCount, hash, fs.ID)
				for i := 0; i < deleteCount; i++ {
					if err := RemoveChunk(hash, fs); err != nil {
						logger.Errorf("failed to remove chunk %s: %v", hash, err)
						return fmt.Errorf("failed to remove chunk %s: %w", hash, err)
					}
				}
			}
		}

		inode.DropChunks = []*INodeChunk{}
	}

	if err := fs.KVStore.Set(key, inode); err != nil {
		logger.Errorf("failed to save inode %d: %v", inode.Ino, err)
		return err
	}

	G_INODE_CACHE.Put(key, inode)
	logger.Debugf("inode %d saved successfully", inode.Ino)
	return nil
}

func FlushINode(fs *DedupFS, inode *INode) error {
	if err := SaveINode(fs, inode); err != nil {
		logger.Errorf("failed to save inode %d: %v", inode.Ino, err)
		return fmt.Errorf("failed to save inode %d: %w", inode.Ino, err)
	}
	if err := SaveBlock(nil, fs); err != nil {
		logger.Errorf("failed to save block: %v", err)
		return fmt.Errorf("failed to save block: %w", err)
	}
	return nil
}

func GetINode(fs *DedupFS, ino uint64) (*INode, error) {
	logger.Debugf("getting inode %d from filesystem %s", ino, fs.ID)
	key := fmt.Sprintf("inode:%s:%d", fs.ID, ino)
	if inode, exists := G_INODE_CACHE.Get(key); exists && inode != nil {
		logger.Debugf("inode %d name %s size %d found in cache", inode.Ino, inode.Name, inode.Size)
		return inode, nil
	}
	var inode *INode
	err := fs.KVStore.Get(key, &inode)
	if err != nil {
		logger.Errorf("failed to get inode %d: %v", ino, err)
		return nil, err
	}
	logger.Debugf("retrieved inode %d: name=%s, kind=%s", inode.Ino, inode.Name, inode.Kind)
	return inode, nil
}

func DelINode(fs *DedupFS, ino uint64) error {
	logger.Debugf("deleting inode %d from filesystem %s", ino, fs.ID)

	// 删除inode 的 chunks
	if inode, err := GetINode(fs, ino); err != nil || inode == nil {
		logger.Errorf("failed to get inode %d: %v", ino, err)
		return fmt.Errorf("failed to get inode %d: %v", ino, err)
	} else {
		for _, chunk := range inode.Chunks {
			if chunk.Hash != "" {
				if err := RemoveChunk(chunk.Hash, fs); err != nil {
					logger.Errorf("failed to remove chunk %s: %v", chunk.Hash, err)
					return fmt.Errorf("failed to remove chunk %s: %w", chunk.Hash, err)
				}
			}
		}
	}

	key := fmt.Sprintf("inode:%s:%d", fs.ID, ino)
	G_INODE_CACHE.Del(key)

	err := fs.KVStore.Del(key)
	if err != nil {
		logger.Errorf("failed to delete inode %d: %v", ino, err)
		return err
	}
	logger.Debugf("inode %d deleted successfully", ino)
	return nil
}
