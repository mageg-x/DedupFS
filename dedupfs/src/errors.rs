use anyhow::Error as AnyhowError;
use thiserror::Error;

macro_rules! define_error {
    // 无字段
    ($vis:vis $name:ident, $fmt:expr) => {
        #[derive(Error, Debug)]
        #[error($fmt)]
        $vis struct $name;
    };

    // 有字段（支持尾随逗号）
    ($vis:vis $name:ident, $fmt:expr, $($field:ident),+ $(,)?) => {
        #[derive(Error, Debug)]
        #[error($fmt)]
        $vis struct $name {
            $(pub $field: String,)+
        }
    };

    ($name:ident, $fmt:expr) => {
        define_error!(pub $name, $fmt);
    };
    ($name:ident, $fmt:expr, $($field:ident),+ $(,)?) => {
        define_error!(pub $name, $fmt, $($field),+);
    };
}

// === 错误定义 ===
// 加密相关错误
define_error!(pub InvalidKeyLength, "invalid key length: {key_len}, expected 16 bytes", key_len);
define_error!(pub EncryptedDataTooShort, "encrypted data too short: {length} bytes, need at least 12 bytes", length);
define_error!(pub EncryptionFailed, "encryption failed: {reason}", reason);
define_error!(pub DecryptionFailed, "decryption failed: {reason}", reason);
define_error!(pub KeyGenerationFailed, "failed to generate decryption key for block {block_id}: {error}", block_id, error);

// 块相关错误
define_error!(pub CompressionFailed, "failed to compress block {block_id}: {error}", block_id, error);
define_error!(pub BlockDeserializationFailed, "failed to deserialize block {block_id}: {error}", block_id, error);
define_error!(pub BlockCreationFailed, "failed to create new block: {error}", error);
define_error!(pub DecompressionFailed, "failed to decompress block {block_id}: {error}", block_id, error);
define_error!(pub BlockDirectoryCreateFailed, "failed to create block directory {path}: {error}", path, error);
define_error!(pub BlockSerializationFailed, "failed to serialize block {block_id}: {error}", block_id, error);
define_error!(pub BlockWriteFailed, "failed to write block to {path}: {error}", path, error);
define_error!(pub BlockSaveFailed, "failed to save block: {error}", error);
define_error!(pub BlockNotFound, "block not found: {block_id}", block_id);
define_error!(pub BlockReadFailed, "failed to read block {block_id}: {error}", block_id, error);
define_error!(pub BlockDeleteFailed, "failed to delete block: {error}", error);
define_error!(pub BlockSizeMismatch, "block size mismatch: {block_id}, expected: {expected_size}, actual: {actual_size}", block_id, expected_size, actual_size);

// 分块相关错误
define_error!(pub ChunkSizeMismatch, "chunk size mismatch: {hash}, expected: {expected_size}, actual: {actual_size}", hash, expected_size, actual_size);
define_error!(pub ChunkSizeConversionError, "failed to convert chunk size {size}: {error}", size, error);
define_error!(pub ChunkRefCountUpdateFailed, "failed to update chunk {hash} ref_count: {error}", hash, error);
define_error!(pub ChunkMetadataSaveFailed, "failed to save chunk metadata for {hash}: {error}", hash, error);
define_error!(pub ChunkExistenceCheckFailed, "failed to check chunk existence for {hash}: {error}", hash, error);
define_error!(pub ChunkNotFound, "chunk not found: {hash}", hash);
define_error!(pub ChunkDataNotFound, "chunk data not found in block {block_id}: {hash}", block_id, hash);
define_error!(pub ChunkProcessFailed, "failed to process chunk: {error}", error);

// 缓存相关错误
define_error!(pub CacheOperationFailed, "cache operation failed: {operation}", operation);
define_error!(pub CacheItemTooLarge, "cache item too large: {size} bytes, max capacity: {capacity} bytes", size, capacity);

// inode相关错误
define_error!(pub INodeNotFound, "inode not found: {ino}", ino);
define_error!(pub INodeAccessFailed, "failed to access inode {ino}: {error}", ino, error);
define_error!(pub INodeSaveFailed, "failed to save inode {ino}: {error}", ino, error);
define_error!(pub INodeRemoveFailed, "failed to remove inode {ino}: {error}", ino, error);

// 文件系统操作错误
define_error!(pub FileReadFailed, "failed to read file {path}: {error}", path, error);
define_error!(pub FileWriteFailed, "failed to write file {path}: {error}", path, error);
define_error!(pub DirectoryCreateFailed, "failed to create directory {path}: {error}", path, error);
define_error!(pub DirectoryRemoveFailed, "failed to remove directory {path}: {error}", path, error);
define_error!(pub PathBuildFailed, "failed to build path for inode {ino}", ino);
define_error!(pub MetadataLoadFailed, "failed to load metadata: {error}", error);

define_error!(pub UserNotFound, "user {id} not found", id);
define_error!(pub Timeout, "timed out");
define_error!(pub ConfigMissing, "config file {path} missing", path);
define_error!(pub ConfigParseError, "failed to parse config file: {error}", error);
define_error!(pub ConfigNormalizeError, "failed to normalize config paths: {error}", error);
define_error!(pub DaemonFailed, "daemon start failed: {reason}", reason);
define_error!(pub InvalidArguments, "invalid arguments for {command}", command);
define_error!(pub ForkFailed, "fork failed");
define_error!(pub RuntimeCreationFailed, "failed to create runtime: {reason}", reason);
define_error!(pub TaskSpawnFailed, "failed to spawn blocking task: {reason}", reason);
define_error!(pub DaemonStartupTimeout, "daemon failed to start within timeout");
define_error!(pub SocketBindError, "failed to bind socket: {path}", path);
define_error!(pub SocketConnectError, "failed to connect to socket: {path}", path);
define_error!(pub SerializationError, "serialization error: {error}", error);
define_error!(pub DeserializationError, "deserialization error: {error}", error);
define_error!(pub MountPointExists, "mount point already exists: {path}", path);
define_error!(pub MountPointNotEmpty, "mount point is not empty: {path}", path);
define_error!(pub MountPointNotFound, "mount point not found: {path}", path);
define_error!(pub DataDirCreateError, "failed to create data directory: {path}", path);
define_error!(pub MountFailed, "failed to mount filesystem: {error}", error);
define_error!(pub UnmountFailed, "failed to unmount filesystem: {error}", error);
define_error!(pub CommandNotRegistered, "command not registered: {name}", name);
define_error!(pub WatcherThreadPanic, "watcher thread panicked");
define_error!(pub FileSystemError, "filesystem error: {description}", description);
define_error!(pub MetaDataCleanError, "failed to clean meta data: {path}", path);
define_error!(pub CanonicalPathError, "failed to get canonical path: {path}", path);
define_error!(pub SocketCommunicationError, "socket communication error: {error}", error);

// === 工具函数 ===
pub fn new<E>(err: E) -> AnyhowError
where
    E: std::error::Error + Send + Sync + 'static,
{
    AnyhowError::from(err)
}

pub fn is<E: std::error::Error + 'static>(err: &AnyhowError) -> bool {
    err.chain().any(|e| e.is::<E>())
}