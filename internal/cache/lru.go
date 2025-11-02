package cache

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mageg-x/dedupfs/internal/log"
)

var (
	// 获取logger实例用于输出日志，与mount包保持一致的名称
	logger = log.GetLogger("dedupfs")
)

// CacheItem 定义缓存项必须实现的接口
// Len() 返回该项占用的字节数（或任意单位的“大小”）
type CacheItem interface {
	Len() int
}

// entry 封装缓存值、访问时间和大小
type entry[V CacheItem] struct {
	value      V
	accessedAt time.Time
	size       int
}

// Cache 是一个可配置的缓存，支持 LRU 驱逐或普通 map 模式
type Cache[V CacheItem] struct {
	capacity    int
	autotrim    bool          // 是否自动裁剪（启用淘汰策略）
	currentSize atomic.Uint64 // 当前总大小（单位与 Len() 一致）
	mu          sync.RWMutex
	data        map[string]*entry[V] // 使用普通 map + RWMutex 实现线程安全，key固定为string类型
}

// NewCache 创建新缓存
// capacity: 缓存容量
// autotrim: 是否启用自动裁剪（淘汰策略），false时退化为普通线程安全map
func NewCache[V CacheItem](capacity int, autotrim bool) *Cache[V] {
	return &Cache[V]{
		capacity: capacity,
		autotrim: autotrim,
		data:     make(map[string]*entry[V]),
	}
}

// Get 获取缓存项，若存在则更新访问时间
func (c *Cache[V]) Get(key string) (V, bool) {
	c.mu.RLock()
	e, ok := c.data[key]
	if ok {
		// 为避免写锁竞争，先释放读锁，再加写锁更新时间（或采用 copy-on-read 策略）
		c.mu.RUnlock()

		c.mu.Lock()
		// 再次检查，防止并发删除
		if e2, ok2 := c.data[key]; ok2 {
			e2.accessedAt = time.Now()
		}
		c.mu.Unlock()

		return e.value, true
	}
	c.mu.RUnlock()
	var zero V
	return zero, false
}

// Put 插入缓存项
// 注意：Go 泛型无法像 Rust 那样传入 size_calculator 闭包（因为 V 已约束为 CacheItem）
// 所以直接调用 value.Len()
func (c *Cache[V]) Put(key string, value V) {
	itemSize := value.Len()

	c.mu.Lock()
	defer c.mu.Unlock()

	// 如果启用了自动裁剪，检查容量限制
	if c.autotrim {
		if itemSize > c.capacity {
			logger.Infof("cache item size %d exceeds cache capacity %d", itemSize, c.capacity)
			return
		}
	}

	// 如果 key 已存在，先减去旧大小
	oldSize := 0
	if oldEntry, exists := c.data[key]; exists {
		oldSize = oldEntry.size
	}

	// 只有在启用自动裁剪时才执行shrink
	if c.autotrim {
		// 确保有足够空间（可能触发驱逐）
		c.shrink(itemSize - oldSize)
	}

	// 插入新项
	c.data[key] = &entry[V]{
		value:      value,
		accessedAt: time.Now(),
		size:       itemSize,
	}

	// 更新总大小
	newSize := int(c.currentSize.Load()) - oldSize + itemSize
	c.currentSize.Store(uint64(newSize))
}

// Del 删除缓存项
func (c *Cache[V]) Del(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.data[key]; ok {
		delete(c.data, key)
		newSize := int(c.currentSize.Load()) - e.size
		c.currentSize.Store(uint64(newSize))
		logger.Infof("cache item deleted, size reduced by %d", e.size)
	}
}

// shrink 在持有写锁的前提下，驱逐最久未使用的项，直到有足够空间
// requiredDelta 可能为负（替换时）
func (c *Cache[V]) shrink(requiredDelta int) {
	// 只有启用自动裁剪时才执行淘汰逻辑
	if !c.autotrim {
		return
	}

	if requiredDelta <= 0 {
		return // 替换旧项，空间只会变多或不变
	}

	current := int(c.currentSize.Load())
	needed := current + requiredDelta
	if needed <= c.capacity {
		return // 空间足够
	}

	// 构建访问时间列表
	type kv struct {
		key   string
		time  time.Time
		entry *entry[V]
	}
	var entries []kv
	for k, v := range c.data {
		entries = append(entries, kv{key: k, time: v.accessedAt, entry: v})
	}

	// 按访问时间升序（最旧在前）
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].time.Before(entries[j].time)
	})

	toFree := needed - c.capacity // 至少要释放这么多空间
	freed := 0
	removed := 0

	for i := 0; i < len(entries) && freed < toFree; i++ {
		k := entries[i].key
		e := entries[i].entry
		delete(c.data, k)
		freed += e.size
		removed++
	}

	// 更新总大小
	newSize := current - freed
	c.currentSize.Store(uint64(newSize))

	if removed > 0 {
		logger.Infof("cache cleanup completed: removed %d items, freed %d bytes", removed, freed)
	}
}

// Len 返回当前缓存总大小
func (c *Cache[V]) Len() int {
	return int(c.currentSize.Load())
}

// Count 返回缓存项数量
func (c *Cache[V]) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.data)
}

func (c *Cache[V]) Clear(keyPrefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.data {
		if strings.HasPrefix(k, keyPrefix) {
			delete(c.data, k)
		}
	}
}
