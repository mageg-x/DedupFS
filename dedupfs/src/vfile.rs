use anyhow::Result;
use clap::{Arg, Command};
use libc::{ENOENT, EISDIR, EEXIST, ENOTDIR, c_int};
use serde::{Deserialize, Serialize};
use std::collections::{BTreeMap, HashMap, BTreeSet};
use std::ffi::OsStr;
use std::fs::{self}; // 仅保留fs::create_dir_all用于初始化数据目录
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicU64, Ordering};
use std::time::{Duration, SystemTime, UNIX_EPOCH};
use fuser::{Filesystem, KernelConfig, FileAttr, FileType,  MountOption, ReplyAttr, ReplyCreate, ReplyData, ReplyDirectory, ReplyEntry, ReplyEmpty, ReplyWrite, ReplyOpen, ReplyStatfs,ReplyLseek, ReplyLock, ReplyXattr, Request, TimeOrNow};
use crate::inode::{INode};
use tracing::{info, error};
use crate::kvstore::{KVStore, key_prefix, make_prefixed_key};
use std::sync::Arc;
use crate::config::{ChunkConfig, BlockConfig};
use crate::block::Block;
use std::sync::LazyLock;
use std::cell::RefCell;
use crate::chunk::Chunk;

// 去重文件系统实现
pub struct DedupFS {
    pub id: String,
    pub base_path: PathBuf,
    pub meta_path: PathBuf,
    pub data_path: PathBuf,
    pub relation_map: BTreeMap<u64, BTreeSet<u64>>, // 节点关系树
    pub next_inode: AtomicU64,
    pub kv_store: Arc<KVStore>,
    pub chunk_conf: ChunkConfig,
    pub block_conf: BlockConfig,
    pub current_block: RefCell<Option<Block>>,
}

impl DedupFS {
    pub fn new(mount_point: &str, data_root: &str) -> Result<Self> {
        info!("dedupfs::new - mount_point: {}, data_root: {}", mount_point, data_root);
        // 计算挂载点的哈希值作为子目录名
        let mount_hash = format!("{:x}", md5::compute(mount_point.as_bytes()));
        let base_path = PathBuf::from(data_root).join(&mount_hash);
        let meta_path = base_path.join("meta");
        let data_path = base_path.join("data");

        // 创建必要的目录结构
        fs::create_dir_all(&meta_path)?;
        fs::create_dir_all(&data_path)?;

        // 初始化KVStore
        let kv_store = KVStore::init("meta-data", &meta_path)?;
        
        // 设置默认配置
        let chunk_conf = ChunkConfig {
            fixed_size: false,
            min_size: 1024*1024,
            avg_size: 2*1024*1024,
            max_size: 4*1024*1024,
        };
        
        let block_conf = BlockConfig {
            size: 64 * 1024 * 1024, // 64MB
            compress: false,
            encrypt: false,
            compress_level: 3,
        };

        let mut fs = Self {
            id: mount_hash,
            base_path,
            meta_path,
            data_path,
            relation_map: BTreeMap::new(),
            next_inode: AtomicU64::new(1),
            kv_store,
            chunk_conf,
            block_conf,
            current_block: RefCell::new(None),
        };
      
        // 创建根目录
        if let Ok(None) = crate::inode::get_inode(1, &fs) {
            let root_ino = 1; // 根目录固定为1
            fs.next_inode.store(2, Ordering::SeqCst); // 设置下一个可用inode
            
            let root_inode = INode::new(root_ino, "/", 1, FileType::Directory);
            
            // 初始化根目录的子节点映射
            fs.relation_map.insert(root_ino, BTreeSet::new());
            
            crate::inode::save_inode(&root_inode, &fs)?;
            
            // 创建对应的数据目录
            let root_data_path = fs.data_path.clone();
            fs::create_dir_all(root_data_path)?;
        }

        // 加载现有的元数据
        fs.load_inode()?;

        Ok(fs)
    }
  
    pub fn build_full_path(&self, inode: &INode) -> String {
        info!("dedupfs::build_full_path - ino: {}, name: {}, parent: {}", inode.ino, inode.name, inode.parent);
        if inode.ino == 1 { return "/".to_string(); }
        
        let mut path_parts = vec![inode.name.clone()];
        let mut current = inode.parent;
        
        while current != 0 && current != 1 {
            match crate::inode::get_inode(current, self) {
                Ok(Some(parent_inode)) => {
                    path_parts.push(parent_inode.name.clone());
                    current = parent_inode.parent;
                },
                Ok(None) => {
                    error!("dedupfs::build_full_path - parent inode {} not found, breaking path building", current);
                    break;
                },
                Err(e) => {
                    error!("dedupfs::build_full_path - error getting parent inode {}: {:?}, breaking path building", current, e);
                    break;
                }
            }
        }
        
        path_parts.reverse();
        let result = if path_parts.is_empty() { "/".to_string() } else { format!("/{}", path_parts.join("/")) };
        info!("dedupfs::build_full_path - constructed path: {}", result);
        result
    }

    pub fn get_data_path(&self, path: &str) -> PathBuf {
        info!("dedupfs::get_data_path - path: {}", path);
        if path == "/" { self.data_path.clone() } else { self.data_path.join(&path[1..]) } // 去掉开头的 '/'
    }

    pub fn load_inode(&mut self) -> Result<()> {
        info!("dedupfs::load_inode - loading metadata from kvstore");
        
        // 扫描所有inode键
        let inode_prefix = format!("{}:", key_prefix::INODE);
        let (keys, _) = self.kv_store.scan(inode_prefix.as_bytes(), None, usize::MAX)?;
        
        for key in keys {
            // 从KVStore获取元数据
            if let Some(inode) = self.kv_store.get::<INode>(&key)? {
                let full_path = self.build_full_path(&inode);
                
                // 重建父子关系索引 - 只存储inode ID
                if inode.parent != inode.ino { // 避免自引用
                    self.relation_map
                        .entry(inode.parent)
                        .or_insert_with(BTreeSet::new)
                        .insert(inode.ino);
                }
                
                let current_next = self.next_inode.load(Ordering::SeqCst);
                if inode.ino >= current_next {
                    self.next_inode.store(inode.ino + 1, Ordering::SeqCst);
                }
            }
        }
        
        Ok(())
    }

    pub fn create_node(&mut self, parent: u64, name: &str, kind: FileType) -> Result<INode> {
        info!("dedupfs::create_node - parent: {}, name: {}, kind: {:?}", parent, name, kind);
        let ino = self.next_inode.fetch_add(1, Ordering::SeqCst);
        let inode = INode::new(ino, name, parent, kind);
        
        // 构建完整路径（仅用于创建数据文件/目录）
        let parent_path = if parent == 1 { "/".to_string() } else { 
            if let Ok(Some(parent_inode)) = crate::inode::get_inode(parent, self) {
                self.build_full_path(&parent_inode)
            } else {
                String::new()
            }
        };
        
        let full_path = if parent_path == "/" { 
            format!("/{}", name) 
        } else { 
            format!("{}/{}", parent_path, name) 
        };
        
        // 更新父子关系索引 - 只存储inode ID
        self.relation_map
            .entry(parent)
            .or_insert_with(BTreeSet::new)
            .insert(ino);
        
        // 如果是目录，初始化其子节点映射
        if kind == FileType::Directory {
            self.relation_map.insert(ino, BTreeSet::new());
        }

        // 保存元数据
        crate::inode::save_inode(&inode, self)?;

        Ok(inode)
    }

    pub fn lookup(&mut self, parent: u64, name: &str) -> Option<INode> {
        info!("dedupfs::lookup - parent: {}, name: {}", parent, name);
        
        // 使用父子关系索引快速查找
        if let Some(children) = self.relation_map.get(&parent) {
            for &child_ino in children {
                match crate::inode::get_inode(child_ino, self) {
                    Ok(Some(child_inode)) => {
                        if child_inode.name == name {
                            return Some(child_inode.clone());
                        }
                    },
                    Ok(None) => {
                        error!("dedupfs::lookup - child inode {} not found in kvstore", child_ino);
                    },
                    Err(e) => {
                        error!("dedupfs::lookup - error getting child inode {}: {:?}", child_ino, e);
                    }
                }
            }
        } else {
            info!("dedupfs::lookup - no children found for parent inode {}", parent);
        }
        None
    }
}


impl Filesystem for DedupFS {
    fn init(&mut self, _req: &Request<'_>, _config: &mut KernelConfig) -> Result<(), c_int> {
        // 设置最大写入块大小为 8MB
        _config.set_max_write(8 * 1024 * 1024);
        Ok(())
    }

    fn lookup(&mut self, _req: &Request, parent: u64, name: &OsStr, reply: ReplyEntry) {
        let name_str = name.to_str().unwrap_or("");
        info!("filesystem::lookup - parent: {}, name: {}", parent, name_str);
        
        if let Some(inode) = self.lookup(parent, name_str) {
            let attr = inode.to_file_attr();
            let ttl = Duration::from_secs(1);
            reply.entry(&ttl, &attr, 0);
        } else {
            info!("filesystem::lookup - entry '{}' not found in parent {}", name_str, parent);
            reply.error(ENOENT);
        }
    }

    fn getattr(&mut self, _req: &Request, ino: u64, _fh: Option<u64>, reply: ReplyAttr) {
        info!("filesystem::getattr - ino: {}, fh: {:?}", ino, _fh);
        match crate::inode::get_inode(ino, self) {
            Ok(Some(inode)) => {
                let attr = inode.to_file_attr();
                let ttl = Duration::from_secs(1);
                reply.attr(&ttl, &attr);
            },
            Ok(None) => {
                error!("filesystem::getattr - inode {} not found", ino);
                reply.error(ENOENT);
            },
            Err(e) => {
                error!("filesystem::getattr - error getting inode {}: {:?}", ino, e);
                reply.error(ENOENT);
            }
        }
    }

    // 偏移量0：保留，不使用（或者使用0作为"."的偏移量，但是这样不好，因为0可能被理解为起始偏移量）
    // 偏移量1： "."
    // 偏移量2： ".."
    // 从3开始，每个子项分配一个偏移量，按子项的 inode 排序。
    fn readdir(&mut self, _req: &Request, ino: u64, _fh: u64, offset: i64, mut reply: ReplyDirectory) {
        info!("filesystem::readdir - ino: {}, fh: {}, offset: {}", ino, _fh, offset);
        
        if let Ok(Some(parent_inode)) = crate::inode::get_inode(ino, self) {
            if parent_inode.kind != FileType::Directory { 
                error!("filesystem::readdir - inode {} is not a directory, kind: {:?}", ino, parent_inode.kind);
                reply.error(ENOTDIR); 
                return; 
            }

            // 构建所有条目
            let mut entries = Vec::new();
            entries.push((ino, ".".to_string(), FileType::Directory));
            let parent_inode = if ino == 1 { 1 } else { parent_inode.parent };
            entries.push((parent_inode, "..".to_string(), FileType::Directory));

            // 添加子项
            if let Some(children) = self.relation_map.get(&ino) {
                for &child_ino in children {
                    match crate::inode::get_inode(child_ino, self) {
                        Ok(Some(child_inode)) => {
                            entries.push((child_ino, child_inode.name.clone(), child_inode.kind));
                        },
                        Ok(None) => {
                            error!("filesystem::readdir - child inode {} not found in kvstore", child_ino);
                        },
                        Err(e) => {
                            error!("filesystem::readdir - error getting child inode {}: {:?}", child_ino, e);
                        }
                    }
                }
            } else {
                info!("filesystem::readdir - no children found for directory inode {}", ino);
            }

            // 按 inode 排序
            entries.sort_by_key(|&(inode, _, _)| inode);

            // 如果偏移量已经超过条目数量，直接返回完成
            if offset >= entries.len() as i64 {
                info!("filesystem::readdir - no more entries, offset {} >= total {}", offset, entries.len());
                reply.ok();
                return;
            }

            let mut current_offset = offset;
            for (i, &(inode, ref name, kind)) in entries.iter().enumerate().skip(offset as usize) {
                let entry_offset = i as i64;
                
                if reply.add(inode, entry_offset + 1, kind, name) {
                    // 缓冲区满了，提前返回
                    info!("filesystem::readdir - buffer full after {} entries", i - offset as usize + 1);
                    return;
                }
                current_offset = entry_offset + 1;
            }

            info!("filesystem::readdir - completed, all {} entries returned", entries.len());
            reply.ok();
        } else {
            error!("filesystem::readdir - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }

    fn mkdir(&mut self, _req: &Request, parent: u64, name: &OsStr, _mode: u32, _umask: u32, reply: ReplyEntry) {
        let name_str = name.to_str().unwrap_or("");
        info!("filesystem::mkdir - parent: {}, name: {}, mode: {}, umask: {}", parent, name_str, _mode, _umask);
        
        // 检查父目录是否存在
        if let Ok(None) = crate::inode::get_inode(parent, self) {
            error!("filesystem::mkdir - parent inode {} not found", parent);
            reply.error(ENOENT);
            return;
        }
        
        // 检查是否已存在
        if let Some(existing) = self.lookup(parent, name_str) {
            error!("filesystem::mkdir - entry '{}' already exists in parent {}, inode {}", name_str, parent, existing.ino);
            reply.error(EEXIST);
            return;
        }

        match self.create_node(parent, name_str, FileType::Directory) {
            Ok(inode) => {
                info!("filesystem::mkdir - created directory '{}' with inode {}", name_str, inode.ino);
                let attr = inode.to_file_attr();
                let ttl = Duration::from_secs(1);
                reply.entry(&ttl, &attr, 0);
            },
            Err(e) => {
                  error!("filesystem::mkdir - error creating directory: {:?}", e);
                  reply.error(libc::EIO);
              }
        }
    }

    fn unlink(&mut self, _req: &Request, parent: u64, name: &OsStr, reply: ReplyEmpty) {
        let name_str = name.to_str().unwrap_or("");
        info!("filesystem::unlink - parent: {}, name: {}", parent, name_str);
        
        if let Some(inode) = self.lookup(parent, name_str) {
            if inode.kind == FileType::Directory { 
                error!("filesystem::unlink - trying to unlink a directory '{}', not allowed", name_str);
                reply.error(EISDIR); 
                return; 
            }

            // 构建完整路径（用于删除数据目录）
            let full_path = self.build_full_path(&inode);
            
            // 从父子关系索引中移除
            if let Some(children) = self.relation_map.get_mut(&parent) {
                if children.remove(&inode.ino) {
                    info!("filesystem::unlink - removed inode {} from parent {} relation map", inode.ino, parent);
                }
            }

            // 删除元数据文件
            if let Err(e) = crate::inode::remove_inode(inode.ino, self) {
                error!("filesystem::unlink - error removing metadata for inode {}: {:?}", inode.ino, e);
            }

            info!("filesystem::unlink - successfully removed file '{}' with inode {}", name_str, inode.ino);
            reply.ok();
        } else {
            error!("filesystem::unlink - file '{}' not found in parent {}", name_str, parent);
            reply.error(ENOENT);
        }
    }


    fn rmdir(&mut self, _req: &Request, parent: u64, name: &OsStr, reply: ReplyEmpty) {
        let name_str = name.to_str().unwrap_or("");
        info!("filesystem::rmdir - parent: {}, name: {}", parent, name_str);
        
        if let Some(inode) = self.lookup(parent, name_str) {
            if inode.kind != FileType::Directory { 
                error!("filesystem::rmdir - '{}' is not a directory", name_str);
                reply.error(ENOTDIR); 
                return; 
            }

            // 检查目录是否为空 - 现在可以快速检查
            if let Some(children) = self.relation_map.get(&inode.ino) {
                if !children.is_empty() {
                    error!("filesystem::rmdir - directory '{}' is not empty, contains {} entries", name_str, children.len());
                    reply.error(libc::ENOTEMPTY); 
                    return; 
                }
            } else {
                error!("filesystem::rmdir - no relation map entry for directory inode {}", inode.ino);
            }

            // 构建完整路径（用于删除数据目录）
            let full_path = self.build_full_path(&inode);
            
            // 从父子关系索引中移除
            if let Some(children) = self.relation_map.get_mut(&parent) {
                if children.remove(&inode.ino) {
                    info!("filesystem::rmdir - removed inode {} from parent {} relation map", inode.ino, parent);
                }
            }
            
            // 移除该目录的子节点映射
            if self.relation_map.remove(&inode.ino).is_some() {
                info!("filesystem::rmdir - removed child map for directory inode {}", inode.ino);
            }

            // 删除元数据文件
            if let Err(e) = crate::inode::remove_inode(inode.ino, self) {
                error!("filesystem::rmdir - error removing metadata for inode {}: {:?}", inode.ino, e);
            }

            info!("filesystem::rmdir - successfully removed directory '{}' with inode {}", name_str, inode.ino);
            reply.ok();
        } else {
            error!("filesystem::rmdir - directory '{}' not found in parent {}", name_str, parent);
            reply.error(ENOENT);
        }
    }

    fn rename(&mut self, _req: &Request, parent: u64, name: &OsStr, newparent: u64, newname: &OsStr, _flags: u32, reply: ReplyEmpty) {
        let name_str = name.to_str().unwrap_or("");
        let newname_str = newname.to_str().unwrap_or("");
        info!("filesystem::rename - parent: {}, name: {}, newparent: {}, newname: {}, flags: {}", parent, name_str, newparent, newname_str, _flags);
        
        if let Some(mut inode) = self.lookup(parent, name_str) {
            // 构建旧路径和新路径
            let old_parent_path = if parent == 1 { "/".to_string() } else { 
                if let Ok(Some(parent_inode)) = crate::inode::get_inode(parent, self) {
                    self.build_full_path(&parent_inode)
                } else {
                    String::new()
                }
            };
            
            let old_full_path = if old_parent_path == "/" { 
                format!("/{}", name_str) 
            } else { 
                format!("{}/{}", old_parent_path, name_str) 
            };

            let new_parent_path = if newparent == 1 { "/".to_string() } else { 
                if let Ok(Some(parent_inode)) = crate::inode::get_inode(newparent, self) {
                    self.build_full_path(&parent_inode)
                } else {
                    String::new()
                }
            };
            
            let new_full_path = if new_parent_path == "/" { 
                format!("/{}", newname_str) 
            } else { 
                format!("{}/{}", new_parent_path, newname_str) 
            };

            // 更新元数据
            inode.name = newname_str.to_string();
            inode.parent = newparent;
            inode.mtime = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
            inode.ctime = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
            
            // 更新父子关系索引
            if let Some(children) = self.relation_map.get_mut(&parent) {
                children.remove(&inode.ino);
            }
            
            self.relation_map
                .entry(newparent)
                .or_insert_with(BTreeSet::new)
                .insert(inode.ino);

            // 保存更新的元数据
            let _ = crate::inode::save_inode(&inode, self);

            reply.ok();
        } else {
            error!("filesystem::readlink - inode {} not found", name_str);
            reply.error(ENOENT);
        }
    }

    fn create(&mut self, _req: &Request, parent: u64, name: &OsStr, _mode: u32, _umask: u32, flags: i32, reply: ReplyCreate) {
        let name_str = name.to_str().unwrap_or("");
        info!("filesystem::create - parent: {}, name: {}, mode: {}, umask: {}, flags: {}", parent, name_str, _mode, _umask, flags);
        
        // 检查是否已存在
        if let Some(existing) = self.lookup(parent, name_str) {
            if existing.kind == FileType::Directory { 
                error!("filesystem::create - cannot create file '{}', directory with same name exists", name_str);
                reply.error(EISDIR); 
                return; 
            }
            // 如果文件已存在，我们覆盖它
            let full_path = self.build_full_path(&existing);
            let data_path = self.get_data_path(&full_path);
           
            
            // 更新元数据
            let inode_ino = existing.ino;
            let mut saved_attr: Option<FileAttr> = None;
            
            // 第一阶段：修改元数据
            {  
                match crate::inode::get_inode(inode_ino, self) {
                    Ok(Some(mut inode)) => {
                        inode.update_size(0);
                        saved_attr = Some(inode.to_file_attr());
                        // 保存更新的元数据
                        if let Err(e) = crate::inode::save_inode(&inode, self) {
                            error!("filesystem::create - error saving metadata: {:?}", e);
                        }
                    },
                    Ok(None) => {
                        error!("filesystem::create - inode {} not found during update", inode_ino);
                    },
                    Err(e) => {
                        error!("filesystem::create - error getting inode {}: {:?}", inode_ino, e);
                    }
                }
            }
            
            if let Some(attr) = saved_attr {
                let ttl = Duration::from_secs(1);
                reply.created(&ttl, &attr, 0, 0, flags.try_into().unwrap_or(0));
                return;
            }
        }

        match self.create_node(parent, name_str, FileType::RegularFile) {
            Ok(inode) => { let attr = inode.to_file_attr(); let ttl = Duration::from_secs(1); reply.created(&ttl, &attr, 0, 0, flags.try_into().unwrap_or(0)); }
            Err(e) => {
                error!("filesystem::create - error creating node: {:?}", e);
                reply.error(libc::EIO);
            }
        }
    }

    fn open(&mut self, _req: &Request, ino: u64, flags: i32, reply: ReplyOpen) {
        info!("filesystem::open - ino: {}, flags: {}", ino, flags);
        if let Ok(Some(_)) = crate::inode::get_inode(ino, self) { 
            reply.opened(ino, flags.try_into().unwrap_or(0)); 
        } else { 
            error!("filesystem::open - inode {} not found", ino);
            reply.error(ENOENT); 
        }
    }

    fn read(&mut self, _req: &Request, ino: u64, _fh: u64, offset: i64, size: u32, _flags: i32, _lock: Option<u64>, reply: ReplyData) {
        info!("filesystem::read - ino: {}, fh: {}, offset: {}, size: {}, flags: {}, lock: {:?}", ino, _fh, offset, size, _flags, _lock);
        match crate::inode::get_inode(ino, self) {
            Ok(Some(inode)) => {
                if inode.kind != FileType::RegularFile {
                    error!("filesystem::read - inode {} is not a regular file, kind: {:?}", ino, inode.kind);
                    reply.error(libc::EISDIR);
                    return;
                }

                // 确保偏移量有效
                if offset < 0 {
                    error!("filesystem::read - negative offset: {}", offset);
                    reply.error(libc::EINVAL);
                    return;
                }
                
                // 使用INode的read方法读取数据
                let data = inode.read(offset as u64, size as usize, self);
                info!("filesystem::read - read {} bytes from inode {}", data.len(), ino);
                reply.data(&data);
            },
            Ok(None) => {
                error!("filesystem::read - inode {} not found", ino);
                reply.error(ENOENT);
            },
            Err(e) => {
                error!("filesystem::read - error getting inode {}: {:?}", ino, e);
                reply.error(ENOENT);
            },
        }
    }

    fn write(&mut self, _req: &Request, ino: u64, _fh: u64, offset: i64, data: &[u8], _write_flags: u32, _flags: i32, _lock: Option<u64>, reply: ReplyWrite) {
        info!("filesystem::write - ino: {}, fh: {}, offset: {}, data_len: {}, write_flags: {}, flags: {}, lock: {:?}", ino, _fh, offset, data.len(), _write_flags, _flags, _lock);
        
        // 确保偏移量有效
        if offset < 0 {
            error!("filesystem::write - negative offset: {}", offset);
            reply.error(libc::EINVAL);
            return;
        }
        
        // 获取并更新inode
        if let Ok(Some(mut inode)) = crate::inode::get_inode(ino, self) {
            if inode.kind != FileType::RegularFile { 
                error!("filesystem::write - inode {} is not a regular file, kind: {:?}", ino, inode.kind);
                reply.error(libc::EISDIR); 
                return; 
            }
            
            // 使用DedupFS实例作为参数
            inode.write(offset as u64, data, self);
            
            // 保存更新后的元数据
            if crate::inode::cache_inode(&inode, self).is_err() {
                error!("filesystem::write - error saving metadata for inode {}", ino);
                reply.error(libc::EIO);
                return;
            }
            
            info!("filesystem::write - written {} bytes to inode {} at offset {}", data.len(), ino, offset);
            reply.written(data.len() as u32);
        } else {
            error!("filesystem::write - inode {} not found", ino);
            reply.error(libc::ENOENT);
            return;
        }
    }

    fn release(&mut self, _req: &Request, _ino: u64, _fh: u64, _flags: i32, _lock_owner: Option<u64>, _flush: bool, reply: ReplyEmpty) { 
        info!("filesystem::release - ino: {}, fh: {}, flags: {}, lock_owner: {:?}, flush: {}", _ino, _fh, _flags, _lock_owner, _flush);
        // 保存更新的元数据
        if let Ok(Some(inode)) = crate::inode::get_inode(_ino, self) {
            let _ = crate::inode::save_inode(&inode, self);
        }
        reply.ok(); 
    }

    fn statfs(&mut self, _req: &Request, _ino: u64, reply: ReplyStatfs) { 
        info!("filesystem::statfs - ino: {}", _ino);

        // 扫描KVStore获取总文件数
        let inode_prefix = format!("{}:", key_prefix::INODE);
        let (keys, _) = self.kv_store.scan(inode_prefix.as_bytes(), None, usize::MAX).unwrap_or_default();
        let total_files = keys.len() as u64;

        // 直接使用 data_path 所在文件系统的统计信息
        match nix::sys::statvfs::statvfs(&self.data_path) {
            Ok(stat) => {
                reply.statfs(
                    stat.blocks(),   // 总块数
                    stat.blocks_free(),     // 空闲块数
                    stat.blocks_available(), // 可用块数
                    total_files,           // 总文件数
                    stat.files_free(),      // 空闲文件数
                    stat.block_size() as u32, // 块大小
                    stat.name_max() as u32, // 最大文件名长度
                    stat.fragment_size() as u32, // 片段大小
                );
            }
            Err(e) => {
                // 如果获取失败，使用合理的默认值
                reply.statfs(
                    0,  // 总块数 (约 100GB)
                    0,   // 空闲块数
                    0,  // 可用块数
                    0,   // 总文件数
                    0,   // 空闲文件数
                    0,   // 块大小
                    0, // 最大文件名长度
                    0,  // 片段大小
                );
            }
        }
    }

    fn setattr(&mut self, _req: &Request, ino: u64, mode: Option<u32>, uid: Option<u32>, gid: Option<u32>, size: Option<u64>, atime: Option<TimeOrNow>, mtime: Option<TimeOrNow>, _ctime: Option<SystemTime>, _fh: Option<u64>, _crtime: Option<SystemTime>, _chgtime: Option<SystemTime>, _bkuptime: Option<SystemTime>, _flags: Option<u32>, reply: ReplyAttr) {
        info!("filesystem::setattr - ino: {}, mode: {:?}, uid: {:?}, gid: {:?}, size: {:?}, atime: {:?}, mtime: {:?}, fh: {:?}", ino, mode, uid, gid, size, atime, mtime, _fh);
        // 如果需要调整大小，先获取路径信息
        if let Some(size_val) = size {
            if let Ok(Some(mut inode)) = crate::inode::get_inode(ino, self) {
                if inode.kind != FileType::RegularFile { 
                    error!("filesystem::setattr - inode {} is not a regular file, kind: {:?}", ino, inode.kind);
                    reply.error(libc::EISDIR); 
                    return; 
                }
                if let Err(e) = inode.truncate(size_val, self) {
                    error!("filesystem::setattr - failed to truncate inode {} to size {}: {:?}", ino, size_val, e);
                    reply.error(libc::EIO);
                    return;
                }
                // 保存更新后的inode
                if let Err(e) = crate::inode::cache_inode(&inode, self) {
                    error!("filesystem::setattr - failed to save inode {} after truncate: {:?}", ino, e);
                    reply.error(libc::EIO);
                    return;
                }
            } else {
                error!("filesystem::setattr - inode {} not found during size adjustment", ino);
                reply.error(libc::ENOENT);
                return;
            }
        }
        
        let saved_attr: Option<FileAttr> = None;
        
        // 获取并更新元数据
        if let Ok(Some(mut inode)) = crate::inode::get_inode(ino, self) {
            let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos();
            
            if let Some(mode) = mode { inode.perm = mode as u16; }
            if let Some(uid) = uid { inode.uid = uid; }
            if let Some(gid) = gid { inode.gid = gid; }
            if let Some(size) = size {
                inode.update_size(size);
            }
            
            // 更新时间
            if let Some(atime) = atime {
                inode.atime = match atime {
                    TimeOrNow::Now => now,
                    TimeOrNow::SpecificTime(time) => time.duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos()
                };
            }
            
            if let Some(mtime) = mtime {
                inode.mtime = match mtime {
                    TimeOrNow::Now => now,
                    TimeOrNow::SpecificTime(time) => time.duration_since(UNIX_EPOCH).unwrap_or_default().as_nanos()
                };
            }
            
            inode.ctime = now;
            
            // 保存元数据
            let _ = crate::inode::save_inode(&inode, self);
            
            // 返回更新后的属性
            let attr = inode.to_file_attr();
            let ttl = Duration::from_secs(1);
            reply.attr(&ttl, &attr);
        } else {
            error!("filesystem::setattr - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }

    // 其他必要的方法实现
    fn flush(&mut self, _req: &Request, ino: u64, _fh: u64, _lock_owner: u64, reply: ReplyEmpty) {
        info!("filesystem::flush - ino: {}, fh: {}, lock_owner: {}", ino, _fh, _lock_owner);
        if let Ok(Some(_)) = crate::inode::get_inode(ino, self) { 
            reply.ok(); 
        } else { 
            error!("filesystem::flush - inode {} not found", ino);
            reply.error(ENOENT); 
        }
    }

    fn fsync(&mut self, _req: &Request, ino: u64, _fh: u64, datasync: bool, reply: ReplyEmpty) {
        info!("filesystem::fsync - ino: {}, fh: {}, datasync: {}", ino, _fh, datasync);
        if let Ok(Some(inode)) = crate::inode::get_inode(ino, self) {
            let _ = crate::inode::save_inode(&inode, self);
            reply.ok();
        } else {
            error!("filesystem::fsync - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }
    fn symlink(&mut self, _req: &Request, parent: u64, name: &OsStr, target: &Path, reply: ReplyEntry) {
        info!("filesystem::symlink - parent: {}, name: {}, target: {}", parent, name.to_string_lossy(), target.display());
        
        let name_str = name.to_str().unwrap_or("");
        
        // 检查父目录是否存在
        if let Ok(None) = crate::inode::get_inode(parent, self) {
            error!("filesystem::symlink - parent inode {} not found", parent);
            reply.error(ENOENT);
            return;
        }
        
        // 检查链接名是否已存在
        if self.lookup(parent, name_str).is_some() {
            error!("filesystem::symlink - name '{}' already exists in parent {}", name_str, parent);
            reply.error(EEXIST);
            return;
        }
        
        // 生成新的inode
        let ino = self.next_inode.fetch_add(1, Ordering::SeqCst);
        
        // 创建符号链接元数据
        let mut inode = INode::new(ino, name_str, parent, FileType::Symlink);
        
        // 构建完整路径
        let full_path = if parent == 1 {
            format!("/{}", name_str)
        } else if let Ok(Some(parent_inode)) = crate::inode::get_inode(parent, self) {
            format!("{}/{}", self.build_full_path(&parent_inode), name_str)
        } else {
            error!("filesystem::symlink - failed to build full path for parent {}", parent);
            reply.error(ENOENT);
            return;
        };
        
        // 更新父子关系
        self.relation_map
            .entry(parent)
            .or_insert_with(BTreeSet::new)
            .insert(ino);
        
        // 存储符号链接目标到inode元数据
        inode.symlink_target = Some(target.to_string_lossy().to_string());
        
        // 保存元数据
        if crate::inode::save_inode(&inode, self).is_err() {
            error!("filesystem::symlink - error saving metadata for inode {}", ino);
            reply.error(libc::EIO);
            return;
        }
        
        // 返回创建成功的响应
        let attr = inode.to_file_attr();
        let ttl = Duration::from_secs(1);
        reply.entry(&ttl, &attr, 0);
    }

        /// Read symbolic link.
    fn readlink(&mut self, _req: &Request<'_>, ino: u64, reply: ReplyData) {
        info!("filesystem::readlink - ino: {}", ino);
        
        // 检查inode是否存在
        if let Ok(Some(inode)) = crate::inode::get_inode(ino, self) {
            // 检查是否是符号链接
            if inode.kind != FileType::Symlink {
                error!("filesystem::readlink - inode {} is not a symlink, kind: {:?}", ino, inode.kind);
                reply.error(libc::EINVAL);
                return;
            }
            
            // 从inode的元数据中读取符号链接目标
            if let Some(target) = &inode.symlink_target {
                reply.data(target.as_bytes());
            } else {
                reply.data(b"");
            }
        } else {
            error!("filesystem::readlink - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }

    fn access(&mut self, _req: &Request<'_>, ino: u64, mask: i32, reply: ReplyEmpty) {
        info!("filesystem::access - ino: {}, mask: {}", ino, mask);
        
        // 检查inode是否存在
        if let Ok(Some(inode)) = crate::inode::get_inode(ino, self) {
            // 获取当前用户的UID和GID
            let current_uid = unsafe { libc::getuid() };
            let current_gid = unsafe { libc::getgid() };
            
            // 检查权限
            let mut allowed = true;
            
            // 根据用户类型（所有者、组成员、其他用户）检查权限
            if current_uid == inode.uid {
                // 所有者权限检查
                allowed = match mask {
                    libc::R_OK => (inode.perm & 0o400) != 0,
                    libc::W_OK => (inode.perm & 0o200) != 0,
                    libc::X_OK => (inode.perm & 0o100) != 0,
                    libc::F_OK => true, // F_OK只检查文件是否存在
                    _ => (inode.perm & ((mask as u16) << 6)) != 0,
                };
            } else if current_gid == inode.gid {
                // 组成员权限检查
                allowed = match mask {
                    libc::R_OK => (inode.perm & 0o040) != 0,
                    libc::W_OK => (inode.perm & 0o020) != 0,
                    libc::X_OK => (inode.perm & 0o010) != 0,
                    libc::F_OK => true,
                    _ => (inode.perm & ((mask as u16) << 3)) != 0,
                };
            } else {
                // 其他用户权限检查
                allowed = match mask {
                    libc::R_OK => (inode.perm & 0o004) != 0,
                    libc::W_OK => (inode.perm & 0o002) != 0,
                    libc::X_OK => (inode.perm & 0o001) != 0,
                    libc::F_OK => true,
                    _ => (inode.perm & (mask as u16)) != 0,
                };
            }
            
            if allowed {
                reply.ok();
            } else {
                  error!("filesystem::access - permission denied for inode {}, mask: {}", ino, mask);
                  reply.error(libc::EACCES);
              }
        } else {
            error!("filesystem::access - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }

    fn lseek(&mut self, _req: &Request, ino: u64, _fh: u64, offset: i64, whence: i32, reply: ReplyLseek) {
        info!("filesystem::lseek - ino: {}, offset: {}, whence: {}", ino, offset, whence);
        
        // 检查inode是否存在
        if let Ok(Some(inode)) = crate::inode::get_inode(ino, self) {
            // 获取文件大小
            let file_size = inode.size as i64;
            
            // 计算新的文件位置
            let new_offset = match whence {
                libc::SEEK_SET => offset,  // 从文件开头偏移
                libc::SEEK_CUR => offset,  // 从当前位置偏移（由于没有实际的文件指针状态，这里简化处理为直接使用offset）
                libc::SEEK_END => file_size + offset,  // 从文件结尾偏移
                _ => {
                    // 不支持的whence值
                    error!("filesystem::lseek - unsupported whence value: {}", whence);
                    reply.error(libc::EINVAL);
                    return;
                }
            };
            
            // 确保偏移量不为负数
            if new_offset < 0 {
                error!("filesystem::lseek - negative offset: {}", new_offset);
                reply.error(libc::EINVAL);
                return;
            }
            
            // 返回新的文件位置
            reply.offset((new_offset as u64).try_into().unwrap());
        } else {
            // inode不存在
            error!("filesystem::lseek - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }

    fn getlk(&mut self, _req: &Request, ino: u64, _fh: u64, _lock_owner: u64, _start: u64, _end: u64, _typ: i32, _pid: u32, reply: ReplyLock) {
        info!("filesystem::getlk - ino: {}, fh: {}, lock_owner: {}, start: {}, end: {}, typ: {}, pid: {}", 
              ino, _fh, _lock_owner, _start, _end, _typ, _pid);
        
        error!("filesystem::getlk - file locking not supported");
        reply.error(libc::ENOSYS);
    }

    fn setlk(&mut self, _req: &Request, ino: u64, _fh: u64, _lock_owner: u64, _start: u64, _end: u64, _typ: i32, _pid: u32, _sleep: bool, reply: ReplyEmpty) {
        info!("filesystem::setlk - ino: {}, fh: {}, lock_owner: {}, start: {}, end: {}, typ: {}, pid: {}, sleep: {}", 
              ino, _fh, _lock_owner, _start, _end, _typ, _pid, _sleep);
        
        error!("filesystem::setlk - file locking not supported");
        reply.error(libc::ENOSYS);
    }

    fn setxattr(&mut self, _req: &Request, ino: u64, name: &OsStr, value: &[u8], _flags: i32, _position: u32, reply: ReplyEmpty) {
        info!("filesystem::setxattr - ino: {}, name: {}, value: {}, flags: {}, position: {}", 
              ino, name.to_string_lossy(), value.len(), _flags, _position);
        let name_str = name.to_str().unwrap_or("");
        
        if let Ok(Some(mut inode)) = crate::inode::get_inode(ino, self) {
            // 使用inode内部的set_xattr方法
            inode.set_xattr(name_str, value);
            
            // 保存更新后的元数据
            if crate::inode::save_inode(&inode, self).is_err() {
                error!("filesystem::setxattr - error saving metadata for inode {}", ino);
                reply.error(libc::EIO);
                return;
            }
            reply.ok();
        } else {
            error!("filesystem::setxattr - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }


    fn getxattr(&mut self, _req: &Request, ino: u64, name: &OsStr, size: u32, reply: ReplyXattr) {
        info!("filesystem::getxattr - ino: {}, name: {}, size: {}", ino, name.to_string_lossy(), size);
        let name_str = name.to_str().unwrap_or("");
        
        match crate::inode::get_inode(ino, self) {
            Ok(Some(inode)) => {
                // 使用inode内部的get_xattr方法
                match inode.get_xattr(name_str) {
                    Some(value) => {
                        if size == 0 {
                            // 返回属性值大小
                            reply.size(value.len() as u32);
                        } else if size >= value.len() as u32 {
                            // 返回属性值
                            reply.data(&value);
                        } else {
                            error!("filesystem::getxattr - buffer too small for xattr '{}', need {} bytes", name_str, value.len());
                            reply.error(libc::ERANGE);
                        }
                    },
                    None => {
                        // 属性不存在
                        info!("filesystem::getxattr - xattr '{}' not found on inode {}", name_str, ino);
                        reply.error(libc::ENODATA);
                    },
                }
            },
            Ok(None) => {
                error!("filesystem::getxattr - inode {} not found", ino);
                reply.error(libc::ENODATA);
            },
            Err(e) => {
                error!("filesystem::getxattr - error getting inode {}: {:?}", ino, e);
                reply.error(ENOENT);
            },
        }
    }

    fn listxattr(&mut self, _req: &Request, ino: u64, size: u32, reply: ReplyXattr) {
        info!("filesystem::listxattr - ino: {}, size: {}", ino, size);
        if let Ok(Some(inode)) = crate::inode::get_inode(ino, self) {
            // 使用inode内部的list_xattr方法
            let attrs = inode.list_xattr();
            
            // 构建属性名列表（以 null 分隔的字符串）
            let mut attr_list = Vec::new();
            for attr_str in attrs {
                attr_list.extend_from_slice(attr_str.as_bytes());
                attr_list.push(0); // null 分隔符
            }
            
            if size == 0 {
                // 返回所需缓冲区大小
                reply.size(attr_list.len() as u32);
            } else if size >= attr_list.len() as u32 {
                // 返回属性列表
                reply.data(&attr_list);
            } else {
                    error!("filesystem::listxattr - buffer too small, need {} bytes", attr_list.len());
                    reply.error(libc::ERANGE);
                }
        } else {
            error!("filesystem::listxattr - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }

    fn removexattr(&mut self, _req: &Request, ino: u64, name: &OsStr, reply: ReplyEmpty) {
        info!("filesystem::removexattr - ino: {}, name: {}", ino, name.to_string_lossy());
        let name_str = name.to_str().unwrap_or("");
               
        if let Ok(Some(mut inode)) = crate::inode::get_inode(ino, self) {
            // 使用inode内部的remove_xattr方法
            if !inode.remove_xattr(name_str) {
            // 属性不存在
            error!("filesystem::removexattr - xattr '{}' not found on inode {}", name_str, ino);
            reply.error(libc::ENODATA);
            return;
        }
            
            // 保存更新后的元数据
            if crate::inode::save_inode(&inode, self).is_err() {
                error!("filesystem::removexattr - error saving metadata for inode {}", ino);
                reply.error(libc::EIO);
                return;
            }
            reply.ok();
        } else {
            error!("filesystem::removexattr - inode {} not found", ino);
            reply.error(ENOENT);
        }
    }
}