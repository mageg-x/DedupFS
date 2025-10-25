use dashmap::DashMap;
  use std::path::{Path, PathBuf};
  use std::sync::Arc;
  use std::time::Duration;
  use std::fs;
  use std::sync::mpsc;
  use tracing::{info, error};
  use std::sync::Mutex;
  use once_cell::sync::OnceCell;

  // 定义处理器结构
  #[derive(Debug, Clone)]
  pub struct Processor {
      pub func: fn(&[u8], &std::collections::HashMap<String, String>) -> Result<Vec<u8>, Box<dyn std::error::Error>>,
      pub params: std::collections::HashMap<String, String>,
  }

static INSTANCE: OnceCell<MemFs> = OnceCell::new();

/// 带版本的文件数据
#[derive(Debug, Clone)]
struct FileData {
    data: Vec<u8>,
    version: u64,
    processor: Option<Processor>,
}

/// 获取内存文件系统实例（自动初始化）
fn get_instance() -> &'static MemFs {
    INSTANCE.get_or_init(|| {
        MemFs::new().expect("Failed to initialize MemFs")
    })
}

/// 扩展了 std::fs::write 签名，添加了处理器参数
pub fn write<P: AsRef<Path>, C: AsRef<[u8]>>(
    path: P, 
    contents: C,
    processor: Option<Processor>
) -> std::io::Result<()> {
    info!("memfs: public write called for {:?}", path.as_ref());
    get_instance().write(path, contents, processor)
}

/// 扩展了 std::fs::read 签名，添加了反处理器参数
pub fn read<P: AsRef<Path>>(
    path: P, 
    unprocessor: Option<Processor>
) -> std::io::Result<Vec<u8>> {
    info!("memfs: public read called for {:?}", path.as_ref());
    get_instance().read(path, unprocessor)
}

/// 与 std::fs::remove_file 签名完全一致
pub fn remove_file<P: AsRef<Path>>(path: P) -> std::io::Result<()> {
    info!("memfs: public remove_file called for {:?}", path.as_ref());
    get_instance().remove_file(path)
}

/// 强制刷新所有数据到磁盘
pub fn flush() -> std::io::Result<()> {
    info!("memfs: public flush called");
    get_instance().flush_all()
}

/// 显式关闭内存文件系统
pub fn shutdown() -> std::io::Result<()> {
    info!("memfs: public shutdown called");
    if let Some(instance) = INSTANCE.get() {
        instance.shutdown()?;
    }
    Ok(())
}

// 内部实现
struct MemFs {
    cache: Arc<DashMap<PathBuf, FileData>>,
    flush_sender: mpsc::Sender<FlushCommand>,
}

enum FlushCommand {
    Paths(Vec<PathBuf>),
    Shutdown,
}

impl MemFs {
    fn new() -> Result<Self, Box<dyn std::error::Error>> {
        let cache = Arc::new(DashMap::new());
        
        // 创建刷新命令通道
        let (sender, receiver) = mpsc::channel();
        
        // 启动后台刷盘任务
        let cache_clone = cache.clone();
        std::thread::Builder::new()
            .name("memfs_flush_thread".to_string())
            .spawn(move || {
                info!("memfs: starting background flush task");
                Self::flush_task(cache_clone, receiver);
                info!("memfs: background flush task terminated");
            })?;

        info!("memfs: initialized successfully");
        Ok(Self {
            cache,
            flush_sender: sender,
        })
    }
    
    // 注意：set_file_processors 方法已移除，处理器应在 write 调用时传入

    fn write<P: AsRef<Path>, C: AsRef<[u8]>>(&self, path: P, contents: C, processor: Option<Processor>) -> std::io::Result<()> {
        let full_path = path.as_ref().to_path_buf();
        let mut data_len = contents.as_ref().len();
        
        // 克隆processor和full_path以避免移动问题
        let processor_clone = processor.clone();
        let full_path_clone = full_path.clone();
        self.cache.entry(full_path.clone()).and_modify(|file_data| {
            file_data.data = contents.as_ref().to_vec();
            file_data.version += 1;
            file_data.processor = processor.clone();
            info!("memfs: updated file {:?}, size: {}, version: {}", full_path, data_len, file_data.version);
        }).or_insert_with(move || {
            data_len = contents.as_ref().len();
            info!("memfs: created new file {:?}, size: {}, version: 1", full_path_clone, data_len);
            FileData {
                data: contents.as_ref().to_vec(),
                version: 1,
                processor: processor_clone,
            }
        });
        
        // 发送刷新命令
        self.flush_sender.send(FlushCommand::Paths(vec![full_path])).map_err(|e| std::io::Error::new(std::io::ErrorKind::Other, format!("failed to send flush command: {:?}", e)))?;
        
        Ok(())
    }

    fn read<P: AsRef<Path>>(&self, path: P, unprocessor: Option<Processor>) -> std::io::Result<Vec<u8>> {
        let full_path = path.as_ref().to_path_buf();

        // 先从内存读
        if let Some(file_data) = self.cache.get(&full_path) {
            info!("memfs: read from cache {:?}, size: {}, version: {}", full_path, file_data.data.len(), file_data.version);
            
            // 使用传入的unprocessor进行处理
            let data = file_data.data.clone();
            if let Some(unprocessor) = unprocessor {
                match (unprocessor.func)(&data, &unprocessor.params) {
                    Ok(processed_data) => return Ok(processed_data),
                    Err(e) => {
                        error!("memfs: unprocess_data failed for {:?}: {:?}", full_path, e);
                        return Ok(data); // 失败时返回原始数据
                    }
                }
            }
            return Ok(data);
        }

        // 内存没有，从磁盘读
        if full_path.exists() {
            info!("memfs: reading from disk {:?}", full_path);
            match std::fs::read(&full_path) {
                Ok(data) => {
                    info!("memfs: read from disk successful, size: {}", data.len());
                    // 使用传入的unprocessor进行处理
                    if let Some(unprocessor) = unprocessor {
                        match (unprocessor.func)(&data, &unprocessor.params) {
                            Ok(processed_data) => return Ok(processed_data),
                            Err(e) => {
                                error!("memfs: unprocess_data failed for disk file {:?}: {:?}", full_path, e);
                                return Ok(data); // 失败时返回原始数据
                            }
                        }
                    }
                    Ok(data)
                },
                Err(e) => {
                    error!("memfs: failed to read from disk {:?}: {:?}", full_path, e);
                    Err(e)
                }
            }
        } else {
            error!("memfs: file not found {:?}", full_path);
            Err(std::io::Error::new(
                std::io::ErrorKind::NotFound,
                "file not found in memory or disk",
            ))
        }
    }

    fn remove_file<P: AsRef<Path>>(&self, path: P) -> std::io::Result<()> {
        let full_path = path.as_ref().to_path_buf();
        
        // 从内存缓存中移除
        let removed = self.cache.remove(&full_path);
        if removed.is_some() {
            info!("memfs: removed file from cache {:?}", full_path);
        } else {
            info!("memfs: file not found in cache {:?}", full_path);
        }
        
        // 从磁盘删除（如果存在）
        if full_path.exists() {
            info!("memfs: removing file from disk {:?}", full_path);
            match fs::remove_file(&full_path) {
                Ok(_) => {
                    info!("memfs: removed file from disk successfully");
                },
                Err(e) => {
                    error!("memfs: failed to remove file from disk {:?}: {:?}", full_path, e);
                    return Err(e);
                }
            }
        }
        
        Ok(())
    }

    fn flush_all(&self) -> std::io::Result<()> {
        // 收集所有需要刷新的文件路径
        let paths: Vec<PathBuf> = self.cache
            .iter()
            .map(|entry| entry.key().clone())
            .collect();

        if paths.is_empty() {
            info!("memfs: flush_all - no files to flush");
            return Ok(());
        }

        info!("memfs: flush_all - sending flush command for {} files", paths.len());
        // 发送刷新命令给后台任务
        self.flush_sender.send(FlushCommand::Paths(paths))
            .map_err(|e| {
                error!("memfs: flush_all - failed to send flush command: {:?}", e);
                std::io::Error::new(
                    std::io::ErrorKind::Other, 
                    format!("failed to send flush command: {}", e)
                )
            })?;

        Ok(())
    }

    fn shutdown(&self) -> std::io::Result<()> {
        info!("memfs: shutdown - starting");
        
        // 先刷新所有数据
        self.flush_all()?;
        
        info!("memfs: shutdown - sending shutdown command");
        // 发送关闭信号
        self.flush_sender.send(FlushCommand::Shutdown)
            .map_err(|e| {
                error!("memfs: shutdown - failed to send shutdown command: {:?}", e);
                std::io::Error::new(
                    std::io::ErrorKind::Other,
                    format!("failed to send shutdown command: {}", e)
                )
            })?;
            
        info!("memfs: shutdown - completed");
        Ok(())
    }

    fn flush_task(cache: Arc<DashMap<PathBuf, FileData>>, receiver: mpsc::Receiver<FlushCommand>) {
        info!("memfs: flush_task - started");
        
        loop {
            // 等待命令或超时
            let command = match receiver.recv_timeout(Duration::from_secs(1)) {
                Ok(cmd) => cmd,
                Err(mpsc::RecvTimeoutError::Timeout) => {
                    // 超时，刷新所有缓存
                    let paths: Vec<PathBuf> = cache
                        .iter()
                        .map(|entry| entry.key().clone())
                        .collect();
                    
                    if paths.is_empty() {
                        continue;
                    }
                    info!("memfs: flush_task - timeout, flushing {} cached files", paths.len());
                    FlushCommand::Paths(paths)
                }
                Err(mpsc::RecvTimeoutError::Disconnected) => {
                    error!("memfs: flush_task - channel disconnected");
                    // 通道断开，退出线程
                    break;
                }
            };

            match command {
                FlushCommand::Paths(paths) => {
                    info!("memfs: flush_task - processing flush for {} files", paths.len());
                    Self::flush_paths(&cache, paths.clone());
                    info!("memfs: flush_task - flush completed for {} files", paths.len());
                }
                FlushCommand::Shutdown => {
                    info!("memfs: flush_task - shutdown command received");
                    // 刷新所有剩余数据
                    let all_paths: Vec<PathBuf> = cache
                        .iter()
                        .map(|entry| entry.key().clone())
                        .collect();
                    
                    if !all_paths.is_empty() {
                        info!("memfs: flush_task - flushing {} files before shutdown", all_paths.len());
                        Self::flush_paths(&cache, all_paths);
                    }
                    info!("memfs: flush_task - shutdown complete");
                    break;
                }
            }
        }
    }

    fn flush_paths(cache: &DashMap<PathBuf, FileData>, paths: Vec<PathBuf>) {
        let mut success_count = 0;
        let mut failure_count = 0;
        
        for path in paths {
            // 克隆一份数据用于写入
            let (data_to_write, version_to_write, processor) = {
                if let Some(file_data) = cache.get(&path) {
                    (file_data.data.clone(), file_data.version, file_data.processor.clone())
                } else {
                    info!("memfs: flush_paths - file already removed from cache {:?}", path);
                    continue; // 数据已被其他线程移除
                }
            };

            info!("memfs: flush_paths - writing file {:?}, size: {}, version: {}", 
                 path, data_to_write.len(), version_to_write);
            
            // 确保目录存在
            if let Some(parent) = path.parent() {
                if let Err(e) = std::fs::create_dir_all(parent) {
                    error!("memfs: flush_paths - failed to create directory {:?}: {:?}", parent, e);
                    failure_count += 1;
                    continue;
                }
            }
            
            // 尝试处理数据（压缩/加密）
            let processed_data = if let Some(processor) = processor {
                match (processor.func)(&data_to_write, &processor.params) {
                    Ok(data) => data,
                    Err(e) => {
                        error!("memfs: flush_paths - failed to process data for {:?}: {:?}", path, e);
                        failure_count += 1;
                        continue;
                    }
                }
            } else {
                // 没有处理器，返回原始数据
                data_to_write.clone()
            };
            
            // 尝试写入磁盘
            match std::fs::write(&path, &processed_data) {
                Ok(_) => {
                    // 写入成功，尝试从缓存中移除
                    // 使用 remove_if 确保只移除版本号匹配的条目
                    let removed = cache.remove_if(&path, |_, file_data| {
                        file_data.version == version_to_write
                    });
                    
                    if removed.is_some() {
                        info!("memfs: flush_paths - file flushed and removed from cache {:?}", path);
                    } else {
                        info!("memfs: flush_paths - file flushed but already modified in cache {:?}", path);
                    }
                    success_count += 1;
                }
                Err(e) => {
                    error!("memfs: flush_paths - failed to write file {:?}: {:?}", path, e);
                    failure_count += 1;
                    // 写入失败，保留在缓存中等待下次刷新
                }
            }
        }
        
        if failure_count > 0 {
            error!("memfs: flush_paths - completed with {} successes and {} failures", success_count, failure_count);
        } else if success_count > 0 {
            info!("memfs: flush_paths - successfully flushed {} files", success_count);
        }
    }
}