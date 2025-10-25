use anyhow::Result;
use path_absolutize::Absolutize;
use serde::{ Deserialize };
use std::path::{ Path, PathBuf };
use tracing;

/// 块配置结构体
#[derive(Deserialize, Debug, Clone)]
pub struct BlockConfig {
    pub size: usize,
    pub compress: bool,
    pub encrypt: bool,
    pub compress_level: u8,
}

/// 数据块配置结构体
#[derive(Deserialize, Debug, Clone)]
pub struct ChunkConfig {
    pub fixed_size: bool,
    pub min_size: usize,
    pub avg_size: usize,
    pub max_size: usize,
}

/// 主配置结构体
#[derive(Deserialize, Debug, Clone)]
pub struct Config {
    pub block: BlockConfig,
    pub chunk: ChunkConfig,
}

impl Config {
    /// 从配置文件加载配置
    pub fn load() -> Result<Self> {
        // 尝试从当前目录读取配置文件
        let current_dir_config = Path::new("./config.yaml");
        if current_dir_config.exists() {
            tracing::info!(
                "loading configuration from current directory: {}",
                current_dir_config.display()
            );
            return Self::load_from_file(current_dir_config);
        }

        // 尝试从系统配置目录读取
        let system_config = Path::new("/etc/dedupfs/config.yaml");

        if system_config.exists() {
            tracing::info!(
                "loading configuration from system directory: {}",
                system_config.display()
            );
            return Self::load_from_file(system_config);
        }

        // 如果都不存在，使用默认配置
        tracing::warn!("no configuration file found, using default settings");
        Ok(Self::default())
    }

    /// 从指定文件加载配置
    fn load_from_file(path: &Path) -> Result<Self> {
        let contents = std::fs::read_to_string(path).map_err(|e|
            crate::errors::new(crate::errors::ConfigMissing {
                path: path.display().to_string(),
            })
        )?;

        let mut config: Self = serde_yaml::from_str(&contents).map_err(|e|
            crate::errors::new(crate::errors::ConfigParseError {
                error: e.to_string(),
            })
        )?;

        match config.normalize_path() {
            Ok(_) => Ok(config),
            Err(e) => Err(e),
        }
    }

    /// 在加载时规范化路径
    fn normalize_path(&mut self) -> Result<()> {
        // 这里可以实现路径规范化逻辑，如果需要的话
        Ok(())
    }

    /// 创建默认配置
    pub fn default() -> Self {
        Self {
            block: BlockConfig {
                size: 64*1024*1024,
                compress: true,
                encrypt: false,
                compress_level: 6,
            },
            chunk: ChunkConfig {
                fixed_size: false,
                min_size: 1048576, // 1MB
                avg_size: 2097152, // 2MB
                max_size: 4194304, // 4MB
            },
        }
    }
}

impl Default for Config {
    /// 默认配置实现
    fn default() -> Self {
        let mut config = Self {
            block: BlockConfig {
                size: 67108864, // 64MB
                compress: true,
                encrypt: false, // 默认不加密
                compress_level: 6,
            },
            chunk: ChunkConfig {
                fixed_size:false,
                min_size: 1048576, // 1MB
                avg_size: 2097152, // 2MB
                max_size: 4194304, // 4MB
            },
        };
        // 对默认配置也进行路径规范化
        config.normalize_path();
        config
    }
}
