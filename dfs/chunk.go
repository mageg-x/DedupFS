package dfs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	fastcdc "github.com/PlakarKorp/go-cdc-chunkers"
	_ "github.com/PlakarKorp/go-cdc-chunkers/chunkers/fastcdc"
	"github.com/cespare/xxhash/v2"

	"github.com/mageg-x/dedupfs/internal/cache"
)

var (
	G_CHUNK_CACHE = cache.NewCache[string, *Chunk](1024*1024*1024, true)
)

// Chunk 表示一个数据块
type Chunk struct {
	Hash     string `json:"hash"`
	Size     int32  `json:"size"`
	RefCount int32  `json:"ref_count"`
	BlockID  string `json:"block_id"`
	Data     []byte `json:"-"` // 仅内存使用，不持久化
}

// MarshalJSONWithData 返回包含 Data 字段的 JSON
func (c *Chunk) MarshalJSONWithData() ([]byte, error) {
	type Alias Chunk
	return json.Marshal(&struct {
		Data []byte `json:"data"`
		*Alias
	}{
		Data:  c.Data,
		Alias: (*Alias)(c),
	})
}
func (c *Chunk) UnmarshalJSONWithData(data []byte) error {
	type Alias Chunk
	aux := &struct {
		Data []byte `json:"data"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	c.Data = aux.Data
	return nil
}

// 实现 CacheItem
func (c *Chunk) Len() int {
	return len(c.Data) + len(c.Hash) + len(c.BlockID) + 12 // 估算其他字段
}

// Merge 合并另一个 chunk 的数据
func (c *Chunk) Merge(other *Chunk) {
	logger.Debugf("merging chunks: %s + %s", c.Hash, other.Hash)
	c.Data = append(c.Data, other.Data...)
	c.Size = int32(len(c.Data))
	c.Hash = calcHash(c.Data)
	logger.Debugf("merged chunk hash: %s, size: %d", c.Hash, c.Size)
}

// calcHash 使用 blake3
func calcHash(data []byte) string {
	// fp := blake3.Sum256(data)
	// hash := hex.EncodeToString(fp[:20])
	fp := xxhash.Sum64(data)
	hash := fmt.Sprintf("%x", fp)
	return hash
}

// NewChunk 创建新块
func NewChunk(data []byte) *Chunk {
	hash := calcHash(data)
	logger.Debugf("creating new chunk with hash %s, size: %d bytes", hash, len(data))
	return &Chunk{
		Hash:     hash,
		Size:     int32(len(data)),
		RefCount: 0,
		BlockID:  "",
		Data:     data,
	}
}

// GetChunkMeta 获取元数据（不含 Data）
func GetChunkMeta(hash string, fs *DedupFS) (*Chunk, error) {
	logger.Debugf("getting chunk meta for hash %s in filesystem %s", hash, fs.ID)
	cacheKey := fmt.Sprintf("chunk:%s:%s", fs.ID, hash)
	if chunk, ok := G_CHUNK_CACHE.Get(cacheKey); ok && chunk != nil {
		logger.Debugf("chunk meta %s found in cache", hash)
		return chunk, nil
	}

	// 从 KVStore 读取
	chunkKey := fmt.Sprintf("chunk:%s", hash)
	var chunk Chunk
	if err := fs.KVStore.Get(chunkKey, &chunk); err != nil {
		logger.Warnf("chunk %s not found in kvstore", hash)
		return nil, fmt.Errorf("chunk not found")
	}

	G_CHUNK_CACHE.Put(cacheKey, &chunk)
	logger.Debugf("loaded chunk meta %s from kvstore", hash)

	return &chunk, nil
}

// GetChunkData 获取完整 chunk（含 Data）
func GetChunkData(hash string, fs *DedupFS) (*Chunk, error) {
	logger.Debugf("getting chunk data for hash %s in filesystem %s", hash, fs.ID)
	if len(hash) == 0 || fs == nil {
		logger.Errorf("invalid arguments: hash=%s, fs=%v", hash, fs)
		return nil, fmt.Errorf("invalid arguments")
	}
	// 先取元数据
	chunk, err := GetChunkMeta(hash, fs)
	if err != nil || chunk == nil {
		logger.Errorf("failed to get chunk meta for %s: %v", hash, err)
		return nil, err
	}

	if len(chunk.Data) > 0 {
		logger.Debugf("chunk %s data already available", hash)
		return chunk, nil
	}

	// 读 block
	block, err := ReadBlock(chunk.BlockID, fs)
	if err != nil || block == nil {
		logger.Errorf("failed to read block %s for chunk %s: %v", chunk.BlockID, hash, err)
		return nil, err
	}

	// 在 block 中找 chunk
	offset := 0
	var chunkData []byte
	for _, c := range block.Header.ChunkList {
		if c.Hash == hash {
			end := offset + int(c.Size)
			if end > len(block.Data) {
				logger.Errorf("chunk %s out of bounds in block %s", hash, chunk.BlockID)
				return nil, fmt.Errorf("chunk out of bounds in block %s", chunk.BlockID)
			}
			chunkData = block.Data[offset:end]
			break
		}
		offset += int(c.Size)
	}

	if chunkData == nil {
		logger.Errorf("failed to read chunk data for %s from block %s", hash, chunk.BlockID)
		return nil, fmt.Errorf("failed to read chunk data")
	}

	chunk.Data = chunkData
	cacheKey := fmt.Sprintf("chunk:%s:%s", fs.ID, hash)
	G_CHUNK_CACHE.Put(cacheKey, chunk)
	logger.Debugf("successfully loaded chunk data for %s, size: %d bytes", hash, len(chunkData))

	return chunk, nil
}

// RemoveChunk 减引用计数，必要时删除
func RemoveChunk(hash string, fs *DedupFS) error {
	logger.Debugf("removing chunk %s from filesystem %s", hash, fs.ID)
	chunkKey := fmt.Sprintf("chunk:%s", hash)
	var chunk Chunk
	if err := fs.KVStore.Get(chunkKey, &chunk); err != nil {
		logger.Errorf("chunk %s not found for removal: %v", hash, err)
		return fmt.Errorf("chunk not found")
	}

	chunk.RefCount--
	logger.Debugf("chunk %s ref count decremented to %d", hash, chunk.RefCount)
	if chunk.RefCount <= 0 {
		logger.Debugf("chunk %s ref count zero, removing completely", hash)
		// 从 block 中删除
		if err := RemoveChunkFromBlock(&chunk, fs); err != nil {
			logger.Errorf("failed to remove chunk %s from block: %v", hash, err)
			return err
		}
		// 删除元数据
		if err := fs.KVStore.Del(chunkKey); err != nil {
			logger.Errorf("failed to delete chunk %s metadata: %v", hash, err)
			return err
		}
		// 清缓存
		cacheKey := fmt.Sprintf("chunk:%s:%s", fs.ID, hash)
		G_CHUNK_CACHE.Del(cacheKey)
		logger.Infof("chunk %s completely removed", hash)
	} else {
		// 更新引用计数
		if err := fs.KVStore.Set(chunkKey, &chunk); err != nil {
			logger.Errorf("failed to update chunk %s ref count: %v", hash, err)
			return err
		}
		cacheKey := fmt.Sprintf("chunk:%s:%s", fs.ID, hash)
		G_CHUNK_CACHE.Put(cacheKey, &chunk)
		logger.Debugf("updated chunk %s ref count to %d", hash, chunk.RefCount)
	}

	return nil
}

func PutChunks(chunks []*Chunk, fs *DedupFS) error {
	logger.Debugf("putting %d chunks into block", len(chunks))
	for _, chunk := range chunks {
		if int32(len(chunk.Data)) != chunk.Size {
			logger.Errorf("chunk size mismatch: %s expected=%d actual=%d", chunk.Hash, chunk.Size, len(chunk.Data))
			return fmt.Errorf("hash=%s expected=%d actual=%d", chunk.Hash, chunk.Size, len(chunk.Data))
		}

		// 从 KVStore 读取
		chunkKey := fmt.Sprintf("chunk:%s", chunk.Hash)
		var c Chunk
		var exist bool
		if err := fs.KVStore.Get(chunkKey, &c); err != nil {
			logger.Warnf("chunk %s not found in kvstore", chunk.Hash)
			chunk.RefCount = 1
		} else {
			chunk.RefCount = c.RefCount + 1
			chunk.BlockID = c.BlockID
			exist = true
			logger.Debugf("chunk %s:%d:%d found in kvstore", chunk.Hash, chunk.RefCount, chunk.Size)
		}

		if !exist {
			logger.Debugf("chunk %s size %d adding to block", chunk.Hash, len(chunk.Data))
			if err := AddChunkToBlock(chunk, fs); err != nil {
				logger.Errorf("failed to add chunk %s to block: %v", chunk.Hash, err)
				return fmt.Errorf("failed to add chunk %s to block: %w", chunk.Hash, err)
			}
		}

		logger.Debugf("chunk %s:%d:%d block %s saved to kvstore", chunk.Hash, chunk.RefCount, chunk.Size, chunk.BlockID)

		if err := fs.KVStore.Set(chunkKey, chunk); err != nil {
			logger.Errorf("failed to save chunk %s metadata: %v", chunk.Hash, err)
			return fmt.Errorf("failed to save chunk %s metadata: %w", chunk.Hash, err)
		}

		cacheKey := fmt.Sprintf("chunk:%s:%s", fs.ID, chunk.Hash)
		G_CHUNK_CACHE.Put(cacheKey, chunk)
	}
	return nil
}

// DoChunking 分块主函数
func DoChunking(data []byte, fs *DedupFS) ([]*Chunk, error) {
	logger.Debugf("starting chunking process for %d bytes of data", len(data))
	cfg := fs.ChunkConf
	var chunks []*Chunk

	if cfg.FixedSize {
		logger.Debugf("using fixed size chunking with size %d", cfg.AvgSize)
		chunkSize := int(cfg.AvgSize)
		for i := 0; i < len(data); i += chunkSize {
			end := i + chunkSize
			if end > len(data) {
				end = len(data)
			}
			chunk := NewChunk(data[i:end])
			chunks = append(chunks, chunk)
		}
	} else {
		logger.Infof("using cdc chunking: min=%d, avg=%d, max=%d", cfg.MinSize, cfg.AvgSize, cfg.MaxSize)
		chunker, err := fastcdc.NewChunker("fastcdc", bytes.NewReader(data), &fastcdc.ChunkerOpts{
			MinSize:    int(cfg.MinSize),
			NormalSize: int(cfg.AvgSize),
			MaxSize:    int(cfg.MaxSize),
		})

		if err != nil || chunker == nil {
			logger.Errorf("fastcdc init failed: %v", err)
			return nil, fmt.Errorf("fastcdc init failed: %w", err)
		}

		for {
			chunkData, err := chunker.Next()
			if err != nil && err != io.EOF {
				logger.Errorf("cdc chunk error: %v", err)
				return nil, fmt.Errorf("cdc chunk error: %w", err)
			}
			if len(chunkData) > 0 {
				chunk := NewChunk(chunkData)
				chunks = append(chunks, chunk)
			}

			if err == io.EOF {
				break
			}
		}
	}

	// 合并过小的最后一个块
	if len(chunks) > 1 {
		last := chunks[len(chunks)-1]
		minAcceptable := cfg.AvgSize / 2
		if int64(last.Size) < minAcceptable {
			logger.Debugf("last chunk too small (%d bytes), merging with previous chunk", last.Size)
			prev := chunks[len(chunks)-2]
			prev.Merge(last)
			chunks = chunks[:len(chunks)-1]
		}
	}

	logger.Debugf("chunking completed, created %d chunks", len(chunks))
	return chunks, nil
}
