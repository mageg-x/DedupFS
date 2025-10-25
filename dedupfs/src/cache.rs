use dashmap::DashMap;
use std::sync::{Arc, atomic::{AtomicUsize, Ordering}};
use std::time::{Instant};
use std::any::{Any, TypeId};
use tracing::{info, error};


// 缓存项特质，用于计算大小和类型转换
pub trait CacheItem: Any + Send + Sync {
    // 获取项目大小的方法（用于计算缓存容量）
    fn size(&self) -> usize;
    
    // 转换为Any类型，用于类型向下转换
    fn as_any(&self) -> &dyn Any;
    
    // 转换为Any类型的可变引用，用于可变类型向下转换
    fn as_any_mut(&mut self) -> &mut dyn Any;
}

// 注意：每个实现CacheItem的具体类型都需要实现size方法

// 缓存条目，包含值和访问时间
struct CacheEntry<V> {
    value: Arc<V>,
    accessed_at: Instant,
    size: usize, // 项目大小
}

// 泛型缓存封装，使用Arc存储值以避免借用问题
pub struct Cache<K: std::cmp::Eq + std::hash::Hash + Clone, V> {
    data: DashMap<K, CacheEntry<V>>,
    capacity: usize,         // 缓存总容量
    current_size: AtomicUsize, // 当前缓存使用的容量，使用AtomicUsize提高并发性能
}

impl<K: std::cmp::Eq + std::hash::Hash + Clone, V> Cache<K, V> {
    // 创建新的缓存实例
    pub fn new(capacity: usize) -> Self {
        Self {
            data: DashMap::new(),
            capacity,
            current_size: AtomicUsize::new(0),
        }
    }

    // 获取缓存中的项目（返回Arc<V>）
    pub fn get(&self, key: &K) -> Option<Arc<V>> {
        if let Some(mut entry) = self.data.get_mut(key) {
            // 更新访问时间
            entry.accessed_at = Instant::now();
            // 返回值的Arc克隆
            return Some(Arc::clone(&entry.value));
        }
        None
    }

    // 将项目放入缓存，需要提供大小计算函数
    pub fn put(&self, key: K, value: V, size_calculator: impl FnOnce(&V) -> usize) {
        // 计算值的大小
        let value_size = size_calculator(&value);
        
        // 检查单个项目是否超过容量
        if value_size > self.capacity {
            error!("Cache item size {} exceeds cache capacity {}", value_size, self.capacity);
            return; // 无法存储
        }
        
        // 检查是否有足够空间，如果没有，清理早期数据
        self.shrink(value_size);
        
        // 如果键已存在，减去旧值的大小
        let old_size = self.data.get(&key).map(|entry| entry.size).unwrap_or(0);
        
        // 创建值的Arc并添加到缓存
        let arc_value = Arc::new(value);
        self.data.insert(key.clone(), CacheEntry {
            value: arc_value,
            accessed_at: Instant::now(),
            size: value_size,
        });
        
        // 更新当前大小（原子操作）
        if old_size > 0 {
            self.current_size.fetch_sub(old_size, Ordering::SeqCst);
        }
        self.current_size.fetch_add(value_size, Ordering::SeqCst);
    }

    // 从缓存中删除项目
    pub fn del(&self, key: &K) {
        if let Some(entry) = self.data.remove(key) {
            let size = entry.1.size;
            self.current_size.fetch_sub(size, Ordering::SeqCst);
            info!("Cache item deleted, size reduced by {}", size);
        }
    }

    // 确保缓存有足够空间，如果不足则清理
    fn shrink(&self, required_size: usize) {
        let current_size = self.current_size.load(Ordering::SeqCst);
        let remaining_space = self.capacity.saturating_sub(current_size);
        
        // 检查剩余空间是否不足
        if remaining_space < required_size {
            info!("Cache cleanup needed: required_size={}, remaining_space={}, current_size={}, capacity={}", 
                  required_size, remaining_space, current_size, self.capacity);
            
            // 获取所有键和访问时间
            let keys_with_access_times: Vec<_> = self.data
                .iter()
                .map(|entry| (entry.key().clone(), entry.value().accessed_at))
                .collect();
            
            // 对键按访问时间排序
            let mut sorted_keys = keys_with_access_times;
            sorted_keys.sort_by(|a, b| a.1.cmp(&b.1));
            
            // 继续删除最久未使用的项目，直到有足够空间
            let mut i = 0;
            let mut items_removed = 0;
            let mut size_freed = 0;
            
            while i < sorted_keys.len() {
                let current_size = self.current_size.load(Ordering::SeqCst);
                if self.capacity.saturating_sub(current_size) >= required_size {
                    break; // 空间足够了
                }
                
                let key = &sorted_keys[i].0;
                if let Some(entry) = self.data.remove(key) {
                    let size = entry.1.size;
                    self.current_size.fetch_sub(size, Ordering::SeqCst);
                    size_freed += size;
                    items_removed += 1;
                    // error!("Cache item removed: key={:?}, size={}", key, size);
                }
                i += 1;
            }
            
            if items_removed > 0 {
                error!("Cache cleanup completed: removed {} items, freed {} bytes", items_removed, size_freed);
            }
        }
    }
    
    // 获取当前缓存大小
    pub fn size(&self) -> usize {
        self.current_size.load(Ordering::SeqCst)
    }
    
    pub fn count(&self) -> usize {
        self.data.len()
    }
}


// 为Cache实现Arc，方便在多线程环境中共享
pub type SharedCache<K, V> = Arc<Cache<K, V>>;

// 创建共享缓存的辅助函数
pub fn new_shared_cache<K: std::cmp::Eq + std::hash::Hash + Clone, V>(capacity: usize) -> SharedCache<K, V> {
    Arc::new(Cache::new(capacity))
}

// 注意：我们可以直接使用Rust标准库的downcast方法，不需要额外的特质