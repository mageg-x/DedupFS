package dfs

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mageg-x/dedupfs/internal/cache"
	"github.com/mageg-x/dedupfs/internal/memfs"
	"github.com/mageg-x/dedupfs/internal/utils"
)

var (
	G_BLOCK_CACHE = cache.NewCache[string, *Block](1024*1024*1024, true)
	CURRENT_BLOCK = sync.Map{}
)

// BlockChunk 表示块中的一个块条目
type BlockChunk struct {
	Hash string // base64 or hex string
	Size int32
}

// BlockHeader 存放在磁盘上只包含头信息，不含 Data
type BlockHeader struct {
	ID         string
	Ver        int32
	Etag       [16]byte
	TotalSize  int64
	RealSize   int64
	Compressed bool
	Encrypted  bool
	ChunkList  []*BlockChunk
	CreatedAt  uint64 // nanoseconds since Unix epoch (Go uses uint64 for time)
	UpdatedAt  uint64
}

// Block 完整结构（包含 Data）
type Block struct {
	Header BlockHeader
	Data   []byte
}

func (b *Block) Len() int {
	return len(b.Data) + len(b.Header.ChunkList)*32 + len(b.Header.ID) + 64
}

// RemoveChunk 从 block 中删除指定 chunk
func (b *Block) RemoveChunk(chunk *Chunk) error {
	logger.Debugf("removing chunk %s from block %s", chunk.Hash, b.Header.ID)
	var targetIndex int = -1
	offset := 0
	for i, c := range b.Header.ChunkList {
		if c.Hash == chunk.Hash {
			targetIndex = i
			break
		}
		offset += int(c.Size)
	}

	if targetIndex == -1 {
		logger.Info("chunk %s not in block %s", chunk.Hash, b.Header.ID)
		return nil
	}

	chunkSize := int(b.Header.ChunkList[targetIndex].Size)
	if offset+chunkSize > len(b.Data) {
		logger.Errorf("data out of bounds for chunk %s in block %s", chunk.Hash, b.Header.ID)
		return fmt.Errorf("data out of bounds")
	}

	// Remove from chunk list
	b.Header.ChunkList = append(b.Header.ChunkList[:targetIndex], b.Header.ChunkList[targetIndex+1:]...)

	// Remove from data
	newData := make([]byte, 0, len(b.Data)-chunkSize)
	newData = append(newData, b.Data[:offset]...)
	newData = append(newData, b.Data[offset+chunkSize:]...)
	b.Data = newData

	b.Header.TotalSize -= int64(chunk.Size)
	b.Header.UpdatedAt = uint64(time.Now().UnixNano())

	return nil
}

// NewBlock 创建新的块
func NewBlock() (*Block, error) {
	now := uint64(time.Now().UnixNano())
	blockID := generateBlockID()
	logger.Debugf("creating new block with ID %s", blockID)
	return &Block{
		Header: BlockHeader{
			ID:        blockID,
			Ver:       1,
			Etag:      [16]byte{},
			TotalSize: 0,
			RealSize:  0,
			ChunkList: []*BlockChunk{},
			CreatedAt: now,
			UpdatedAt: now,
		},
		Data: []byte{},
	}, nil
}

// Serialize 快速序列化 Block 到 []byte（与 Rust 二进制格式兼容）
func SerializeBlock(b *Block) ([]byte, error) {
	var buf bytes.Buffer

	// ID
	idBytes := []byte(b.Header.ID)
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(idBytes))); err != nil {
		return nil, err
	}
	buf.Write(idBytes)

	// Ver
	if err := binary.Write(&buf, binary.LittleEndian, b.Header.Ver); err != nil {
		return nil, err
	}

	// Etag
	buf.Write(b.Header.Etag[:])

	// TotalSize, RealSize
	if err := binary.Write(&buf, binary.LittleEndian, b.Header.TotalSize); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, b.Header.RealSize); err != nil {
		return nil, err
	}

	// Compressed, Encrypted
	var compByte, encByte byte
	if b.Header.Compressed {
		compByte = 1
	}
	if b.Header.Encrypted {
		encByte = 1
	}
	buf.WriteByte(compByte)
	buf.WriteByte(encByte)

	// ChunkList
	if err := binary.Write(&buf, binary.LittleEndian, uint32(len(b.Header.ChunkList))); err != nil {
		return nil, err
	}
	for _, chunk := range b.Header.ChunkList {
		logger.Debugf("writing chunk %s with size %d", chunk.Hash, chunk.Size)
		chunkHash := []byte(chunk.Hash)
		if err := binary.Write(&buf, binary.LittleEndian, uint32(len(chunkHash))); err != nil {
			return nil, err
		}
		buf.Write(chunkHash)
		if err := binary.Write(&buf, binary.LittleEndian, chunk.Size); err != nil {
			return nil, err
		}
	}

	// CreatedAt, UpdatedAt
	if err := binary.Write(&buf, binary.LittleEndian, b.Header.CreatedAt); err != nil {
		return nil, err
	}
	if err := binary.Write(&buf, binary.LittleEndian, b.Header.UpdatedAt); err != nil {
		return nil, err
	}

	// Data
	if err := binary.Write(&buf, binary.LittleEndian, uint64(len(b.Data))); err != nil {
		return nil, err
	}
	buf.Write(b.Data)

	return buf.Bytes(), nil
}

// Deserialize 反序列化 []byte 到 Block
func DeserializeBlock(data []byte) (*Block, error) {
	r := bytes.NewReader(data)

	// ID
	var idLen uint32
	if err := binary.Read(r, binary.LittleEndian, &idLen); err != nil {
		return nil, fmt.Errorf("read id len %w", err)
	}
	idBytes := make([]byte, idLen)
	if _, err := io.ReadFull(r, idBytes); err != nil {
		return nil, fmt.Errorf("read id %w", err)
	}
	id := string(idBytes)

	// Ver
	var ver int32
	if err := binary.Read(r, binary.LittleEndian, &ver); err != nil {
		return nil, fmt.Errorf("read ver %w", err)
	}

	// Etag
	var etag [16]byte
	if _, err := io.ReadFull(r, etag[:]); err != nil {
		return nil, fmt.Errorf("read etag %w", err)
	}

	// TotalSize, RealSize
	var totalSize, realSize int64
	if err := binary.Read(r, binary.LittleEndian, &totalSize); err != nil {
		return nil, fmt.Errorf("read total_size %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &realSize); err != nil {
		return nil, fmt.Errorf("read real_size %w", err)
	}

	// Compressed, Encrypted
	compByte, _ := r.ReadByte()
	encByte, _ := r.ReadByte()
	compressed := compByte != 0
	encrypted := encByte != 0

	// ChunkList
	var chunkListLen uint32
	if err := binary.Read(r, binary.LittleEndian, &chunkListLen); err != nil {
		return nil, fmt.Errorf("read chunk_list len %w", err)
	}
	chunkList := make([]*BlockChunk, chunkListLen)
	for i := range chunkList {
		var hashLen uint32
		if err := binary.Read(r, binary.LittleEndian, &hashLen); err != nil {
			return nil, fmt.Errorf("read chunk[%d] hash len %w", i, err)
		}
		hashBytes := make([]byte, hashLen)
		if _, err := io.ReadFull(r, hashBytes); err != nil {
			return nil, fmt.Errorf("read chunk[%d] hash %w", i, err)
		}
		var size int32
		if err := binary.Read(r, binary.LittleEndian, &size); err != nil {
			return nil, fmt.Errorf("read chunk[%d] size %w", i, err)
		}
		chunkList[i] = &BlockChunk{
			Hash: string(hashBytes),
			Size: size,
		}
	}

	// CreatedAt, UpdatedAt
	var createdAt, updatedAt uint64
	if err := binary.Read(r, binary.LittleEndian, &createdAt); err != nil {
		return nil, fmt.Errorf("read created_at %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &updatedAt); err != nil {
		return nil, fmt.Errorf("read updated_at %w", err)
	}

	// Data
	var dataLen uint64
	if err := binary.Read(r, binary.LittleEndian, &dataLen); err != nil {
		return nil, fmt.Errorf("read data len %w", err)
	}
	blockData := make([]byte, dataLen)
	if _, err := io.ReadFull(r, blockData); err != nil {
		return nil, fmt.Errorf("read data %w", err)
	}

	return &Block{
		Header: BlockHeader{
			ID:         id,
			Ver:        ver,
			Etag:       etag,
			TotalSize:  totalSize,
			RealSize:   realSize,
			Compressed: compressed,
			Encrypted:  encrypted,
			ChunkList:  chunkList,
			CreatedAt:  createdAt,
			UpdatedAt:  updatedAt,
		},
		Data: blockData,
	}, nil
}

// generateBlockID 生成 block ID
func generateBlockID() string {
	u := uuid.New()
	uuidStr := strings.ReplaceAll(u.String(), "-", "")
	ts := time.Now().UnixNano()
	timePart := fmt.Sprintf("%016x", ts)
	randomPart := uuidStr[:16]
	return timePart + randomPart
}

// GetBlockPath 根据 blockID 生成存储路径
func GetBlockPath(blockID string) string {
	n := len(blockID)
	if n < 9 {
		return filepath.Join("blocks", "default", blockID)
	}
	dir1 := blockID[n-3:]
	dir2 := blockID[n-6 : n-3]
	dir3 := blockID[n-9 : n-6]
	return filepath.Join("blocks", dir1, dir2, dir3, blockID)
}

func ReadBlockMeta(blockID string, fs *DedupFS) (*Block, error) {
	blockPath := filepath.Join(fs.DataDir, GetBlockPath(blockID))
	logger.Debugf("block path: %s", blockPath)
	mfs := memfs.GetInstance()
	if mfs == nil {
		logger.Errorf("memfs not initialized")
		return nil, fmt.Errorf("memfs not initialized")
	}

	data, err := mfs.Read(blockPath, nil)
	if err != nil || data == nil {
		logger.Errorf("memfs read block failed: %v", err)
		return nil, fmt.Errorf("memfs read block failed %w", err)
	}
	logger.Errorf("successfully read block data, size: %d bytes", len(data))

	block, err := DeserializeBlock(data)
	if err != nil {
		logger.Errorf("failed to deserialize block: %v", err)
		return nil, fmt.Errorf("deserial block failed %w", err)
	}
	return block, nil
}

// ReadBlock 读取 block
func ReadBlock(blockID string, fs *DedupFS) (*Block, error) {
	logger.Debugf("reading block %s for filesystem %s", blockID, fs.ID)

	cacheKey := "block:" + fs.ID + ":" + blockID
	if block, ok := G_BLOCK_CACHE.Get(cacheKey); ok && block != nil {
		logger.Debugf("block %s found in cache", blockID)
		return block, nil
	}

	blockPath := filepath.Join(fs.DataDir, GetBlockPath(blockID))
	logger.Debugf("block path: %s", blockPath)
	mfs := memfs.GetInstance()
	if mfs == nil {
		logger.Errorf("memfs not initialized")
		return nil, fmt.Errorf("memfs not initialized")
	}

	data, err := mfs.Read(blockPath, nil)
	if err != nil || data == nil {
		logger.Errorf("memfs read block failed: %v", err)
		return nil, fmt.Errorf("memfs read block failed %w", err)
	}
	logger.Errorf("successfully read block data, size: %d bytes", len(data))

	block, err := DeserializeBlock(data)
	if err != nil {
		logger.Errorf("failed to deserialize block: %v", err)
		return nil, fmt.Errorf("deserial block failed %w", err)
	}

	if block.Header.RealSize != int64(len(block.Data)) {
		return nil, fmt.Errorf("real_size=%d != data_len=%d %w", block.Header.RealSize, len(block.Data), err)
	}

	if block.Header.Encrypted {
		if d, err := utils.Decrypt(block.Data, blockID+fs.BlockConf.Password); err != nil {
			return nil, fmt.Errorf("decrypt block failed %w", err)
		} else {
			block.Data = d
			block.Header.Encrypted = false
			block.Header.RealSize = int64(len(block.Data))
		}
	}

	if block.Header.Compressed {
		if d, err := utils.Decompress(block.Data); err != nil {
			return nil, fmt.Errorf("decompress block failed %w", err)
		} else {
			block.Data = d
			block.Header.Compressed = false
			block.Header.RealSize = int64(len(block.Data))
		}
	}

	G_BLOCK_CACHE.Put(cacheKey, block)
	logger.Debugf("block %s successfully read and cached, contains %d chunks", blockID, len(block.Header.ChunkList))
	return block, nil
}

// SaveBlock 保存 block
func SaveBlock(block *Block, fs *DedupFS) error {
	if fs == nil {
		logger.Errorf("fs is nil")
		return fmt.Errorf("fs is nil")
	}

	// block 为 nil 时候，缺省是 current block
	if block == nil {
		key := fmt.Sprintf("block:%s", fs.ID)
		if b, ok := CURRENT_BLOCK.Load(key); ok && b != nil {
			if curBlock, yes := b.(*Block); yes && curBlock != nil {
				block = curBlock
			}
		}
	}

	if block == nil {
		logger.Errorf("block is nil")
		return fmt.Errorf("block is nil")
	}

	logger.Debugf("saving block %s for filesystem %s", block.Header.ID, fs.ID)
	block.Header.UpdatedAt = uint64(time.Now().UnixNano())
	block.Header.Compressed = false
	block.Header.Encrypted = false
	block.Header.RealSize = int64(len(block.Data))
	block.Header.TotalSize = 0
	for _, c := range block.Header.ChunkList {
		block.Header.TotalSize += int64(c.Size)
	}
	logger.Debugf("block %s: %d chunks, total size: %d bytes", block.Header.ID, len(block.Header.ChunkList), block.Header.TotalSize)

	if block.Header.TotalSize != int64(len(block.Data)) {
		logger.Errorf("size mismatch: total_size=%d != data_len=%d", block.Header.TotalSize, len(block.Data))
		return fmt.Errorf("total_size=%d != data_len=%d", block.Header.TotalSize, len(block.Data))
	}

	data, err := SerializeBlock(block)
	if err != nil || data == nil {
		logger.Errorf("failed to serialize block: %v", err)
		return fmt.Errorf("serialize block failed %w", &err)
	}

	blockPath := filepath.Join(fs.DataDir, GetBlockPath(block.Header.ID))
	logger.Debugf("block save path: %s", blockPath)
	if err := os.MkdirAll(filepath.Dir(blockPath), 0755); err != nil {
		logger.Errorf("failed to create directory: %v", err)
		return fmt.Errorf("failed to create dir: %w", err)
	}

	mfs := memfs.GetInstance()
	if mfs == nil {
		logger.Errorf("memfs not initialized")
		return fmt.Errorf("memfs not initialized")
	}

	// 下面这些耗时的操作，放在 后面处理
	processor := &memfs.Processor{
		Func: func(data []byte, params map[string]string) ([]byte, error) {
			// 反序列化data
			b, err := DeserializeBlock(data)
			if err != nil || b == nil {
				return nil, fmt.Errorf("deserialize block failed %w", err)
			}

			// 更新etag
			b.Header.Etag = md5.Sum(b.Data)

			// 获取配置信息
			compress := params["compress"] == "true"
			encrypted := params["encrypted"] == "true"
			if compress {
				// 压缩
				if d, err := utils.Compress(b.Data); err != nil || d == nil {
					return nil, fmt.Errorf("compress block failed %w", err)
				} else {
					// 如果压缩后的大小没有什么压缩空间，就放弃，为后续读提速
					if float32(len(d))/float32(len(b.Data)) > 0.8 {
						// 放弃
					} else {
						b.Data = d
						b.Header.Compressed = true
						b.Header.RealSize = int64(len(b.Data))
					}
				}
			}

			if encrypted {
				// 加密
				if d, err := utils.Encrypt(b.Data, block.Header.ID+fs.BlockConf.Password); err != nil || d == nil {
					return nil, fmt.Errorf("encrypt block failed %w", err)
				} else {
					b.Data = d
					b.Header.Encrypted = true
					b.Header.RealSize = int64(len(b.Data))
				}
			}
			logger.Debugf("block %s: %d chunks, size %d:%d", block.Header.ID, len(block.Header.ChunkList), len(data), len(b.Data))
			// 序列化data
			return SerializeBlock(b)
		},
		Params: map[string]string{
			"compress":  strconv.FormatBool(fs.BlockConf.Compress),
			"encrypted": strconv.FormatBool(fs.BlockConf.Encrypt),
		},
	}

	if err := mfs.Write(blockPath, data, processor); err != nil {
		logger.Errorf("failed to write block to memfs: %v", err)
		return fmt.Errorf("memfs write block failed %w", err)
	}
	logger.Debugf("block %s successfully saved", block.Header.ID)

	cacheKey := "block:" + fs.ID + ":" + block.Header.ID
	G_BLOCK_CACHE.Put(cacheKey, block)
	return nil
}

func RemoveChunkFromBlock(chunk *Chunk, fs *DedupFS) error {
	logger.Debugf("removing chunk %s from block %s in filesystem %s", chunk.Hash, chunk.BlockID, fs.ID)

	// current block 清理
	key := fmt.Sprintf("block:%s", fs.ID)
	if b, ok := CURRENT_BLOCK.Load(key); ok && b != nil {
		if curBlock, yes := b.(*Block); yes && curBlock != nil {
			if err := curBlock.RemoveChunk(chunk); err != nil {
				logger.Errorf("failed to remove chunk from current block: %v", err)
				return fmt.Errorf("remove chunk from current block failed %w", err)
			} else {
				if len(curBlock.Header.ChunkList) == 0 {
					logger.Errorf("current block %s is empty, removing it", curBlock.Header.ID)
					CURRENT_BLOCK.Store(key, nil)
				}
				CURRENT_BLOCK.Store(key, curBlock)
			}
		} else {
			logger.Errorf("invalid block type: %T", b)
			return fmt.Errorf("invalid block type: %T", b)
		}
	}

	block, err := ReadBlock(chunk.BlockID, fs)
	if err != nil || block == nil {
		logger.Errorf("failed to read block %s: %v", chunk.BlockID, err)
		return fmt.Errorf("read block failed %w", err)
	}

	if err := block.RemoveChunk(chunk); err != nil {
		logger.Errorf("failed to remove chunk from block: %v", err)
		return fmt.Errorf("remove chunk from block failed %w", err)
	} else {
		if len(block.Header.ChunkList) == 0 {
			// 删除 block 文件
			blockPath := filepath.Join(fs.DataDir, GetBlockPath(chunk.BlockID))
			logger.Errorf("removing block %s from memfs", blockPath)
			mfs := memfs.GetInstance()
			if mfs == nil {
				logger.Errorf("memfs not initialized")
				return fmt.Errorf("memfs not initialized")
			}
			if err := mfs.Remove(blockPath); err != nil {
				logger.Errorf("failed to remove block from memfs: %v", err)
				return fmt.Errorf("memfs remove block failed %w", err)
			}

			cacheKey := "block:" + fs.ID + ":" + block.Header.ID
			G_BLOCK_CACHE.Del(cacheKey)
			return nil
		} else {
			if err := SaveBlock(block, fs); err != nil {
				logger.Errorf("failed to save block after removing chunk: %v", err)
				return fmt.Errorf("save block failed %w", err)
			}
			logger.Debugf("successfully removed chunk %s from block %s", chunk.Hash, chunk.BlockID)
			return nil
		}
	}
}

func AddChunkToBlock(chunk *Chunk, fs *DedupFS) error {
	// 先获取 current block
	key := fmt.Sprintf("block:%s", fs.ID)
	_b, ok := CURRENT_BLOCK.Load(key)
	if !ok || _b == nil {
		logger.Debugf("no current block found for filesystem %s, creating a new one", fs.ID)
		if b, err := NewBlock(); err != nil || b == nil {
			logger.Errorf("failed to create new block: %v", err)
			return fmt.Errorf("create new block failed %w", err)
		} else {
			_b = b
		}
	}
	block, yes := _b.(*Block)
	if !yes || block == nil {
		logger.Errorf("invalid block type: %T", _b)
		return fmt.Errorf("invalid block type: %T", _b)
	}

	block.Header.ChunkList = append(block.Header.ChunkList, &BlockChunk{Hash: chunk.Hash, Size: chunk.Size})
	block.Data = append(block.Data, chunk.Data...)
	chunk.BlockID = block.Header.ID
	logger.Debugf("added chunk %s data size %d to block %s block size %d ", chunk.Hash, len(chunk.Data), block.Header.ID, len(block.Data))
	if len(block.Data) >= int(fs.BlockConf.Size) {
		if err := SaveBlock(block, fs); err != nil {
			logger.Errorf("failed to save block %s: %v", block.Header.ID, err)
			return fmt.Errorf("failed to save block %s: %w", block.Header.ID, err)
		} else {
			logger.Info("block %s size %d:%d successfully saved", block.Header.ID, len(block.Data), block.Header.TotalSize)
			CURRENT_BLOCK.Store(key, nil)
		}
	} else {
		CURRENT_BLOCK.Store(key, block)
	}

	cacheKey := "block:" + fs.ID + ":" + block.Header.ID
	G_BLOCK_CACHE.Put(cacheKey, block)

	return nil
}
