use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::fmt::Display;
use dashmap::DashMap;
use tracing::{error, info};
use crate::errors::{DeserializationError, FileSystemError, SerializationError};

/// 数据类型前缀常量
pub mod key_prefix {
    pub const INODE: &str = "inode";
    pub const CHUNK: &str = "chunk";
}

/// KVStore 是一个基于 RocksDB 的键值存储封装，支持泛型
pub struct KVStore {
    db: Arc<rocksdb::DB>,
    instance_name: String,
    db_path: PathBuf,
}

// 使用 DashMap 替代 Mutex<HashMap>，支持高并发读写
static KV_STORE_INSTANCES: std::sync::LazyLock<DashMap<String, Arc<KVStore>>> = 
    std::sync::LazyLock::new(|| DashMap::new());

impl KVStore {
    /// 初始化指定名称的KVStore实例
    pub fn init<S: AsRef<str> + Display>(instance_name: S, db_path: &Path) -> anyhow::Result<Arc<KVStore>> {
        let instance_name_str = instance_name.to_string();
        info!("initializing kvstore instance: '{}' at path: {:?}", instance_name_str, db_path);
        
        // 尝试获取实例，如果不存在则创建
        if let Some(instance) = KV_STORE_INSTANCES.get(&instance_name_str) {
            info!("using existing kvstore instance: '{}'", instance_name_str);
            return Ok(Arc::clone(instance.value()));
        }

        // 创建 RocksDB 选项
        let mut options = rocksdb::Options::default();
        options.create_if_missing(true);
        options.create_missing_column_families(true);
        
        if !db_path.exists() {
            info!("creating database directory for kvstore '{}': {:?}", instance_name_str, db_path);
            std::fs::create_dir_all(db_path).map_err(|e| {
                error!("failed to create directory for kvstore '{}': {}, path: {:?}", instance_name_str, e, db_path);
                e
            })?;
        }
        
        let db = rocksdb::DB::open(&options, db_path).map_err(|e| {
            error!("failed to open rocksdb for '{}': {}, path: {:?}", instance_name_str, e, db_path);
            e
        })?;
        info!("successfully opened rocksdb for kvstore '{}'", instance_name_str);
        
        let kv_store = Arc::new(KVStore {
            db: Arc::new(db),
            instance_name: instance_name_str.clone(),
            db_path: db_path.to_path_buf(),
        });
        
        // 插入实例，如果已存在则返回已存在的实例（避免重复创建）
        let entry = KV_STORE_INSTANCES.entry(instance_name_str).or_insert_with(|| Arc::clone(&kv_store));
        Ok(Arc::clone(entry.value()))
    }
    
    /// 根据实例名获取KVStore实例（无需泛型）
    pub fn get_instance<S: AsRef<str> + Display>(instance_name: S) -> anyhow::Result<Arc<KVStore>> {
        let instance_name_str = instance_name.to_string();
        info!("getting kvstore instance: '{}'", instance_name_str);
        
        match KV_STORE_INSTANCES.get(&instance_name_str) {
            Some(instance) => {
                info!("successfully retrieved kvstore instance: '{}'", instance_name_str);
                Ok(Arc::clone(instance.value()))
            },
            None => {
                error!("kvstore instance '{}' not initialized", instance_name_str);
                Err(anyhow::Error::from(FileSystemError { description: format!("KVStore instance '{}' not initialized", instance_name_str) }))
            },
        }
    }
    
    /// 获取实例名称
    pub fn get_instance_name(&self) -> &str {
        &self.instance_name
    }
    
    /// 获取数据库路径
    pub fn get_db_path(&self) -> &Path {
        &self.db_path
    }
    
    /// 关闭并移除指定实例
    pub fn close<S: AsRef<str> + Display>(instance_name: S) -> anyhow::Result<()> {
        let instance_name_str = instance_name.to_string();
        info!("closing kvstore instance: '{}'", instance_name_str);
        
        if KV_STORE_INSTANCES.remove(&instance_name_str).is_none() {
            error!("failed to close: kvstore instance '{}' not found", instance_name_str);
            return Err(anyhow::Error::from(FileSystemError { description: format!("KVStore instance '{}' not found", instance_name_str) }));
        }
        
        info!("successfully closed kvstore instance: '{}'", instance_name_str);
        Ok(())
    }
    
    /// 获取所有已初始化的实例名称
    pub fn get_all_instances() -> anyhow::Result<Vec<String>> {
        let instances: Vec<_> = KV_STORE_INSTANCES.iter().map(|entry| entry.key().clone()).collect();
        info!("retrieved {} kvstore instances", instances.len());
        Ok(instances)
    }
    
    /// 获取指定key的值（自动反序列化为指定类型）
    pub fn get<T>(&self, key: &[u8]) -> anyhow::Result<Option<T>>
    where
        T: serde::de::DeserializeOwned,
    {
        info!("kvstore '{}': getting value for key ({} bytes): '{}'", self.instance_name, key.len(), String::from_utf8_lossy(key));
        
        match self.db.get(key).map_err(|e| {
            error!("kvstore '{}': failed to get key: {}, key length: {}, key: '{}'", self.instance_name, e, key.len(), String::from_utf8_lossy(key));
            e
        })? {
            Some(bytes) => {
                info!("kvstore '{}': key found, value size: {} bytes, key: '{}'", self.instance_name, bytes.len(), String::from_utf8_lossy(key));
                let value: T = bincode::deserialize(&bytes)
                    .map_err(|e| {
                        error!("kvstore '{}': failed to deserialize value: {}, bytes size: {}, key: '{}'", self.instance_name, e, bytes.len(), String::from_utf8_lossy(key));
                        DeserializationError { error: format!("{}", e) }
                    })?;
                Ok(Some(value))
            }
            None => {
                info!("kvstore '{}': key not found: '{}'", self.instance_name, String::from_utf8_lossy(key));
                Ok(None)
            },
        }
    }
    
    /// 设置key-value对（自动序列化T为字节数组）
    pub fn set<T>(&self, key: &[u8], value: &T) -> anyhow::Result<()>
    where
        T: serde::Serialize,
    {
        info!("kvstore '{}': setting value for key ({} bytes): '{}'", self.instance_name, key.len(), String::from_utf8_lossy(key));
        
        let serialized = bincode::serialize(value)
            .map_err(|e| {
                error!("kvstore '{}': failed to serialize value: {}, key: '{}'", self.instance_name, e, String::from_utf8_lossy(key));
                SerializationError { error: format!("{}", e) }
            })?;
        
        self.db.put(key, &serialized).map_err(|e| {
            error!("kvstore '{}': failed to put key-value pair: {}, key length: {}, value size: {}, key: '{}'", self.instance_name, e, key.len(), serialized.len(), String::from_utf8_lossy(key));
            e
        })?;
        
        info!("kvstore '{}': successfully set key-value pair, value size: {} bytes, key: '{}'", self.instance_name, serialized.len(), String::from_utf8_lossy(key));
        Ok(())
    }
    
    /// 删除指定key
    pub fn del(&self, key: &[u8]) -> anyhow::Result<()>
    {
        info!("kvstore '{}': deleting key ({} bytes): '{}'", self.instance_name, key.len(), String::from_utf8_lossy(key));
        
        self.db.delete(key).map_err(|e| {
            error!("kvstore '{}': failed to delete key: {}, key length: {}, key: '{}'", self.instance_name, e, key.len(), String::from_utf8_lossy(key));
            e
        })?;
        
        info!("kvstore '{}': successfully deleted key: '{}'", self.instance_name, String::from_utf8_lossy(key));
        Ok(())
    }
    
    /// 前缀扫描，返回键列表和下一个标记（用于分页）
    pub fn scan(&self, prefix: &[u8], marker: Option<&[u8]>, limit: usize) -> anyhow::Result<(Vec<Vec<u8>>, Option<Vec<u8>>)> {
        info!("kvstore '{}': scanning with prefix ({} bytes): '{}', limit: {}, marker: {}", 
             self.instance_name, prefix.len(), String::from_utf8_lossy(prefix), limit, 
             marker.map_or("none".to_string(), |m| format!("{} bytes: '{}'", m.len(), String::from_utf8_lossy(m))));
        
        let mode = if let Some(marker) = marker {
            // 从标记位置开始扫描
            info!("kvstore '{}': scanning from marker position", self.instance_name);
            rocksdb::IteratorMode::From(marker, rocksdb::Direction::Forward)
        } else {
            // 从前缀开始扫描
            info!("kvstore '{}': scanning from prefix start", self.instance_name);
            rocksdb::IteratorMode::From(prefix, rocksdb::Direction::Forward)
        };

        let iter = self.db.iterator(mode);
        
        let mut keys = Vec::new();
        let mut next_marker = None;
        let mut count = 0;
        let iter_error: Option<anyhow::Error> = None;
        
        for result in iter {
            match result {
                Ok((key, _)) => {
                    // 检查键是否以指定前缀开头
                    if key.starts_with(prefix) {
                        if count >= limit {
                            next_marker = Some(key.to_vec());
                        info!("kvstore '{}': scan reached limit of {}, next marker available: '{}'", self.instance_name, limit, String::from_utf8_lossy(&key));
                            break;
                        }
                        keys.push(key.to_vec());
                        count += 1;
                    } else {
                        // 键不再匹配前缀，停止扫描
                        info!("kvstore '{}': scan stopped, no longer matching prefix", self.instance_name);
                        break;
                    }
                },
                Err(e) => {
                    error!("kvstore '{}': failed to iterate during scan: {}", self.instance_name, e);
                    return Err(anyhow::Error::from(FileSystemError { description: format!("failed to iterate: {}", e) }));
                },
            }
        }
        
        let next_marker = if next_marker.is_some() {
            next_marker
        } else if !keys.is_empty() {
            // 如果没有达到限制，且有结果，则下一次从最后一个键之后开始
            info!("kvstore '{}': no next marker, using last key for continuation", self.instance_name);
            Some(keys.last().unwrap().clone())
        } else {
            info!("kvstore '{}': scan returned no keys, no next marker", self.instance_name);
            None
        };
        
        info!("kvstore '{}': scan completed, found {} keys", self.instance_name, keys.len());
        Ok((keys, next_marker))
    }
    
    /// 批量操作（泛型）
    pub fn batch<'a, T>(&self, ops: impl Iterator<Item = BatchOp<'a, T>>) -> anyhow::Result<()>
    where
        T: serde::Serialize + 'a,
    {
        info!("kvstore '{}': starting batch operation", self.instance_name);
        let mut batch = rocksdb::WriteBatch::default();
        let mut set_count = 0;
        let mut del_count = 0;
        
        for op in ops {
            match op {
                BatchOp::Set(key, value) => {
                    let serialized = bincode::serialize(value)
                        .map_err(|e| {
                            error!("kvstore '{}': failed to serialize value in batch: {}, key: '{}'", self.instance_name, e, String::from_utf8_lossy(key));
                            SerializationError { error: format!("{}", e) }
                        })?;
                    batch.put(key, &serialized);
                    set_count += 1;
                },
                BatchOp::Del(key) => {
                    batch.delete(key);
                    del_count += 1;
                },
            }
        }
        
        info!("kvstore '{}': executing batch with {} set and {} delete operations", self.instance_name, set_count, del_count);
        
        self.db.write(batch).map_err(|e| {
            error!("kvstore '{}': failed to execute batch operations: {}", self.instance_name, e);
            e
        })?;
        
        info!("kvstore '{}': batch operation completed successfully", self.instance_name);
        Ok(())
    }
}

/// 批量操作类型（支持泛型）
pub enum BatchOp<'a, T> {
    Set(&'a [u8], &'a T),
    Del(&'a [u8]),
}

/// 为特定类型的数据创建带前缀的键
pub fn make_prefixed_key(prefix: &str, key: &[u8]) -> Vec<u8> {
    let mut result = format!("{}:", prefix).into_bytes();
    result.extend_from_slice(key);
    result
}

