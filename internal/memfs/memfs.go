/*
 * Copyright (C) 2025-2025 raochaoxun <raochaoxun@gmail.com>.
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <https://www.gnu.org/licenses/>.
 */

package memfs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mageg-x/dedupfs/internal/log"
	"github.com/mageg-x/dedupfs/internal/utils"
)

var (
	// 获取logger实例用于输出日志，与mount包保持一致的名称
	logger = log.GetLogger("dedupfs")
)

// ProcessorFunc 定义处理器函数类型
type ProcessorFunc func(data []byte, params map[string]string) ([]byte, error)

// Processor 封装处理逻辑和参数
type Processor struct {
	Func   ProcessorFunc
	Params map[string]string
}

// FileData 缓存中的文件数据结构
type FileData struct {
	data      *[]byte // 指向堆上独立分配的内存（write 时拷贝一次）
	version   uint64  // 版本号，用于一致性检查
	processor *Processor
}

// FlushCommand 用于通知刷盘任务
type FlushCommand struct {
	Paths    []string
	Shutdown bool
}

// MemFs 内存文件系统
type MemFs struct {
	cache      sync.Map          // key: absPath, value: *FileData
	flushChan  chan FlushCommand // 异步刷盘通道
	shutdown   atomic.Bool
	shutdownWG sync.WaitGroup
}

var (
	instance *MemFs
	initOnce sync.Once
)

// GetInstance 获取单例
func GetInstance() *MemFs {
	initOnce.Do(func() {
		m := &MemFs{
			flushChan: make(chan FlushCommand, 1024),
		}
		m.shutdownWG.Add(1)
		go m.flushTask()
		instance = m
		logger.Infof("memfs: initialized with sync.Map + sharded locks")
	})
	return instance
}

// Write 写入文件到内存缓存（线程安全，原子更新）
func (m *MemFs) Write(path string, contents []byte, processor *Processor) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	// 必须拷贝 caller 的数据，防止外部修改影响缓存
	dataCopy := make([]byte, len(contents))
	copy(dataCopy, contents)
	dataPtr := &dataCopy

	return utils.WithLockKey(absPath, func() error {
		var newVersion uint64 = 1
		if existing, ok := m.cache.Load(absPath); ok {
			existingData := existing.(*FileData)
			newVersion = existingData.version + 1
		}

		m.cache.Store(absPath, &FileData{
			data:      dataPtr,
			version:   newVersion,
			processor: processor,
		})

		action := "created"
		if newVersion > 1 {
			action = "updated"
		}
		logger.Infof("memfs: %s file %s, size: %d, version: %d", action, absPath, len(dataCopy), newVersion)

		// 异步触发刷盘
		select {
		case m.flushChan <- FlushCommand{Paths: []string{absPath}}:
		default:
			logger.Infof("memfs: flush channel full, dropping flush request")
		}
		return nil
	})
}

// Read 从缓存或磁盘读取文件（线程安全）
func (m *MemFs) Read(path string, unprocessor *Processor) ([]byte, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	if val, ok := m.cache.Load(absPath); ok {
		fileData := val.(*FileData)
		data := *fileData.data
		version := fileData.version

		var result []byte
		if unprocessor != nil {
			if processed, err := unprocessor.Func(data, unprocessor.Params); err == nil {
				result = processed
			} else {
				logger.Errorf("memfs: unprocess failed for %s: %v", absPath, err)
				result = data
			}
		} else {
			result = data
		}

		// 安全返回：必须拷贝，防止调用者修改缓存
		out := make([]byte, len(result))
		copy(out, result)
		logger.Infof("memfs: read from cache %s, size: %d, version: %d", absPath, len(out), version)
		return out, nil
	}

	// 从磁盘读取
	if _, err := os.Stat(absPath); err == nil {
		data, err := os.ReadFile(absPath)
		if err != nil {
			return nil, err
		}
		if unprocessor != nil {
			if processed, err := unprocessor.Func(data, unprocessor.Params); err == nil {
				return processed, nil
			}
			logger.Errorf("memfs: unprocess disk file %s failed: %v", absPath, err)
		}
		return data, nil
	}

	return nil, fmt.Errorf("file not found: %s", absPath)
}

// Remove 删除文件（缓存 + 磁盘）
func (m *MemFs) Remove(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	// 原子删除缓存
	utils.WithLockKey(absPath, func() error {
		m.cache.Delete(absPath)
		return nil
	})

	logger.Infof("memfs: removed from cache %s", absPath)

	// 删除磁盘文件
	delfile := func(_filepath string) error {
		if _, err := os.Stat(_filepath); err == nil {
			if err := os.Remove(_filepath); err != nil {
				logger.Errorf("memfs: remove file failed %s: %v", _filepath, err)
				return err
			}

			dir := filepath.Dir(_filepath)
			for i := 0; i < 3; i++ {
				// 安全边界：避免删除 . / 根目录 / 当前工作目录等
				if dir == "/" || dir == "." || dir == "" {
					break
				}

				// 尝试删除（os.Remove 只能删空目录）
				if err := os.Remove(dir); err != nil {
					// 目录非空、不存在、权限不足等 → 停止向上删
					break
				}

				// 成功删除，继续向上
				dir = filepath.Dir(dir)
			}
		}

		// 因为还在缓存中没有写，或者正在写进行中
		logger.Errorf("memfs: remove file not found %s", _filepath)
		return errors.New("file not found")
	}

	if err := delfile(absPath); err != nil {
		// 因为删除失败， 延迟3s再删除一次
		time.AfterFunc(3*time.Second, func() {
			logger.Infof("memfs: remove file retry %s", absPath)
			delfile(absPath)
		})
	}

	return nil
}

// flushPaths 刷盘指定路径（带版本检查）
func (m *MemFs) flushPaths(paths []string) {
	for _, path := range paths {
		val, ok := m.cache.Load(path)
		if !ok {
			continue
		}
		fileData := val.(*FileData)
		data := *fileData.data
		version := fileData.version
		processor := fileData.processor

		// 准备写入数据（可能需要处理）
		writeData := data
		if processor != nil {
			if processed, err := processor.Func(data, processor.Params); err == nil {
				writeData = processed
			} else {
				logger.Errorf("memfs: processor error on flush %s: %v", path, err)
				continue
			}
		}

		// 确保目录存在
		if dir := filepath.Dir(path); dir != "." {
			if err := os.MkdirAll(dir, 0755); err != nil {
				logger.Errorf("memfs: mkdir failed %s: %v", dir, err)
				continue
			}
		}

		// 写磁盘（锁外执行！）
		// 再次检查是否已经被删除
		if _, ok := m.cache.Load(path); ok {
			// 先写临时文件
			tmpFile := path + ".tmp"
			defer os.Remove(tmpFile)
			if err := os.WriteFile(tmpFile, writeData, 0644); err != nil {
				logger.Errorf("memfs: write file failed %s: %v", tmpFile, err)
				continue
			}
			// 再 rename
			if err := os.Rename(tmpFile, path); err != nil {
				logger.Errorf("memfs: rename file failed %s: %v", path, err)
				continue
			}
		} else {
			logger.Errorf("memfs: already removed from cache %s", path)
		}

		// 关键：加锁检查版本，再决定是否删除缓存
		utils.WithLockKey(path, func() error {
			if current, ok := m.cache.Load(path); ok {
				if current.(*FileData).version == version {
					// 写完就删除缓存，避免 重复写 磁盘
					m.cache.Delete(path)
					logger.Infof("memfs: flushed and removed from cache %s", path)
				} else {
					logger.Infof("memfs: flushed but cache modified (v%d != v%d) %s", current.(*FileData).version, version, path)
				}
			}
			return nil
		})
	}
}

// flushAll 触发全量刷盘
func (m *MemFs) flushAll() error {
	var allPaths []string
	m.cache.Range(func(key, _ interface{}) bool {
		allPaths = append(allPaths, key.(string))
		return true
	})
	if len(allPaths) == 0 {
		return nil
	}
	select {
	case m.flushChan <- FlushCommand{Paths: allPaths}:
		return nil
	default:
		return errors.New("flush channel full")
	}
}

// flushTask 后台刷盘协程
func (m *MemFs) flushTask() {
	defer m.shutdownWG.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case cmd, ok := <-m.flushChan:
			if !ok {
				// channel closed, flush remaining and exit
				m.flushRemaining()
				return
			}
			if cmd.Shutdown {
				m.flushRemaining()
				return
			}
			m.flushPaths(cmd.Paths)
		case <-ticker.C:
			_ = m.flushAll() // 自动定期刷盘
		}
	}
}

// flushRemaining 刷盘剩余所有文件
func (m *MemFs) flushRemaining() {
	var paths []string
	m.cache.Range(func(key, _ interface{}) bool {
		paths = append(paths, key.(string))
		return true
	})
	if len(paths) > 0 {
		m.flushPaths(paths)
	}
}

// Shutdown 安全关闭
func (m *MemFs) Shutdown() error {
	if !m.shutdown.CompareAndSwap(false, true) {
		return nil
	}
	_ = m.flushAll()
	close(m.flushChan)
	m.shutdownWG.Wait()
	logger.Infof("memfs: shutdown complete")
	return nil
}
