use blake3;
use anyhow::Result;
use std::any::Any;
use std::collections::HashMap;
use std::sync::{RwLock, LazyLock};
use fastcdc::v2020::FastCDC;
use serde::{Deserialize, Serialize};
use tracing::{error, info};
use crate::config::ChunkConfig;
use crate::vfile::DedupFS;
use crate::kvstore::{make_prefixed_key, key_prefix};
use crate::block::read_block;
use crate::cache::{CacheItem, new_shared_cache, SharedCache};
use crate::errors::{ChunkNotFound, ChunkDataNotFound};

// 创建全局缓存实例，容量为2GB
pub static G_CHUNK_BLOCK_CACHE: LazyLock<SharedCache<String, Box<dyn CacheItem + Send + Sync>>> = LazyLock::new(|| new_shared_cache(2 * 1024 * 1024 * 1024));

/// Chunk 结构表示一个数据块
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Chunk {
    /// 内容的哈希
    pub hash: String,
    /// 块大小(字节)
    pub size: i32,
    /// 引用计数
    pub ref_count: i32,
    /// 所属BlockID
    pub block_id: String,
    /// 仅用于内存操作，不持久化
    #[serde(skip)]
    pub data: Vec<u8>,
}

impl Chunk {
    /// 创建新的数据块
    pub fn new(data: Vec<u8>) -> Self {
        let mut chunk = Chunk {
            hash: String::new(),
            size: data.len() as i32,
            ref_count: 0,
            block_id: "".to_string(),
            data,
        };
        chunk.hash = calc_hash(&chunk.data);
        chunk
    }
    /// 合并另一个块的数据到这个块
    pub fn merge(&mut self, other: &Chunk) {
        self.data.extend_from_slice(&other.data);
        self.size = self.data.len() as i32;
        self.hash = calc_hash(&self.data); // 重新计算哈希
    }
}

// 实现CacheItem特质，用于缓存和类型转换
impl CacheItem for Chunk {
    // 返回chunk的大小，用于缓存容量计算
    fn size(&self) -> usize {
        // 包括数据大小和其他字段的估计大小
        self.data.len() + 
        self.hash.len() + 
        self.block_id.len() + 
        12 // size(4) + ref_count(4) + 其他字段的估计(4)
    }
    
    // 转换为Any类型
    fn as_any(&self) -> &dyn Any {
        self
    }
    
    // 转换为Any类型的可变引用
    fn as_any_mut(&mut self) -> &mut dyn Any {
        self
    }
}

pub fn calc_hash( data: &[u8]) -> String {
    let hash = blake3::hash(data).to_hex().to_string();
    return hash; 
}

// 保留原有函数以保持兼容性
pub fn do_chunking(data: &[u8], fs: &DedupFS) -> Result<Vec<Chunk>> {
    info!("starting chunking process for data of size {} bytes", data.len());
    let config = &fs.chunk_conf;
    // 把 config 内容 打印到控制台
    info!("chunking configuration: {:?}", config);

    let mut chunks = Vec::new();
    
    if config.fixed_size {
        // 固定大小分块
        let chunk_size = config.avg_size as usize;
        for chunk_data in data.chunks(chunk_size) {
            let chunk = Chunk::new(chunk_data.to_vec());
            chunks.push(chunk);
        }
    } else {
        // 使用 FastCDC 进行内容定义分块
        let chunker = FastCDC::new(
            data,
            (config.min_size as usize).try_into().map_err(|e: std::num::TryFromIntError| {
                error!("chunk size conversion error for min_size {}: {}", config.min_size, e);
                crate::errors::new(crate::errors::ChunkSizeConversionError { size: config.min_size.to_string(), error: e.to_string() })
            })?,
            (config.avg_size as usize).try_into().map_err(|e: std::num::TryFromIntError| {
                error!("chunk size conversion error for avg_size {}: {}", config.avg_size, e);
                crate::errors::new(crate::errors::ChunkSizeConversionError { size: config.avg_size.to_string(), error: e.to_string() })
            })?,
            (config.max_size as usize).try_into().map_err(|e: std::num::TryFromIntError| {
                error!("chunk size conversion error for max_size {}: {}", config.max_size, e);
                crate::errors::new(crate::errors::ChunkSizeConversionError { size: config.max_size.to_string(), error: e.to_string() })
            })?,
        );
        
        for entry in chunker {
            let chunk_data = &data[entry.offset..entry.offset + entry.length];
            let chunk = Chunk::new(chunk_data.to_vec());
            chunks.push(chunk);
        }
    }

    // 后处理：合并太小的最后一个块
    if chunks.len() > 1 {
        let last_chunk = chunks.last().unwrap();
        let min_acceptable_size = (config.avg_size / 2) as i32;
        
        if last_chunk.size < min_acceptable_size {
            // 最后一个块太小，合并到前一个块
            info!("merging small last chunk (size: {}) with previous chunk", last_chunk.size);
            let last_chunk = chunks.pop().unwrap();
            let second_last_index = chunks.len() - 1;
            chunks[second_last_index].merge(&last_chunk);
        }
    }
    
    info!("chunking completed, generated {} chunks", chunks.len());
    Ok(chunks)
}

pub fn get_chunk_meta(hash: &str, fs: &DedupFS) -> Result<Chunk> {
    info!("retrieving chunk metadata with hash: {}", hash);
    
    // 将&str转换为String以便在缓存中使用
    let hash_string = hash.to_string();
    
    // 先从全局缓存中读取
    let cache_key = format!("{}:{}:{}", key_prefix::CHUNK, fs.id, hash_string);
    // 重新实现缓存读取逻辑，避免在trait object上调用clone
    if let Some(chunk_arc) = G_CHUNK_BLOCK_CACHE.get(&cache_key) {
        // 按照编译器建议，使用引用方式进行类型转换
        let chunk_box_ptr = &*chunk_arc as *const _ as *const Box<Chunk>;
        if !chunk_box_ptr.is_null() {
            // 安全地读取缓存中的Chunk数据
            let chunk_data = unsafe { &*chunk_box_ptr };
            let chunk = Chunk {
                hash: chunk_data.hash.clone(),
                size: chunk_data.size,
                ref_count: chunk_data.ref_count,
                block_id: chunk_data.block_id.clone(),
                data: chunk_data.data.clone(),
            };
            info!("chunk metadata found in cache with hash: {}, size: {}, ref_count: {}, block_id: {}", 
                 hash, chunk.size, chunk.ref_count, chunk.block_id);
            return Ok(chunk);
        }
    }
    
    // 缓存未命中，从KVStore获取chunk元数据
    info!("retrieving chunk metadata from kvstore with hash: {}", hash);
    let chunk_key = make_prefixed_key(key_prefix::CHUNK, hash.as_bytes());
    
    let chunk = fs.kv_store.get::<Chunk>(&chunk_key)?
        .ok_or_else(|| {
            error!("chunk metadata not found with hash: {}", hash);
            ChunkNotFound { hash: hash.to_string() }
        })?;
    
    // 使用全局缓存，key为fs.id + ":" + hash
    let cache_key = format!("{}:{}:{}",key_prefix::CHUNK, fs.id, hash_string);
    G_CHUNK_BLOCK_CACHE.put(cache_key, Box::new(chunk.clone()), |item| item.size());
    
    info!("successfully retrieved chunk metadata with hash: {}, size: {}, ref_count: {}, block_id: {}", 
         hash, chunk.size, chunk.ref_count, chunk.block_id);
    
    Ok(chunk)
}

pub fn get_chunk_data(hash: &str, fs: &DedupFS) -> Result<Chunk> {
    info!("retrieving chunk with hash: {}", hash);
    
    // 首先调用get_chunk_meta获取元数据
    let mut chunk = get_chunk_meta(hash, fs)?;
    
    // 检查chunk.data是否已经有数据
    if !chunk.data.is_empty() {
        info!("chunk data already available for hash: {}, size: {} bytes", hash, chunk.data.len());
        return Ok(chunk);
    }
    
    info!("chunk data is empty for hash: {}, proceeding to load from block", hash);
    
    // 从元数据中获取block id（克隆以避免部分移动）
    let block_id = chunk.block_id.clone();
    error!("found chunk metadata, retrieving from block: {}", block_id);
    
    // 调用read_block函数获取block数据
    let block = read_block(&block_id, fs)?;
    
    // 从block中找到对应的chunk数据
    // 计算chunk在block中的偏移量
    let mut offset = 0;
    let mut target_chunk_data = None;
    
    for chunk_info in &block.header.chunk_list {
        if chunk_info.hash == hash {
            // 找到目标chunk，从block数据中提取
            let chunk_size = chunk_info.size as usize;
            info!("found chunk in block, size: {}, offset: {}", chunk_size, offset);
            if offset + chunk_size <= block.data.len() {
                target_chunk_data = Some(block.data[offset..offset + chunk_size].to_vec());
            } else {
                error!("chunk size {} exceeds available block data len {} at offset {}", chunk_size, block.data.len(), offset);
            }
            break;
        }
        // 更新偏移量
        offset += chunk_info.size as usize;
    }
    
    // 检查是否找到chunk数据
    let chunk_data = target_chunk_data
        .ok_or_else(|| {
            error!("chunk data not found in block {} for hash: {}", block_id, hash);
            ChunkDataNotFound { block_id, hash: hash.to_string() }
        })?;
    
    // 设置chunk数据
    chunk.data = chunk_data;
    
    // 使用全局缓存，key为fs.id + ":" + hash
    let cache_key = format!("{}:{}:{}",key_prefix::CHUNK, fs.id, hash);
    G_CHUNK_BLOCK_CACHE.put(cache_key, Box::new(chunk.clone()), |item| item.size());
    
    info!("successfully retrieved chunk with hash: {}, size: {}", hash, chunk.size);
    
    Ok(chunk)
}

pub fn remove_chunk(hash: &str, fs: &DedupFS) -> Result<()> {
    info!("remove_chunk - removing chunk with hash: {}", hash);
    
    // 先从kv_store 中 获取 chunk
    let chunk_key = make_prefixed_key(key_prefix::CHUNK, hash.as_bytes());
    let mut chunk = fs.kv_store.get::<Chunk>(&chunk_key)?
        .ok_or_else(|| {
            error!("remove_chunk - chunk not found with hash: {}", hash);
            ChunkNotFound { hash: hash.to_string() }
        })?;
    
    // 对引用计数 ref_count - 1
    chunk.ref_count -= 1;
    info!("remove_chunk - chunk {} ref_count updated to {}", hash, chunk.ref_count);
    
    if chunk.ref_count == 0 {
        // 如果引用计数为0，需要调用 block:remove_chunk 从对应的 block 中删除该chunk
        info!("remove_chunk - chunk {} ref_count is 0, removing from block {}", hash, chunk.block_id);
        
        // 从block中删除chunk数据
        crate::block::remove_chunk_from_block(&chunk, fs)?;
        
        // 从kv_store中删除chunk元数据
        fs.kv_store.del(&chunk_key)?;
        info!("remove_chunk - chunk {} deleted from kv_store", hash);
        
        // 从缓存中移除
        let cache_key = format!("{}:{}:{}", key_prefix::CHUNK, fs.id, hash);
        G_CHUNK_BLOCK_CACHE.del(&cache_key);
        info!("remove_chunk - chunk {} removed from cache", hash);
    } else {
        // 如果引用计数大于0，保存更新后的chunk到kv_store
        fs.kv_store.set(&chunk_key, &chunk)?;
        info!("remove_chunk - updated chunk {} with ref_count {} saved to kv_store", hash, chunk.ref_count);
        
        // 更新缓存
        let cache_key = format!("{}:{}:{}",key_prefix::CHUNK, fs.id, hash);
        G_CHUNK_BLOCK_CACHE.put(cache_key, Box::new(chunk.clone()), |item| item.size());
    }
    
    info!("remove_chunk - successfully processed chunk {}", hash);
    Ok(())
}
