use bincode::de;
use fuser::{FileType, FileAttr};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{SystemTime};
use serde::{Serialize, Deserialize};
use anyhow::Result;
use std::collections::{HashMap, HashSet};
use tracing::{error, info};
use dashmap::DashMap;
use std::sync::{Arc, LazyLock};
use crate::chunk::{Chunk, do_chunking, calc_hash};
use crate::config::ChunkConfig;
use libc;
use defer::defer;
use sha2::{Sha256, Digest};
use crate::vfile::DedupFS;
use crate::kvstore::{make_prefixed_key, key_prefix};

// 全局INode缓存，使用dashmap实现
static G_INODE_CACHE: LazyLock<DashMap<String, Arc<INode>>> = LazyLock::new(DashMap::new);

/// BlockChunk 表示块中的一个块条目
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct INodeChunk {
    /// 块哈希
    pub hash: String,
    /// 仅用于内存操作，不持久化
    #[serde(skip)]
    pub data: Vec<u8>,
}

// 完整的元数据结构
#[derive(Debug, Serialize, Deserialize, Clone)]
pub struct INode {
    pub ino: u64,
    pub size: u64,
    pub blocks: u64,
    pub atime: SystemTime,
    pub mtime: SystemTime,
    pub ctime: SystemTime,
    pub crtime: SystemTime,
    pub kind: FileType,
    pub perm: u16,
    pub nlink: u32,
    pub uid: u32,
    pub gid: u32,
    pub rdev: u32,
    pub blksize: u32,
    pub flags: u32,
    pub name: String,
    pub parent: u64,
    pub xattr: HashMap<String, Vec<u8>>,
    pub chunks: Vec<INodeChunk>,
    pub symlink_target: Option<String>,
}

impl INode {
    pub fn new(ino: u64, name: &str, parent: u64, kind: FileType) -> Self {
        info!("creating new inode: ino={}, name={}, parent={}, kind={:?}", ino, name, parent, kind);
        let now = SystemTime::now();
        Self {
            ino,
            name: name.to_string(),
            parent,
            kind,
            size: 0,
            blocks: 0,
            atime: now,
            mtime: now,
            ctime: now,
            crtime: now,
            perm: match kind {
                FileType::Directory => 0o755,
                FileType::RegularFile => 0o644,
                _ => 0o644,
            },
            nlink: 1,
            uid: unsafe { libc::getuid() },
            gid: unsafe { libc::getgid() },
            rdev: 0,
            blksize: 512, // 典型的块大小
            flags: 0,
            xattr: HashMap::new(),
            chunks: Vec::new(), // 现在是 INodeChunk 类型的向量
            symlink_target: None,
        }
    }

    pub fn to_file_attr(&self) -> FileAttr {
        FileAttr {
            ino: self.ino,
            size: self.size,
            blocks: self.blocks,
            atime: self.atime,
            mtime: self.mtime,
            ctime: self.ctime,
            crtime: self.crtime,
            kind: self.kind,
            perm: self.perm,
            nlink: self.nlink,
            uid: self.uid,
            gid: self.gid,
            rdev: self.rdev,
            blksize: self.blksize,
            flags: self.flags,
        }
    }

    pub fn update_size(&mut self, new_size: u64) {
        info!("updating inode {} size: {} -> {}", self.ino, self.size, new_size);
        self.size = new_size;
        self.blocks = (new_size + self.blksize as u64 - 1) / self.blksize as u64;
        self.mtime = SystemTime::now();
        self.ctime = SystemTime::now();
    }

    pub fn update_times(&mut self) {
        let now = SystemTime::now();
        self.atime = now;
        self.mtime = now;
        self.ctime = now;
    }
    
    // 设置扩展属性
    pub fn set_xattr(&mut self, name: &str, value: &[u8]) {
        info!("setting xattr for inode {}: {} (size: {})", self.ino, name, value.len());
        self.xattr.insert(name.to_string(), value.to_vec());
        self.update_times();
    }
    
    // 获取扩展属性
    pub fn get_xattr(&self, name: &str) -> Option<Vec<u8>> {
        self.xattr.get(name).cloned()
    }
    
    // 列出所有扩展属性名称
    pub fn list_xattr(&self) -> Vec<String> {
        self.xattr.keys().cloned().collect()
    }
    
    // 删除扩展属性
    pub fn remove_xattr(&mut self, name: &str) -> bool {
        if self.xattr.remove(name).is_some() {
            info!("removed xattr for inode {}: {}", self.ino, name);
            self.update_times();
            true
        } else {
            info!("failed to remove non-existent xattr for inode {}: {}", self.ino, name);
            false
        }
    }
    
    // 从chunks中读取数据，支持偏移量
    pub fn read(&self, offset: u64, size: usize, fs: &DedupFS) -> Vec<u8> {
        info!("reading data from inode {}: offset={}, size={}", self.ino, offset, size);
        // 修复边界条件检查
        if offset >= self.size || size == 0 || self.chunks.is_empty() {
            info!("reading no data from inode {}: offset={}, size={}, file_size={}", 
                 self.ino, offset, size, self.size);
            return Vec::new();
        }
        
        // 计算实际可读取的最大字节数
        let max_bytes_to_read = std::cmp::min(size, (self.size - offset) as usize);
        
        if max_bytes_to_read == 0 {
            return Vec::new();
        }
        
        let mut result = Vec::with_capacity(max_bytes_to_read);
        let mut current_offset = 0u64;
        
        // 遍历所有的chunks，查找与请求范围重叠的部分
        info!("reading from {} chunks in inode {}", self.chunks.len(), self.ino);
        for inode_chunk in &self.chunks {
            let chunk_hash = &inode_chunk.hash;
            
            // 优先使用inode_chunk.data.len计算偏移
            let chunk_size = if !inode_chunk.data.is_empty() {
                inode_chunk.data.len()
            } else {
                // 如果data为空，再使用get_chunk_meta获取chunk元数据
                match crate::chunk::get_chunk_meta(chunk_hash, fs) {
                    Ok(chunk_meta) => chunk_meta.size.try_into().unwrap_or(0),
                    Err(e) => {
                        error!("failed to get chunk metadata for hash {}: {}", chunk_hash, e);
                        continue;
                    }
                }
            };
            
            let chunk_start = current_offset;
            let chunk_end = current_offset + chunk_size as u64;
                    
            // 检查chunk是否与请求的范围重叠
            let read_start = offset;
            let read_end = offset + max_bytes_to_read as u64;
                    
            if chunk_end > read_start && chunk_start < read_end {
                // 计算在这个chunk中要读取的范围
                let start_in_chunk = if chunk_start < read_start { 
                    (read_start - chunk_start) as usize 
                } else { 
                    0 
                };
                
                let end_in_chunk = std::cmp::min(
                    (read_end - chunk_start) as usize,
                    chunk_size
                );
                
                if start_in_chunk < end_in_chunk {
                    // 优先使用inode_chunk中已有的数据
                    let chunk = if !inode_chunk.data.is_empty() {
                        // 直接使用inode_chunk.data，不需要调用get_chunk_data
                        Chunk {
                            hash: chunk_hash.clone(),
                            size: inode_chunk.data.len() as i32,
                            ref_count: 0,
                            block_id: "".to_string(),
                            data: inode_chunk.data.clone()
                        }
                    } else {
                        // 如果data为空，尝试从KVStore获取数据
                        match crate::chunk::get_chunk_data(chunk_hash, fs) {
                            Ok(full_chunk) => full_chunk,
                            Err(e) => {
                                error!("failed to get chunk data for hash {}: {}", chunk_hash, e);
                                continue;
                            }
                        }
                    };
                    
                    // 获取数据切片
                    let data_slice = &chunk.data[start_in_chunk..end_in_chunk];
                    result.extend_from_slice(data_slice);
                    
                    // 如果已经读取足够的数据，提前退出
                    if result.len() >= max_bytes_to_read {
                        break;
                    }
                }
            }
            
            current_offset = chunk_end;
            // 如果已经超过读取范围，提前退出
            if current_offset >= read_end {
                break;
            }
        }
        info!("completed reading data from inode {}, actual bytes read: {}", self.ino, result.len());
        result
    }
    
    // 保留原有方法以保持兼容性
    pub fn write(&mut self, offset: u64, data: &[u8], fs: &DedupFS) -> Result<()> {
        info!("writing data to inode {}: offset={}, size={}", self.ino, offset, data.len());
        if data.is_empty() {
            return Ok(());
        }
      
        let write_end = offset + data.len() as u64;
        
        // 情况1: 在文件末尾或超出文件末尾的写入（包括append和空洞写入）
        if offset >= self.size {
            // 如果有最后一个chunk，尝试扩展它
            if let Some(mut last_inode_chunk) = self.chunks.pop() {
                // 优先使用last_inode_chunk.data
                // 如果data为空，先从KVStore获取数据
                if last_inode_chunk.data.is_empty() {
                    match crate::chunk::get_chunk_data(&last_inode_chunk.hash, fs) {
                        Ok(chunk) => {
                            last_inode_chunk.data = chunk.data;
                        },
                        Err(e) => {
                            error!("failed to get last chunk data: {}", e);
                            // 如果获取失败，保持data为空
                        }
                    }
                }
                
                // 检查是否需要填充空洞
                if offset > self.size {
                    let hole_size = offset - self.size;
                    last_inode_chunk.data.extend(vec![0u8; hole_size as usize]);
                }
                
                // 直接追加到最后一个chunk的data中
                last_inode_chunk.data.extend_from_slice(data);
                
                // 检查大小是否超过avg_size，如果超过则需要分块
                if last_inode_chunk.data.len() > fs.chunk_conf.avg_size {
                    // 只有当数据大小超过阈值时才调用do_chunking
                    let new_chunks = do_chunking(&last_inode_chunk.data, fs).map_err(|e| {
                        error!("failed to rechunk data for inode {}: {}", self.ino, e);
                        e
                    })?;
                    info!("rechunked data into {} chunks for inode {}", new_chunks.len(), self.ino);
                    // 将do_chunking产生的chunk转为INodeChunk存储
                    self.chunks.extend(new_chunks.iter().map(|c| INodeChunk {
                        hash: c.hash.clone(),
                        data: c.data.clone() // 保留数据在内存中
                    }));
                } else {
                    // 如果没有超过avg_size，直接将修改后的chunk放回chunks中
                    // 需要计算hash，直接使用calc_hash函数避免内存拷贝
                    last_inode_chunk.hash = calc_hash(&last_inode_chunk.data);
                    self.chunks.push(last_inode_chunk);
                }
            } else {
                // 如果没有现有chunk，创建新的INodeChunk
                let mut new_inode_chunk = INodeChunk {
                    hash: String::new(),
                    data: Vec::new()
                };
                
                // 如果需要填充空洞
                if offset > self.size {
                    let hole_size = offset - self.size;
                    new_inode_chunk.data.extend(vec![0u8; hole_size as usize]);
                }
                
                // 添加实际数据
                new_inode_chunk.data.extend_from_slice(data);
                
                // 检查大小是否超过avg_size
                if new_inode_chunk.data.len() > fs.chunk_conf.avg_size {
                    // 需要分块
                    let new_chunks = do_chunking(&new_inode_chunk.data, fs).map_err(|e| {
                        error!("failed to chunk new data for inode {}: {}", self.ino, e);
                        e
                    })?;
                    info!("created {} new chunks for inode {}", new_chunks.len(), self.ino);
                    // 转为INodeChunk存储
                    self.chunks.extend(new_chunks.iter().map(|c| INodeChunk {
                        hash: c.hash.clone(),
                        data: c.data.clone() // 保留数据在内存中
                    }));
                } else {
                    // 计算hash并直接添加
                    // 计算hash，避免内存拷贝
                    new_inode_chunk.hash = calc_hash(&new_inode_chunk.data);
                    self.chunks.push(new_inode_chunk);
                }
            }
            
            // 更新文件大小
            self.update_size(write_end);
            return Ok(());
        }
        
        // 情况2: 追加写入 - 写入范围超出当前文件大小（部分覆盖部分追加）
        if write_end > self.size {
            // 找到受影响的chunk范围（从offset开始到文件末尾）
            let mut chunk_start = 0u64;
            let mut affected_start_index = None;
            let mut affected_end_index = None;
            let mut affected_start_offset = 0u64; // 受影响区域的起始文件偏移
            
            for (i, inode_chunk) in self.chunks.iter().enumerate() {
                // 优先使用inode_chunk.data的长度
                let chunk_size = if !inode_chunk.data.is_empty() {
                    inode_chunk.data.len() as u64
                } else {
                    // 否则从meta获取
                    let chunk_hash = &inode_chunk.hash;
                    match crate::chunk::get_chunk_meta(chunk_hash, fs) {
                        Ok(meta) => meta.size as u64,
                        Err(e) => {
                            error!("failed to get chunk meta for hash {}: {}", chunk_hash, e);
                            continue;
                        }
                    }
                };
                
                let chunk_end = chunk_start + chunk_size;
                
                if chunk_start <= offset && chunk_end >= offset && affected_start_index.is_none() {
                    affected_start_index = Some(i);
                    affected_start_offset = chunk_start;
                }
                
                if chunk_start <= self.size && chunk_end >= self.size {
                    affected_end_index = Some(i);
                }
                
                if affected_start_index.is_some() && affected_end_index.is_some() {
                    break;
                }
                
                chunk_start = chunk_end;
            }
            
            // 提取并合并受影响的chunks
            if let (Some(start_idx), Some(end_idx)) = (affected_start_index, affected_end_index) {
                let affected_range = start_idx..=end_idx;
                let mut merged_data = Vec::new();
                
                // 只合并受影响的chunks，优先使用inode_chunk.data
                for i in affected_range.clone() {
                    let inode_chunk = &self.chunks[i];
                    if !inode_chunk.data.is_empty() {
                        merged_data.extend_from_slice(&inode_chunk.data);
                    } else {
                        let chunk_hash = &inode_chunk.hash;
                        match crate::chunk::get_chunk_data(chunk_hash, fs) {
                            Ok(chunk) => {
                                merged_data.extend_from_slice(&chunk.data);
                            },
                            Err(e) => {
                                error!("failed to get chunk data for hash {}: {}", chunk_hash, e);
                                // 使用空数据继续
                                continue;
                            }
                        }
                    }
                }
                
                // 在合并的数据中执行写入操作
                let data_offset_in_merged = (offset - affected_start_offset) as usize;
                let overlap_size = self.size - offset;
                
                // 覆盖部分数据
                if overlap_size > 0 {
                    let overlap_end = data_offset_in_merged + overlap_size as usize;
                    if overlap_end <= merged_data.len() {
                        merged_data[data_offset_in_merged..overlap_end].copy_from_slice(&data[..overlap_size as usize]);
                    }
                }
                
                // 追加剩余数据
                if write_end > self.size {
                    let append_data = &data[overlap_size as usize..];
                    merged_data.extend_from_slice(append_data);
                }
                
                // 移除受影响的chunks
                self.chunks.drain(affected_range);
                
                // 检查合并后的数据大小是否超过avg_size
                if merged_data.len() > fs.chunk_conf.avg_size {
                    // 需要重新分块
                    let new_chunks = do_chunking(&merged_data, fs).map_err(|e| {
                        error!("failed to rechunk affected data for inode {}: {}", self.ino, e);
                        e
                    })?;
                    info!("rechunked affected data into {} chunks for inode {}", new_chunks.len(), self.ino);
                    
                    // 插入新的INodeChunk，保留data在内存中
                    let new_inode_chunks: Vec<INodeChunk> = new_chunks.iter().map(|c| INodeChunk {
                        hash: c.hash.clone(),
                        data: c.data.clone() // 保留数据在内存中
                    }).collect();
                    self.chunks.splice(start_idx..start_idx, new_inode_chunks);
                } else {
                    // 如果不超过avg_size，创建单个INodeChunk
                    let mut new_inode_chunk = INodeChunk {
                        hash: String::new(),
                        data: merged_data
                    };
                    // 计算hash，避免内存拷贝
                    new_inode_chunk.hash = calc_hash(&new_inode_chunk.data);
                    self.chunks.insert(start_idx, new_inode_chunk);
                }
            }
            
            // 更新文件大小
            self.update_size(write_end);
            return Ok(());
        }
        
        // 情况3: 纯覆盖写入 - 写入范围完全在文件大小内
        // 找到受影响的chunk范围
        let mut chunk_start = 0u64;
        let mut affected_start_index = None;
        let mut affected_end_index = None;
        let mut affected_start_offset = 0u64;
        
        for (i, inode_chunk) in self.chunks.iter().enumerate() {
            // 优先使用inode_chunk.data的长度
            let chunk_size = if !inode_chunk.data.is_empty() {
                inode_chunk.data.len() as u64
            } else {
                // 否则从meta获取
                let chunk_hash = &inode_chunk.hash;
                match crate::chunk::get_chunk_meta(chunk_hash, fs) {
                    Ok(meta) => meta.size as u64,
                    Err(e) => {
                        error!("failed to get chunk meta for hash {}: {}", chunk_hash, e);
                        continue;
                    }
                }
            };
            
            let chunk_end = chunk_start + chunk_size;
            
            if chunk_start <= offset && chunk_end >= offset && affected_start_index.is_none() {
                affected_start_index = Some(i);
                affected_start_offset = chunk_start;
            }
            
            if chunk_start <= write_end && chunk_end >= write_end && affected_end_index.is_none() {
                affected_end_index = Some(i);
            }
            
            if affected_start_index.is_some() && affected_end_index.is_some() {
                break;
            }
            
            chunk_start = chunk_end;
        }
        
        // 提取并合并受影响的chunks
        if let (Some(start_idx), Some(end_idx)) = (affected_start_index, affected_end_index) {
            let affected_range = start_idx..=end_idx;
            let mut merged_data = Vec::new();
            
            // 只合并受影响的chunks，优先使用inode_chunk.data
            for i in affected_range.clone() {
                let inode_chunk = &self.chunks[i];
                if !inode_chunk.data.is_empty() {
                    merged_data.extend_from_slice(&inode_chunk.data);
                } else {
                    let chunk_hash = &inode_chunk.hash;
                    match crate::chunk::get_chunk_data(chunk_hash, fs) {
                        Ok(chunk) => {
                            merged_data.extend_from_slice(&chunk.data);
                        },
                        Err(e) => {
                            error!("failed to get chunk data for hash {}: {}", chunk_hash, e);
                            // 使用空数据继续
                            continue;
                        }
                    }
                }
            }
            
            // 在合并的数据中执行写入操作
            let data_offset_in_merged = (offset - affected_start_offset) as usize;
            let write_end_in_merged = data_offset_in_merged + data.len();
            
            if write_end_in_merged <= merged_data.len() {
                merged_data[data_offset_in_merged..write_end_in_merged].copy_from_slice(data);
            }
            
            // 移除受影响的chunks
            self.chunks.drain(affected_range);
            
            // 检查合并后的数据大小是否超过avg_size
            if merged_data.len() > fs.chunk_conf.avg_size {
                // 需要重新分块
                let new_chunks = do_chunking(&merged_data, fs).map_err(|e| {
                    error!("failed to rechunk overlay data for inode {}: {}", self.ino, e);
                    e
                })?;
                info!("rechunked overlay data into {} chunks for inode {}", new_chunks.len(), self.ino);
                
                // 插入新的INodeChunk，保留data在内存中
                let new_inode_chunks: Vec<INodeChunk> = new_chunks.iter().map(|c| INodeChunk {
                    hash: c.hash.clone(),
                    data: c.data.clone() // 保留数据在内存中
                }).collect();
                self.chunks.splice(start_idx..start_idx, new_inode_chunks);
            } else {
                // 如果不超过avg_size，创建单个INodeChunk
                let mut new_inode_chunk = INodeChunk {
                    hash: String::new(),
                    data: merged_data
                };
                
                // 计算hash，避免内存拷贝
                new_inode_chunk.hash = calc_hash(&new_inode_chunk.data);
                self.chunks.insert(start_idx, new_inode_chunk);
            }
        }
        
        // 更新修改时间
        self.mtime = SystemTime::now();
        self.ctime = SystemTime::now();
        
        info!("write completed for inode {}, file size: {}, chunks: {}", self.ino, self.size, self.chunks.len());
        Ok(())
    }

    pub fn truncate(&mut self, new_size: u64, fs: &DedupFS) -> Result<()> {
        info!("truncating inode {} to size {} bytes (current size: {} bytes)", self.ino, new_size, self.size);
        
        // 如果新大小与当前大小相同，无需操作
        if new_size == self.size {
            info!("truncate skipped for inode {}: size unchanged", self.ino);
            return Ok(());
        }
        
        // 情况1: 截断文件（新大小小于当前大小）
        if new_size < self.size {
            let mut current_offset = 0u64;
            let mut chunks_to_keep = Vec::new();
            
            // 遍历所有chunks，确定需要保留的数据
            for inode_chunk in &self.chunks {
                let chunk_hash = &inode_chunk.hash;
                // 获取chunk元数据
                let chunk_meta = match crate::chunk::get_chunk_meta(chunk_hash, fs) {
                    Ok(meta) => meta,
                    Err(e) => {
                        error!("failed to get chunk meta for hash {}: {}", chunk_hash, e);
                        // 假设chunk大小为0，继续处理
                        continue;
                    }
                };
                
                let chunk_end = current_offset + chunk_meta.size as u64;
                
                // 如果整个chunk在截断范围内，直接添加
                if chunk_end <= new_size {
                    chunks_to_keep.push(inode_chunk.clone());
                    current_offset = chunk_end;
                } else if current_offset < new_size {
                    // 如果chunk部分在截断范围内，只保留部分数据
                    let bytes_to_keep = (new_size - current_offset) as usize;
                    
                    // 获取完整的chunk数据
                    let full_chunk = match crate::chunk::get_chunk_data(chunk_hash, fs) {
                        Ok(chunk) => chunk,
                        Err(e) => {
                            error!("failed to get chunk data for hash {}: {}", chunk_hash, e);
                            // 跳过这个chunk
                            continue;
                        }
                    };
                    
                    // 创建一个新的截断后的chunk
                    let truncated_data = full_chunk.data[0..bytes_to_keep].to_vec();
                    let new_chunk = Chunk::new(truncated_data);
                    
                    // 存储新的INodeChunk
                    chunks_to_keep.push(INodeChunk {
                        hash: new_chunk.hash.clone(),
                        data: Vec::new()
                    });
                    current_offset = new_size;
                    break;
                } else {
                    // chunk完全在截断范围外，跳过
                    break;
                }
            }
            
            // 更新chunks列表
            self.chunks = chunks_to_keep;
        }

        // 情况2: 扩展文件（新大小大于当前大小）
        else if new_size > self.size {
            // 计算需要填充的0的数量
            let zero_bytes_needed = new_size - self.size;
            
            // 尝试在最后一个chunk基础上扩展
            if let Some(last_chunk) = self.chunks.last_mut() {
                // 如果最后一个chunk有数据，在其基础上扩展
                if !last_chunk.data.is_empty() {
                    // 扩展现有数据
                    last_chunk.data.extend(vec![0u8; zero_bytes_needed as usize]);
                    // 重新计算hash，避免内存拷贝
                    last_chunk.hash = calc_hash(&last_chunk.data);
                } else {
                    // 最后一个chunk没有数据，创建新的全0数据chunk
                    let zero_data = vec![0u8; zero_bytes_needed as usize];
                    let new_chunk = Chunk::new(zero_data);
                    // 存储为INodeChunk
                    self.chunks.push(INodeChunk {
                        hash: new_chunk.hash.clone(),
                        data: Vec::new()
                    });
                }
            } else {
                // 如果没有任何chunk，创建新的全0数据chunk
                let zero_data = vec![0u8; zero_bytes_needed as usize];
                let new_chunk = Chunk::new(zero_data);
                // 存储为INodeChunk
                self.chunks.push(INodeChunk {
                    hash: new_chunk.hash.clone(),
                    data: Vec::new()
                });
            }
        }
        
        // 更新文件大小和时间戳
        self.update_size(new_size);
        self.mtime = SystemTime::now();
        self.ctime = SystemTime::now();
        
        info!("truncate completed for inode {}, new size: {}, chunks count: {}", self.ino, self.size, self.chunks.len());
        Ok(())
    }
}

pub fn get_inode(ino: u64, fs: &DedupFS) -> Result<Option<INode>> { 
    // 生成缓存key
    let cache_key = format!("{}:{}", fs.id, ino);
    
    // 先从缓存中获取
    if let Some(inode_arc) = (*G_INODE_CACHE).get(&cache_key) {
        info!("inode {} found in cache for filesystem {}", ino, fs.id);
        return Ok(Some(inode_arc.as_ref().clone()));
    }
    
    // 缓存未命中，从KVStore获取
    info!("inode {} not found in cache, fetching from kvstore", ino);
    let key = make_prefixed_key(key_prefix::INODE, ino.to_string().as_bytes());
    match fs.kv_store.get::<INode>(&key)? {
        Some(inode) => {
            // 将获取到的inode放入缓存
            (*G_INODE_CACHE).insert(cache_key, Arc::new(inode.clone()));
            Ok(Some(inode))
        },
        None => {
            error!("inode {} not found in kvstore", ino);
            Ok(None)
        }
    }
}

pub fn save_inode(inode: &INode, fs: &DedupFS) -> Result<()> { 
    info!("saving inode {} for filesystem {}", inode.ino, fs.id);
    
    // 先从kv_store 获取已有的 inode ， 放到 old_inode 中
    let key = make_prefixed_key(key_prefix::INODE, inode.ino.to_string().as_bytes());
    let old_inode = match fs.kv_store.get::<INode>(&key) {
        Ok(result) => {
            info!("inode {} - retrieved from kv_store: {}", inode.ino, result.is_some());
            result
        },
        Err(e) => {
            error!("inode {} - failed to get from kv_store: {:?}, proceeding without old inode", inode.ino, e);
            None
        }
    };
    
    // 对比 old_inode 和 inode 的chunks，记录 old_inode 中有 inode 中没有的chunk
    let mut chunks_to_remove = Vec::new();
    if let Some(old) = &old_inode {
        // 创建新inode的chunks哈希集合，用于快速查找
        let new_chunks_hashes: std::collections::HashSet<_> = 
            inode.chunks.iter().map(|c| c.hash.clone()).collect();
        
        // 找出old_inode中有但new_inode中没有的chunks
        for old_chunk in &old.chunks {
            if !new_chunks_hashes.contains(&old_chunk.hash) {
                chunks_to_remove.push(old_chunk.hash.clone());
            }
        }
        info!("inode {} has {} chunks to remove", inode.ino, chunks_to_remove.len());
    }
    
    // 将 inode 的 chunks 中数据变动部分，即有data的chunk，转为Chunk数据结构
    let mut chunks_to_save = Vec::new();
    for inode_chunk in &inode.chunks {
        if !inode_chunk.data.is_empty() {
            // 这个chunk有数据，需要保存
            let chunk = Chunk {
                hash: inode_chunk.hash.clone(),
                size: inode_chunk.data.len() as i32,
                ref_count: 0, // 这里会在put_chunks中正确设置
                block_id: String::new(), // 这里会在put_chunks中正确设置
                data: inode_chunk.data.clone(),
            };
            chunks_to_save.push(chunk);
        }
    }
    
    // 保存有数据的chunks
    if !chunks_to_save.is_empty() {
        info!("saving {} data chunks for inode {}", chunks_to_save.len(), inode.ino);
        crate::block::put_chunks(chunks_to_save, fs)?;
    }
    
    // 保存 inode 元数据到 kv_store
    fs.kv_store.set(&key, inode)?;
    info!("inode {} metadata saved to kv_store", inode.ino);
    
    // 删除 old_inode 中有 inode 中没有的chunk
    for chunk_hash in chunks_to_remove {
        info!("removing unused chunk {} from inode {}", chunk_hash, inode.ino);
        crate::chunk::remove_chunk(&chunk_hash, fs)?;
    }
    
    // 删除缓存
    let cache_key = format!("{}:{}", fs.id, inode.ino);
    G_INODE_CACHE.remove(&cache_key);
    info!("inode {} removed from cache", inode.ino);
    
    Ok(())
}

pub fn remove_inode(ino: u64, fs: &DedupFS) -> Result<()> { 
    info!("removing inode {} from filesystem {}", ino, fs.id);
    // 先删除缓存
    let cache_key = format!("{}:{}", fs.id, ino);
    G_INODE_CACHE.remove(&cache_key);
    info!("inode {} removed from cache", ino);

    // 从kv_store 获取inode 元数据
    let key = make_prefixed_key(key_prefix::INODE, &ino.to_string().as_bytes());
    let inode = match fs.kv_store.get::<INode>(&key)? {
        Some(inode) => inode,
        None => {
            info!("inode {} not found in kv_store", ino);
            return Ok(()); // 如果inode不存在，视为成功
        }
    };
    info!("inode {} metadata loaded from kv_store, contains {} chunks", ino, inode.chunks.len());

    // 删除inode 的 chunks
    for chunk in &inode.chunks {
        info!("removing chunk {} from inode {}", chunk.hash, ino);
        crate::chunk::remove_chunk(&chunk.hash, fs)?;
    }
    info!("all chunks removed from inode {}", ino);

    // 从kv_store 中删除 inode 元数据
    fs.kv_store.del(&key)?;
    info!("inode {} metadata deleted from kv_store", ino);

    Ok(())
}