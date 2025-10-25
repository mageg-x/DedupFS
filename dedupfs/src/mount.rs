use crate::vfile::{ DedupFS };
use std::fs::{ self, File, OpenOptions };
use std::io::{ Read, Write, Seek, SeekFrom };
use std::path::{ Path, PathBuf };
use std::time::{ Duration, UNIX_EPOCH, SystemTime };
use std::process::Command;
use tracing;
use anyhow::{ Context, Result };
use sha2::{ Sha256, Digest };
use std::ffi::OsStr;
use fuser::{MountOption};
use dashmap::DashMap;

// 挂载点表，使用 DashMap 保证线程安全和高性能
pub struct MountInfo {
    pub session: fuser::BackgroundSession,
    pub data_dir: PathBuf,
    pub mount_point: PathBuf,
}

// 使用 DashMap 替代 Mutex<HashMap<...>>
lazy_static::lazy_static! {
    pub static ref MOUNT_TABLE: DashMap<PathBuf, MountInfo> = DashMap::new();
}

static WATCHER_STARTED: std::sync::atomic::AtomicBool = std::sync::atomic::AtomicBool::new(false);

pub fn watch_filesystem() {
    if
        WATCHER_STARTED.compare_exchange(
            false,
            true,
            std::sync::atomic::Ordering::Acquire,
            std::sync::atomic::Ordering::Relaxed
        ).is_err()
    {
        return;
    }
    std::thread::spawn(|| {
        let result = std::panic::catch_unwind(
            std::panic::AssertUnwindSafe(|| {
                loop {
                    std::thread::sleep(std::time::Duration::from_secs(1));
                    // 使用 DashMap 的 iter_mut 方法进行迭代和修改
                    MOUNT_TABLE.retain(|mount_point, mount_info| {
                        if mount_info.session.guard.is_finished() {
                            ::tracing::info!(
                            "mount point {} is unmounted, data stored at: {}",
                            mount_point.display(),
                            mount_info.data_dir.display()
                        );
                            clean_meta_data(mount_point);
                            false
                        } else {
                            true
                        }
                    });
                }
            })
        );

        // 无论是否 panic，都重置状态
        WATCHER_STARTED.store(false, std::sync::atomic::Ordering::Release);

        if result.is_err() {
            ::tracing::error!(
                 "{}",
                 crate::errors::new(crate::errors::WatcherThreadPanic).to_string()
             );
        }
    });
}

// 挂载文件系统
pub fn mount_filesystem(mount_point: &Path, data_dir: &Path) -> Result<String> {
    // 启动看门狗
    watch_filesystem();

    // 先强制卸载
    let _ = unmount_filesystem(&mount_point);

    // 首先检查并创建目录（如果需要）
    if !mount_point.exists() {
        tracing::info!("creating mount point directory: {}", mount_point.display());
        std::fs::create_dir_all(mount_point).map_err(|e|
            crate::errors::new(crate::errors::MountPointNotFound {
                path: mount_point.display().to_string(),
            })
        )?;
    }

    // 现在转换为绝对路径以确保唯一性
    let mount_path = std::fs::canonicalize(mount_point).map_err(|e|
        crate::errors::new(crate::errors::CanonicalPathError {
            path: mount_point.display().to_string(),
        })
    )?;

    // 检查挂载点是否已存在 - 使用 DashMap 的 contains_key 方法
    if MOUNT_TABLE.contains_key(&mount_path) {
        tracing::warn!("mount point already exists: {}", mount_path.display());
        return Err(
            crate::errors::new(crate::errors::MountPointExists {
                path: mount_path.display().to_string(),
            })
        );
    }

    // 检查挂载点目录是否为空
    let mut entries = std::fs::read_dir(&mount_path).map_err(|e|
        crate::errors::new(crate::errors::FileSystemError {
            description: format!("failed to read mount point directory: {}", e),
        })
    )?;
    if entries.next().is_some() {
        tracing::error!("mount point is not empty: {}", mount_path.display());
        return Err(
            crate::errors::new(crate::errors::MountPointNotEmpty {
                path: mount_path.display().to_string(),
            })
        );
    }

    // 创建文件系统实例
    let fs = DedupFS::new(
        mount_path.to_str().unwrap_or(""),
        data_dir.to_str().unwrap_or("")
    ).map_err(|e|
        crate::errors::new(crate::errors::FileSystemError {
            description: e.to_string(),
        })
    )?;

    // 创建挂载选项 - 移除AutoUnmount以避免自动添加allow_other选项
    let mount_options = vec![MountOption::RW, MountOption::FSName("dedupfs".to_string())];

    // 确保数据目录存在
    if !data_dir.exists() {
        tracing::info!("creating data directory: {}", data_dir.display());
        std::fs::create_dir_all(data_dir).map_err(|e|
            crate::errors::new(crate::errors::DataDirCreateError {
                path: data_dir.display().to_string(),
            })
        )?;
    }

    tracing::info!("mounting filesystem at: {}, data stored at: {}",mount_path.display(),data_dir.display());
    // 使用 spawn_mount2 在后台线程中挂载文件系统
    let session = fuser::spawn_mount2(fs, &mount_path, &mount_options).map_err(|e|
        crate::errors::new(crate::errors::MountFailed {
            error: e.to_string(),
        })
    )?;


    // 将挂载点信息添加到挂载表中 - 使用 DashMap 的 insert 方法
    let data_dir_path = std::fs::canonicalize(data_dir).map_err(|e|
        crate::errors::new(crate::errors::CanonicalPathError {
            path: data_dir.display().to_string(),
        })
    )?;
    MOUNT_TABLE.insert(mount_path.clone(), MountInfo {
        session,
        data_dir: data_dir_path,
        mount_point: mount_path.clone(),
    });

    // 等待挂载完成
    std::thread::sleep(std::time::Duration::from_millis(100));
    tracing::info!("filesystem mounted successfully: {}", mount_path.display());
    Ok(format!("filesystem mounted successfully at: {}", mount_path.display()))
}

// 卸载文件系统
pub fn unmount_filesystem(mount_point: &Path) -> Result<String> {
    unmount_filesystem2(&mount_point);

    // 转换为绝对路径以确保唯一性
    let mount_path = std::fs::canonicalize(mount_point).map_err(|e|
        crate::errors::new(crate::errors::CanonicalPathError {
            path: mount_point.display().to_string(),
        })
    )?;

    // 从挂载表中获取 BackgroundSession - 使用 DashMap 的 remove 方法
    let mount_info = MOUNT_TABLE.remove(&mount_path);

    // 检查挂载点是否存在
    let Some((_, MountInfo { session, .. })) = mount_info else {
        return Err(
            crate::errors::new(crate::errors::MountPointNotFound {
                path: mount_path.display().to_string(),
            })
        );
    };

    tracing::info!("unmounting filesystem from: {}", mount_path.display());

    session.join();
    clean_meta_data(mount_point);

    // 等待一下确保卸载完成
    std::thread::sleep(std::time::Duration::from_millis(500));

    tracing::info!("filesystem successfully unmounted: {}", mount_path.display());
    Ok(format!("filesystem unmounted successfully from: {}", mount_path.display()))
}

// 使用fusermount卸载文件系统
pub fn unmount_filesystem2(mount_point: &Path) -> Result<()> {
    tracing::info!("unmounting filesystem at {:?}", mount_point);

    let mut success = false;
    let mut last_error = None;

    // 尝试 fusermount3（推荐）
    if let Ok(output) = Command::new("fusermount3").arg("-u").arg(mount_point).output() {
        if output.status.success() {
            success = true;
        } else {
            last_error = Some(String::from_utf8_lossy(&output.stderr).to_string());
        }
    }

    // fallback 到 fusermount
    if !success {
        let output = Command::new("fusermount")
            .arg("-u")
            .arg(mount_point)
            .output()
            .map_err(|e|
                crate::errors::new(crate::errors::UnmountFailed {
                    error: e.to_string(),
                })
            )?;

        if output.status.success() {
            success = true;
        } else {
            last_error = Some(String::from_utf8_lossy(&output.stderr).to_string());
        }
    }

    if !success {
        return Err(
            crate::errors::new(crate::errors::UnmountFailed {
                error: last_error.unwrap_or_else(|| "unknown error".to_string()),
            })
        );
    }

    tracing::info!("filesystem unmounted successfully at {:?}", mount_point);
    Ok(())
}

pub fn clean_meta_data(mount_point: &Path) -> anyhow::Result<()> {
    let mount_path = std::fs::canonicalize(mount_point).map_err(|e|
        crate::errors::new(crate::errors::MetaDataCleanError {
            path: mount_point.display().to_string(),
        })
    )?;
    // 从挂载表中移除挂载点信息
    MOUNT_TABLE.remove(&mount_path);
    Ok(())
}
