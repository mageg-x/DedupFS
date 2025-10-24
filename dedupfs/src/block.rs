use anyhow::Result;
use crate::errors::{BlockNotFound, BlockReadFailed, BlockDeleteFailed, BlockSizeMismatch, ChunkSizeMismatch, CompressionFailed, BlockDeserializationFailed, BlockCreationFailed, DecryptionFailed, KeyGenerationFailed, DecompressionFailed, ChunkRefCountUpdateFailed, ChunkMetadataSaveFailed, ChunkExistenceCheckFailed, BlockSaveFailed, ChunkDataNotFound};
use crate::chunk::G_CHUNK_BLOCK_CACHE;
use std::any::Any;
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::RwLock;
use std::sync::LazyLock;
use backtrace::Backtrace;

use serde::{Deserialize, Serialize};
use std::time::{SystemTime, UNIX_EPOCH};
use uuid::Uuid;
use crate::chunk::Chunk;
use crate::kvstore::{KVStore, key_prefix, make_prefixed_key};
use std::sync::Arc;
use tracing::{info, error, debug};
use zstd::stream::copy_encode;
use std::io::Cursor;
use crate::utils::{encrypt_data, decrypt_data, compress_data, decompress_data};
use crate::cache::CacheItem;
use rand::Rng;
use base64::{Engine as _, engine::general_purpose::STANDARD as BASE64_ENGINE};
use std::collections::HashSet as Set;
use crate::vfile::DedupFS;


/// BlockChunk 表示块中的一个块条目
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BlockChunk {
    /// 块哈希
    pub hash: String,
    /// 块大小
    pub size: i32,
}

/// BlockHeader，存放在磁盘上只包含头信息，不含 Data
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct BlockHeader {
    /// 块ID
    pub id: String,
    /// 版本号
    pub ver: i32,
    /// ETag
    pub etag: [u8; 16],
    /// 总大小
    pub total_size: i64,
    /// 实际占用大小
    pub real_size: i64,
    /// 是否压缩
    pub compressed: bool,
    /// 是否加密
    pub encrypted: bool,
    /// 切片列表
    pub chunk_list: Vec<BlockChunk>,
    /// 创建时间（Unix时间戳，纳秒）
    pub created_at: u128,
    /// 更新时间（Unix时间戳，纳秒）
    pub updated_at: u128,
}

/// BlockData: 完整结构（包含 Data）
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Block {
    /// 块头信息
    pub header: BlockHeader,
    /// 块数据
    pub data: Vec<u8>,
}

impl Block {
    /// 创建新的块
    pub fn new() -> Result<Self> {
        let now = SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .unwrap_or_default()
            .as_nanos();
        let block_id = generate_block_id();
        Ok(Block {
            header: BlockHeader {
                id: block_id,
                ver: 1,
                etag: [0; 16],
                total_size: 0,
                real_size: 0,
                compressed: false,
                encrypted: false,
                chunk_list: Vec::new(),
                created_at: now,
                updated_at: now,
            },
            data: Vec::new(),
        })
    }
}

// 确保只导入一次CacheItem特性
// 实现CacheItem特质，用于缓存和类型转换
impl CacheItem for Block {
    // 返回block的大小，用于缓存容量计算
    fn size(&self) -> usize {
        // 包括数据大小和头信息的估计大小
        self.data.len() + 
        self.header.id.len() + 
        self.header.chunk_list.len() * 32 + // 每个chunk估计32字节
        64 // 其他字段的估计大小（版本、etag、时间戳等）
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

/// 生成block id的函数
/// 格式：时间戳部分(16位十六进制) + UUID部分(16位)
pub fn generate_block_id() -> String {
    // 生成UUID并移除所有连字符
    let uuid = Uuid::new_v4().to_string().replace("-", "");
    
    // 获取当前时间戳（纳秒级）
    let now = SystemTime::now();
    let timestamp = now.duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
    
    // 使用完整的时间戳确保顺序性，格式化为16位十六进制，填充前导零
    let time_part = format!("{:016x}", timestamp);
    
    // 添加较短的随机部分确保唯一性，只取UUID的前16位
    let random_part = &uuid[..16];
    
    // 返回组合后的block id
    time_part + random_part
}

/// 根据blockID生成文件存储路径
pub fn get_block_path(block_id: &str) -> PathBuf {
    let n = block_id.len();
    
    // 确保blockID长度足够
    if n < 9 {
        // 如果blockID太短，使用一种简化的路径结构
        return Path::new("blocks").join("blocks").join(block_id);
    }
    
    // 提取路径组件
    let dir1 = &block_id[n-3..];      // 最后3位
    let dir2 = &block_id[n-6..n-3];   // 倒数第4-6位
    let dir3 = &block_id[n-9..n-6];   // 倒数第7-9位
    
    // 构建完整路径
    Path::new("blocks").join(dir1).join(dir2).join(dir3).join(block_id)
}

pub fn read_block(block_id: &String, fs: &DedupFS) -> Result<Block> {
    // 构建缓存键
    let cache_key = format!("{}:{}", key_prefix::BLOCK, block_id);
    
    // 尝试从缓存获取
    if let Some(block_box_arc) = G_CHUNK_BLOCK_CACHE.get(&cache_key) {
        info!("read_block - found block {} in cache", block_id);
        
        // 直接从Box<dyn CacheItem>向下转换到Block类型
        // 由于Arc不支持直接downcast，我们需要获取内部的Box引用
        let box_ref: &Box<dyn CacheItem + Send + Sync> = &*block_box_arc;
        
        // 使用Any特质的downcast_ref方法安全地转换类型
        if let Some(block_ref) = box_ref.as_any().downcast_ref::<Block>() {
            return Ok(block_ref.clone());
        }
        
        // 如果类型转换失败，记录错误并继续从文件读取
        error!("read_block - failed to downcast cached item to Block type for block {}", block_id);
    }
    
    // 构建block文件路径
    let block_path = fs.data_path.join(get_block_path(block_id));

    // 检查block文件是否存在
    if !block_path.exists() {
        error!("read_block - block not found: {}, path: {:?}", block_id, block_path);
        return Err(BlockNotFound { block_id: block_id.to_string() }.into());
    }
    
    // 读取block文件内容
    let block_data = match crate::memfs::read(&block_path) {
        Ok(data) => data,
        Err(e) => {
            error!("read_block - failed to read block file: {}, path: {:?}, error: {:?}", block_id, block_path, e);
            return Err(BlockReadFailed { block_id: block_id.to_string(), error: e.to_string() }.into());
        }
    };
    
    // 反序列化block数据
    let mut block: Block = match bincode::deserialize(&block_data) {
        Ok(block) => block,
        Err(e) => {
            error!("read_block - failed to deserialize block: {}, error: {:?}", block_id, e);
            return Err(BlockDeserializationFailed { block_id: block_id.to_string(), error: e.to_string() }.into());
        }
    };

    // 校验 block.real_size 和 block.data 长度是否一致
    if block.header.real_size != block.data.len() as i64 {
        error!("read_block - block size mismatch: {}, expected: {}, actual: {}", block_id, block.header.real_size, block.data.len());
        return Err(BlockSizeMismatch { 
            block_id: block_id.to_string(), 
            expected_size: block.header.real_size.to_string(), 
            actual_size: (block.data.len() as i64).to_string() 
        }.into());
    }
    
    // 解密数据（如果需要）
    if block.header.encrypted {
        info!("read_block - decrypting block {}", block.header.id);
        match crate::utils::gen_key(&block.header.id, 16) {
            Ok(key_vec) => {
                match decrypt_data(&block.data, &key_vec) {
                    Ok(decrypted) => {
                        block.data = decrypted;
                        info!("read_block - block {} decryption successful", block.header.id);
                    },
                    Err(e) => {
                        error!("read_block - failed to decrypt block: {}, error: {:?}", block_id, e);
                        return Err(DecryptionFailed { reason: e.to_string() }.into());
                    }
                }
            },
            Err(e) => {
                error!("read_block - failed to generate decryption key for block: {}, error: {:?}", block_id, e);
                return Err(KeyGenerationFailed { block_id: block_id.to_string(), error: e.to_string() }.into());
            }
        }
    }
    
    // 解压数据（如果需要）
    if block.header.compressed {
        info!("read_block - decompressing block {}", block.header.id);
        match decompress_data(&block.data) {
            Ok(decompressed) => {
                block.data = decompressed;
                info!("read_block - block {} decompression len {} successful", block.header.id, block.data.len());
            },
            Err(e) => {
                error!("read_block - failed to decompress block: {}, error: {:?}", block_id, e);
                return Err(DecompressionFailed { block_id: block_id.to_string(), error: e.to_string() }.into());
            }
        }
    }

    // 打印 block header
    debug!("read_block - block header: {:?}, data len {}", block.header, block.data.len());
    // 校验 block.header.total_size 和 block.data.len() 一致性
    if block.header.total_size != block.data.len() as i64 {
        error!("read_block - block total size mismatch: {}, expected {}, actual {}", block.header.id, block.header.total_size, block.data.len());
        return Err(BlockSizeMismatch { 
            block_id: block.header.id.to_string(), 
            expected_size: block.header.total_size.to_string(), 
            actual_size: (block.data.len() as i64).to_string() 
        }.into());
    }
    
    // 校验 chunk_list 中所有 chunk 的大小总和
    let chunk_size_sum: i64 = block.header.chunk_list.iter().map(|chunk| chunk.size as i64).sum();
    if block.header.total_size != chunk_size_sum {
        error!("read_block - block chunk list size mismatch: {}, expected {}, actual chunk sum {}", block.header.id, block.header.total_size, chunk_size_sum);
        return Err(BlockSizeMismatch {
            block_id: block.header.id.to_string(),
            expected_size: block.header.total_size.to_string(),
            actual_size: chunk_size_sum.to_string()
        }.into());
    }
    
    // 检查block_id是否匹配
    if block.header.id != *block_id {
        error!("read_block - block_id mismatch: expected {}, got {}", block_id, block.header.id);
        return Err(BlockReadFailed { block_id: block_id.to_string(), error: format!("Block ID mismatch: expected {}, got {}", block_id, block.header.id) }.into());
    }
    
    // 将block保存到缓存中
    let boxed_block: Box<dyn CacheItem + Send + Sync> = Box::new(block.clone());
    G_CHUNK_BLOCK_CACHE.put(cache_key, boxed_block, |item| item.size());
    info!("read_block - added block {} to cache", block_id);
    
    Ok(block)
}

/// 对block数据进行压缩、加密处理并保存到磁盘
pub fn save_block(block: &Block, fs: &DedupFS) -> Result<Block> {
    // 创建block的副本，避免修改原始数据
    let mut saved_block = block.clone();

    // 打印 block信息
    error!("save_block - block id {}, data len {}", saved_block.header.id, saved_block.data.len());

    // 先更新etag
    let etag = blake3::hash(&saved_block.data);
    etag.as_bytes().iter().take(16).enumerate().for_each(|(i, &byte)| {
        saved_block.header.etag[i] = byte;
    });

    // 再统一更新一下total_size
    saved_block.header.total_size = 0;
    for chunk in &saved_block.header.chunk_list {
        saved_block.header.total_size += chunk.size as i64;
    }
    saved_block.header.real_size = saved_block.data.len() as i64;

    // 校验 block.header.total_size 和 block.data.len() 一致性
    if saved_block.header.total_size != saved_block.data.len() as i64 {
        error!("save_block - block total size mismatch: {}, expected {}, actual {}", saved_block.header.id, saved_block.header.total_size, saved_block.data.len());
        return Err(BlockSizeMismatch {
            block_id: saved_block.header.id.to_string(),
            expected_size: saved_block.header.total_size.to_string(),
            actual_size: (saved_block.data.len() as i64).to_string(),
        }.into())
    }
    
    // 更新时间戳
    saved_block.header.updated_at = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_nanos();
    
    // 更新缓存
    let cache_key = format!("{}:{}", key_prefix::BLOCK, saved_block.header.id);
    let boxed_block: Box<dyn CacheItem + Send + Sync> = Box::new(saved_block.clone());
    G_CHUNK_BLOCK_CACHE.put(cache_key, boxed_block, |b| b.size());

    // 先压缩
    if fs.block_conf.compress {
        info!("save_block - compressing block {} with zstd", saved_block.header.id);
        
        // 使用utils中的压缩函数，压缩级别设置为3
        match compress_data(&saved_block.data, 3) {
            Ok(compressed_result) => match compressed_result {
                Some(compressed_data) => {
                    saved_block.data = compressed_data;
                    saved_block.header.compressed = true;
                    saved_block.header.real_size = saved_block.data.len() as i64;
                    info!("save_block - block {} compression successful, original size: {} bytes, compressed: {} bytes", 
                          saved_block.header.id, saved_block.header.total_size, saved_block.data.len());
                },
                None => {
                    // 如果压缩失败，保持原始数据不变
                    saved_block.header.compressed = false;
                    info!("save_block - compression didn't reduce size for block {}, keeping original", saved_block.header.id);
                }
            },
            Err(e) => {
                error!("save_block - failed to compress block {}: {:?}", saved_block.header.id, e);
                return Err(CompressionFailed { block_id: saved_block.header.id.clone(), error: e.to_string() }.into());
            }
        }
    }
    
    // 后加密
    if fs.block_conf.encrypt {
        info!("save_block - encrypting block {} with aes-gcm", saved_block.header.id);
        
        // 使用gen_key函数生成密钥，参数为saved_block.header.id和16字节长度
        let key_vec = crate::utils::gen_key(&saved_block.header.id, 16)?;
        
        // 使用utils中的加密函数
        let encrypted_data = encrypt_data(&saved_block.data, &key_vec)?;
        
        saved_block.data = encrypted_data;
        saved_block.header.encrypted = true;
        saved_block.header.real_size = saved_block.data.len() as i64;
        info!("save_block - block {} encryption successful", saved_block.header.id);
    }
    
    // 保存block到磁盘
    let block_path = fs.data_path.join(get_block_path(&block.header.id));
    
    // 确保目录存在
    if let Some(parent_dir) = block_path.parent() {
        match std::fs::create_dir_all(parent_dir) {
            Ok(_) => {},
            Err(e) => {
                error!("save_block - failed to create directory for block {}: {:?}, error: {:?}", block.header.id, parent_dir, e);
                return Err(crate::errors::new(crate::errors::BlockDirectoryCreateFailed { 
                    path: parent_dir.display().to_string(), 
                    error: e.to_string() 
                }));
            }
        };
    }
    
    // 校验block数据 saved_block.total_size 和 saved_block.chunk_list chunk size总和 是否一致
    let chunk_size_sum: i64 = saved_block.header.chunk_list.iter().map(|chunk| chunk.size as i64).sum();
    if saved_block.header.total_size != chunk_size_sum {
        error!("save_block - block total size mismatch: {}, expected {}, actual {}", saved_block.header.id, saved_block.header.total_size, chunk_size_sum);
        return Err(crate::errors::new(crate::errors::BlockSizeMismatch {
            block_id: saved_block.header.id.clone(),
            expected_size: saved_block.header.total_size.to_string(),
            actual_size: chunk_size_sum.to_string()
        }));
    }
    // 校验block数据 saved_block.real_size 和 saved_block.data.len   是否一致
    if saved_block.header.real_size != saved_block.data.len() as i64 {
        error!("save_block - block real size mismatch: {}, expected {}, actual {}", saved_block.header.id, saved_block.header.real_size, saved_block.data.len() as i64);
        return Err(crate::errors::new(crate::errors::BlockSizeMismatch {
            block_id: saved_block.header.id.clone(),
            expected_size: saved_block.header.real_size.to_string(),
            actual_size: (saved_block.data.len() as i64).to_string()
        }));
    }

    // 序列化并保存block
    let block_data = match bincode::serialize(&saved_block) {
        Ok(data) => data,
        Err(e) => {
            error!("save_block - failed to serialize block {}: {:?}", saved_block.header.id, e);
            return Err(crate::errors::new(crate::errors::BlockSerializationFailed { 
                block_id: saved_block.header.id.clone(), 
                error: e.to_string() 
            }));
        }
    };
    
    match crate::memfs::write(&block_path, block_data) {
        Ok(_) => {},
        Err(e) => {
            error!("save_block - failed to write block {} to {:?}: {:?}", saved_block.header.id, block_path, e);
            return Err(crate::errors::new(crate::errors::BlockWriteFailed { 
                path: block_path.display().to_string(), 
                error: e.to_string() 
            }));
        }
    }

    // 打印 block header
    debug!("save_block - block header: {:?}, data len {}", saved_block.header, saved_block.data.len());
    
    Ok(saved_block)
}

/// 返回成功存储的block IDs列表
pub fn put_chunks(chunks: Vec<Chunk>, fs: &DedupFS) -> Result<Set<String>> {
    info!("put_chunks - starting processing {} chunks", chunks.len());
    if chunks.is_empty() {
        info!("put_chunks - no chunks provided, returning empty result");
        return Ok(Set::new());
    }
    
    let mut block_ids: Set<String> = Set::new();
    
    // 获取DedupFS实例中的current_block（使用RefCell内部可变性）
    let mut current_block = if let Some(block) = fs.current_block.borrow_mut().take() {
        info!("put_chunks - using current_block: {}", block.header.id);
        block
    } else {
        match Block::new() {
            Ok(new_block) => {
                info!("put_chunks - creating new block: {}", new_block.header.id);
                new_block
            },
            Err(e) => {
                error!("put_chunks - failed to create new block: {:?}", e);
                return Err(BlockCreationFailed { error: e.to_string() }.into());
            }
        }
    };
    
    for chunk in chunks {
        if chunk.size != chunk.data.len() as i32 {
            error!("put_chunks - chunk size mismatch: {}, expected {}, actual {}", chunk.hash, chunk.size, chunk.data.len());
            return Err(ChunkSizeMismatch {
                hash: chunk.hash.clone(),
                expected_size: chunk.size.to_string(),
                actual_size: chunk.data.len().to_string(),    
            }.into())
        }
        
        // 检查chunk是否已存在
        let chunk_key = make_prefixed_key(key_prefix::CHUNK, chunk.hash.as_bytes());
        
        match fs.kv_store.get::<Chunk>(&chunk_key) {
            Ok(Some(mut existing_chunk)) => {
                // 已存在，增加引用计数
                existing_chunk.ref_count += 1;
                match fs.kv_store.set(&chunk_key, &existing_chunk) {
                    Ok(_) => {
                        info!("put_chunks - chunk {} already exists, ref_count increased to {}", chunk.hash, existing_chunk.ref_count);
                        // 记录block ID
                        block_ids.insert(existing_chunk.block_id.clone());
                        continue;
                    },
                    Err(e) => {
                        error!("put_chunks - failed to update chunk {} ref_count: {:?}", chunk.hash, e);
                        return Err(ChunkRefCountUpdateFailed { hash: chunk.hash.clone(), error: e.to_string() }.into());
                    }
                }
            },
            Ok(None) => {
                // 不存在，创建新的chunk元数据
                let mut new_chunk = chunk.clone();
                new_chunk.ref_count = 1;
                new_chunk.block_id = current_block.header.id.clone();
                
                // 保存chunk元数据到KVStore（不包含数据）
                match fs.kv_store.set(&chunk_key, &new_chunk) {
                    Ok(_) => {
                        info!("put_chunks - added new chunk {}, size {} bytes", new_chunk.hash, new_chunk.size);
                    },
                    Err(e) => {
                        error!("put_chunks - failed to save chunk metadata for {}: {:?}", chunk.hash, e);
                        return Err(ChunkMetadataSaveFailed { hash: chunk.hash.clone(), error: e.to_string() }.into());
                    }
                }
                
                // 将chunk添加到block
                let block_chunk = BlockChunk {
                    hash: chunk.hash.clone(),
                    size: chunk.size,
                };

                current_block.header.chunk_list.push(block_chunk);
                current_block.header.total_size += chunk.size as i64;
                current_block.header.updated_at = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
                
                // 将数据添加到block的data字段
                current_block.data.extend_from_slice(&chunk.data);

                block_ids.insert(current_block.header.id.clone());
            },
            Err(e) => {
                error!("put_chunks - failed to check chunk existence for {}: {:?}", chunk.hash, e);
                return Err(ChunkExistenceCheckFailed { hash: chunk.hash.clone(), error: e.to_string() }.into());
            }
        }
        
        // 检查是否需要创建新block
        if  current_block.data.len() >= fs.block_conf.size {
            // 处理当前block并保存到磁盘
            match save_block(&current_block, &fs) {
                Ok(_saved_block) => {
                    // 这里我们不需要使用返回的block
                },
                Err(e) => {
                    error!("put_chunks - failed to save block: {:?}", e);
                    return Err(BlockSaveFailed { error: e.to_string() }.into());
                }
            }
                                
            // 创建新block
            current_block = match Block::new() {
                Ok(block) => {
                    info!("put_chunks - creating new block: {}", block.header.id);
                    block
                },
                Err(e) => {
                    error!("put_chunks - failed to create new block: {:?}", e);
                    return Err(BlockCreationFailed { error: e.to_string() }.into());
                }
            };
        }
    }

    // 最后处理当前block
    if current_block.data.len() > 0 {
        match save_block(&current_block, &fs) {
            Ok(saved_block) => {
                // 这里我们不需要使用返回的block
            },
            Err(e) => {
                error!("put_chunks - failed to save block: {:?}", e);
                return Err(BlockSaveFailed { error: e.to_string() }.into());
            }
        }
    }

    // 将当前block保存回DedupFS实例（使用RefCell内部可变性）
    *fs.current_block.borrow_mut() = Some(current_block);
    
    info!("put_chunks - successfully processed all chunks, involving {} blocks", block_ids.len());
    Ok(block_ids)
}

pub fn remove_chunk(chunk: &Chunk, fs: &DedupFS) -> Result<()> {
    info!("remove_chunk - removing chunk {} from block {}", chunk.hash, chunk.block_id);
    
    // 打印调用栈
    // let stack_trace = backtrace::Backtrace::new();
    // error!("call stack {:?}", stack_trace);
 
    // 根据 chunk block_id 获取 block
    let mut block = read_block(&chunk.block_id, fs)?;
    
    // 调用提取的函数从block中删除chunk
    remove_chunk_from_block(chunk, &mut block)?;
    
    // 检查chunk_list是否为空，如果为空则删除block文件
    if block.header.chunk_list.is_empty() {
        info!("remove_chunk - chunk_list is empty, deleting block {}", chunk.block_id);
        // 构建block文件路径
        let block_file_path = fs.data_path.join(&chunk.block_id);
        // 删除block文件
        if let Err(e) = crate::memfs::remove_file(&block_file_path) {
            // 如果文件不存在，我们仍然视为成功
            if e.kind() != std::io::ErrorKind::NotFound {
                error!("remove_chunk - failed to delete block file {}: {:?}", block_file_path.display(), e);
                return Err(BlockDeleteFailed { error: e.to_string() }.into());
            }
        }
    } else {
        // 重新保存block
        info!("remove_chunk - saving updated block {}", chunk.block_id);
        save_block(&block, &fs)?;
    }
    
    // 检查并更新current_block中相同block_id的block
    if fs.current_block.borrow().as_ref().is_some_and(|current| current.header.id == chunk.block_id) {
        debug!("remove_chunk - updating current_block with modified block_id {}", chunk.block_id);
        
        // 先完全取出current_block，避免嵌套borrow
        let mut maybe_current_block = fs.current_block.borrow_mut().take();
        
        if let Some(ref mut current_block) = maybe_current_block {
            // 调用提取的函数从current_block中删除chunk
            if let Err(e) = remove_chunk_from_block(chunk, current_block) {
                debug!("remove_chunk - failed to update current_block: {:?}", e);
                // 错误情况下也保留修改后的block
            }
        }
        
        // 一次性放回current_block，避免嵌套borrow
        *fs.current_block.borrow_mut() = maybe_current_block;
    }
    
    // 从缓存中删除对应的block
    let cache_key = format!("{}:{}", key_prefix::BLOCK, chunk.block_id);
    G_CHUNK_BLOCK_CACHE.del(&cache_key);
    
    info!("remove_chunk - successfully removed chunk {} from block {}", chunk.hash, chunk.block_id);
    Ok(())
}


pub fn remove_chunk_from_block(chunk: &Chunk, block: &mut Block) -> Result<bool> {
    debug!("remove_chunk_from_block - removing chunk {} from block", chunk.hash);
    
    // 找到要删除的chunk在block中的位置和偏移量
    let mut chunk_index = None;
    let mut offset = 0;
    let mut target_offset = 0;
    
    for (i, chunk_info) in block.header.chunk_list.iter().enumerate() {
        if chunk_info.hash == chunk.hash {
            chunk_index = Some(i);
            target_offset = offset;
            break;
        }
        offset += chunk_info.size as usize;
    }
    
    // 如果找不到chunk，返回错误
    if chunk_index.is_none() {
        debug!("remove_chunk_from_block - chunk {} not found", chunk.hash);
        return Err(ChunkDataNotFound {
            block_id: chunk.block_id.clone(),
            hash: chunk.hash.clone()
        }.into());
    }
    
    let chunk_index = chunk_index.unwrap();
    let chunk_size = block.header.chunk_list[chunk_index].size as usize;
    
    // 从chunk_list中删除该chunk
    block.header.chunk_list.remove(chunk_index);
    
    // 从data中删除该chunk的数据
    if target_offset + chunk_size <= block.data.len() {
        // 创建新的数据向量，不包含要删除的chunk
        let mut new_data = Vec::with_capacity(block.data.len() - chunk_size);
        // 添加删除位置之前的数据
        new_data.extend_from_slice(&block.data[0..target_offset]);
        // 添加删除位置之后的数据
        new_data.extend_from_slice(&block.data[target_offset + chunk_size..]);
        block.data = new_data;
    } else {
        return Err(ChunkDataNotFound {
            block_id: chunk.block_id.clone(),
            hash: chunk.hash.clone()
        }.into());
    }
    
    // 更新block的总大小
    block.header.total_size -= chunk.size as i64;
    block.header.updated_at = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
    
    debug!("remove_chunk_from_block - successfully removed chunk {}", chunk.hash);
    Ok(true)
}