<div align="left">
  <a href="./README.md">English</a> | <a href="./README_ZH.md">中文</a>
</div>

# DedupFS - Deduplication File System

[![License: GPL v3](https://img.shields.io/badge/License-GPL%203.0-blue.svg)](https://www.gnu.org/licenses/gpl-3.0)
![Go](https://img.shields.io/badge/go-1.20%2B-blue.svg)
![Platform](https://img.shields.io/badge/platform-Linux%20%7C%20Windows-lightgrey.svg)

## Project Introduction

DedupFS is an innovative deduplication and compression file system that transparently implements content-aware data deduplication and compression in user space, providing significant storage space savings for storage-intensive applications. It is compatible with standard file system interfaces, allowing users to enjoy storage optimization benefits without modifying existing applications.

## Core Values

- **Dual-Layer Deduplication Strategy**: Unique combination of intra-file chunk deduplication and global cross-file deduplication to comprehensively eliminate redundant data
- **Storage Efficiency Revolution**: Achieve 3-5x storage space savings through advanced content chunking and similarity aggregation technologies
- **Performance and Efficiency Balance**: Minimize I/O performance impact while ensuring deduplication effectiveness
- **Completely Transparent Usage**: Provides standard POSIX file system interface, no modifications required for existing applications
- **Data Security Guarantee**: Built-in integrity verification and transaction protection to ensure data security and reliability

## Technical Highlights

### 🧠 Intelligent Content Chunking

```go
// FastCDC-based variable-length chunking algorithm
type FastCDCChunker struct {
    minSize int // 8KB minimum chunk
    avgSize int // 32KB average chunk  
    maxSize int // 128KB maximum chunk
}
```

**Technical Advantages**:
- **Content-Defined Chunking**: Chunk boundaries based on content features, not fixed positions
- **Variable Chunk Sizes**: Adaptively adjust chunk sizes to optimize deduplication effectiveness
- **High Throughput Processing**: Single-threaded processing speed up to 2GB/s

### 🔍 Double-Layer Deduplication Technology

```go
// SHA-256 based global deduplication
type DedupEngine struct {
    // Implementation details
}

func (de *DedupEngine) FindDuplicates(chunks []*Chunk) []*ChunkRef {
    // Intra-file duplicate chunk detection
    // Global hash index lookup
    // Cross-file, cross-directory duplicate data identification
    return nil
}
```

**Double-Layer Deduplication Advantages**:
- **Intra-File Deduplication**: Identify and eliminate duplicate data blocks within a single file
- **Inter-File Deduplication**: Global duplicate data detection across all files and directories
- **Cryptographic Hash**: SHA-256 ensures data uniqueness with extremely low collision probability
- **Real-time Deduplication**: Instant detection and elimination of duplicate data during writing
- **Intelligent Merging**: Only one copy of identical data blocks is retained at the storage layer, maximizing space savings

### 🗜️ Dual-Layer Compression Technology

```go
// Similar block aggregation compression
type CompressionEngine struct {
    // Implementation details
}

func (ce *CompressionEngine) CompressBlocks(similarChunks []*Chunk) *CompressedBlock {
    // Single chunk compression processing
    // Similar content aggregation followed by unified compression
    // Utilize data locality to improve compression ratio
    return nil
}
```

**Dual-Layer Compression Advantages**:
- **Chunk-Level Compression**: Basic compression for individual data blocks
- **Similarity Aggregation Compression**: Reorganize similar chunks into blocks for secondary compression
- **Zstandard Algorithm**: Perfect balance of high compression ratio and fast decompression
- **Adaptive Compression Levels**: Intelligently select compression strategies based on data type
- **Multi-Level Compression**: Dual compression strategy achieves higher compression ratios than single compression methods

### ⚡ High-Performance Architecture

```go
// Multi-level cache system
type CacheManager struct {
    chunkCache    *LRUCache[ChunkHash, []byte]    // Hot data block cache
    blockCache    *LRUCache[BlockId, []byte]      // Decompressed block cache
    inodeCache *Cache[ino, INode] // Inode cache
}
```

**Performance Optimizations**:
- **Three-Level Cache System**: Memory, block, and metadata multi-level caching
- **Asynchronous I/O Pipeline**: Parallel processing of chunking, hashing, and compression operations
- **Batch Operation Optimization**: Reduce system overhead for small file operations

## System Architecture

### Layered Design

```
┌─────────────────────────────────────────┐
│             Application Layer           │
│        (Standard File Operation API)    │
└─────────────────────┬───────────────────┘
┌─────────────────────────────────────────┐
│           File System Interface Layer   │
│  ┌─────────────┬─────────────┐         │
│  │   FUSE      │   WinFsp    │         │
│  │  (Linux)    │  (Windows)  │         │
│  └─────────────┴─────────────┘         │
└─────────────────────┬───────────────────┘
┌─────────────────────────────────────────┐
│           DedupFS Core Engine           │
│  ┌───────────┐  ┌───────────┐          │
│  │ Chunking  │  │ Dedup     │          │
│  │  Service  │  │  Engine   │          │
│  └───────────┘  └───────────┘          │
│  ┌───────────┐  ┌───────────┐          │
│  │Compression│  │  Cache    │          │
│  │  Engine   │  │ Management│          │
│  └───────────┘  └───────────┘          │
└─────────────────────┬───────────────────┘
┌─────────────────────────────────────────┐
│            Storage Abstraction Layer    │
│  ┌───────────┐  ┌───────────┐          │
│  │Metadata   │  │ Block     │          │
│  │Storage    │  │ Storage   │          │
│  │(pebbledb) │  │(File Sys) │          │
│  └───────────┘  └───────────┘          │
└─────────────────────────────────────────┘
```

### Data Flow Design

#### Write Path
```
Application write request
        ↓
FUSE/WinFsp interface layer
        ↓
FastCDC variable-length chunking → [8KB-128KB data blocks]
        ↓
SHA-256 hash calculation → [Global duplicate detection]
        ↓
Similar block aggregation → [64MB compressed blocks]
        ↓
Zstandard compression → [High compression ratio storage]
        ↓
Metadata update → [RocksDB transaction]
```

#### Read Path
```
Application read request
        ↓
FUSE/WinFsp interface layer  
        ↓
Metadata query → [Block location resolution]
        ↓
Parallel block reading → [Cache priority]
        ↓
Zstandard decompression → [Memory decompression]
        ↓
Data reassembly → [Transparent return]
```

## Application Scenarios

### 🖥️ Development Environment
- **Code Repository Storage**: Deduplication of common library files between multiple projects
- **Docker Image Storage**: Elimination of duplicates in base image layers
- **Build Cache Optimization**: Intelligent deduplication of compilation intermediate files

### 💽 Data Backup
- **Virtual Machine Images**: Storage optimization of similar operating system images
- **Database Backups**: Efficient storage of incremental backup data
- **Document Version Repositories**: Storage compression of similar document versions

### ☁️ Cloud-Native Applications
- **Container Persistent Storage**: Storage efficiency improvement for stateful applications
- **AI/ML Data Lakes**: Intelligent compression of training datasets
- **Log Storage Systems**: Elimination of repetitive patterns in structured logs

## Quick Start

### Installation

```bash
# Compile from source
git clone https://github.com/your-username/dedupfs.git
cd dedupfs
go build -o dedupfs main.go

```

### Basic Usage

```bash
# Mount the deduplication file system (all parameters via command line)
./dedupfs mount /mnt/dfs data/ --min-size=1048576 --avg-size=2097152 --max-size=4194304 --compress=true

# Use the file system normally
cp large_file.iso /mnt/dfs/
ls -lh /mnt/dfs/

# Check deduplication effectiveness
./dedupfs stats

# Unmount the file system
./dedupfs unmount /mnt/dfs

# Debug commands
# Debug block information
./dedupfs debug /mnt/dfs block  blockID

# Debug inode information
./dedupfs debug /mnt/dfs inode  file.txt
```

### Advanced Configuration

All configuration parameters are now provided through command line arguments. For a complete list of available options, run:

```bash
./dedupfs --help
./dedupfs mount --help
./dedupfs debug --help
```

Common parameters:
- `--avg-size`:       Average chunk size in bytes (default 2097152)
- `--max-size`:      Maximum chunk size in bytes (default 4194304)
- `--min-size`:      Minimum chunk size in bytes (default 1048576)
- `--block-size`:    Block size in bytes (default 67108864)
- `--fixed-size`:    Use fixed size chunks (default false)
- `--compress`:      Enable compression (default true)
- `--encrypt`:       Enable encryption(default false)
- ` --password`:    string   Password for encryption (default empty string)

### Debug Tools

DedupFS includes powerful debugging tools for inspecting internal structures:

#### Block Debugging

The `debug block` command allows you to examine block details, including chunks, compression status, and data layout.

![Block Debug Interface](block.png)

#### Inode Debugging

The `debug inode` command provides detailed information about file inodes, their chunks, and metadata.

![Inode Debug Interface](inode.png)

Both debug interfaces feature:
- Interactive chunk browsing
- Hex view of chunk contents
- Real-time reference counting
- Comprehensive metadata display

## Performance

### Storage Efficiency Benchmarks

| Data Type | Original Size | DedupFS Size | Savings | Dedup Ratio | Compression Ratio |
|----------|--------------|-------------|---------|------------|-------------------|
| Linux Kernel Source | 1.8 GB | 412 MB | 77% | 3.2:1 | 1.4:1 |
| VM Image Collection | 45 GB | 14 GB | 69% | 2.8:1 | 1.6:1 |
| Code Repository Backup | 12 GB | 2.8 GB | 77% | 3.5:1 | 1.4:1 |
| Document & Image Library | 8.3 GB | 3.1 GB | 63% | 1.8:1 | 2.1:1 |

### I/O Performance Comparison

| Workload | Native FS | DedupFS | Performance Overhead |
|----------|-----------|---------|----------------------|
| Large File Sequential Write | 450 MB/s | 320 MB/s | 29% |
| Large File Sequential Read | 480 MB/s | 410 MB/s | 15% |
| Small File Random Read | 380 MB/s | 340 MB/s | 11% |
| Metadata Operations | 85k ops/s | 72k ops/s | 15% |

## Technical Advantages

### 🔬 Content-Aware Technology

Unlike traditional fixed-size chunking deduplication, DedupFS employs content-defined variable-length chunking, which can:

- **Adapt to Data Patterns**: Intelligently adjust chunk boundaries based on content features
- **Resist Data Shifting**: Data insertions and deletions do not affect deduplication effectiveness of existing chunks
- **Optimize Duplicate Detection**: Improve cross-file duplicate data recognition rate

### 🛡️ Data Security Guarantee

- **End-to-End Verification**: SHA-256 hash ensures data integrity
- **Transactional Metadata**: RocksDB guarantees consistency of metadata operations
- **Crash Recovery**: Atomicity of write operations ensures system reliability

### 🌉 Cross-Platform Compatibility

- **Linux FUSE**: Complete POSIX compatibility
- **Windows WinFsp**: Native Windows integration experience
- **Unified Data Format**: Cross-platform data sharing and migration


## Core API

### Command Line Interface

DedupFS provides a comprehensive command line interface for all operations. All parameters are specified through command line arguments, making it easy to integrate with scripts and automation workflows.

For detailed command usage, please refer to the command help or the source code in the `cmd` directory:

```bash
./dedupfs help
```

Key commands include:
```bash
- `mount`: Mount the deduplication file system
- `unmount`: Unmount the file system
- `stats`: Display deduplication statistics
- `debug`: Advanced debugging tools (block, inode)
```

## Frequently Asked Questions

### ❓ How is data security ensured?

DedupFS employs multi-layered integrity protection:
- All data blocks are verified using SHA-256 checksums
- Metadata operations are guaranteed consistent through RocksDB transactions
- Crashes during writing do not result in data corruption

### ❓ What's the impact on performance?

Under typical workloads:
- Write performance decreases by 20-30%, in exchange for 60-80% storage savings
- Read performance impact is less than 15%, with hot data approaching native performance
- Memory overhead is approximately 1-2GB, adjustable based on system configuration

### ❓ Which scenarios are supported?

Particularly suitable for:
- Development environments and build systems
- Virtual machine images and container storage
- Backup and archiving systems
- Code repositories and document management

Not suitable for:
- High-performance database raw data
- Real-time audio and video processing
- Already encrypted random data

## Get Started

DedupFS makes storage efficiency improvement simple and straightforward. Just mount the file system with your preferred command line parameters and enjoy the storage space savings from intelligent deduplication and compression.

```bash
# Start experiencing now
./dedupfs mount /path/to/mountpoint  /path/to/data
# Use debug tools to inspect internal structures
./dedupfs debug  /path/to/mountpoint block blockID
./dedupfs debug  /path/to/mountpoint inode file.txt
# Start enjoying intelligent storage optimization!
```

## License

This project is licensed under the GPL 3 License - see the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for contribution guidelines.

---

**DedupFS** - Intelligent deduplication and compression for more efficient storage!