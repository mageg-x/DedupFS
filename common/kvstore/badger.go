package kvstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
)

// BKV is badgerdb based kvstore implementation
type BKV struct {
	db       *badger.DB
	closed   bool
	mutex    sync.RWMutex
	notifier Notifier
}

// NewBKV creates a new badgerdb store instance
func NewBKVStore(dbPath string, readOnly bool, notifier Notifier) (KVStore, error) {
	logger.Debugf("initializing badgerdb kv store at path: %s", dbPath)

	opts := badger.DefaultOptions(dbPath)
	opts.Logger = nil       // disable internal logger
	opts.SyncWrites = false // keep async writes

	// memory optimization
	opts.MemTableSize = 512 << 20     // 512MB - large memtable
	opts.NumMemtables = 10            // 10 memtables
	opts.NumLevelZeroTables = 20      // delay L0 compression
	opts.NumLevelZeroTablesStall = 40 // higher stall threshold

	// cache optimization
	opts.BlockCacheSize = 1024 << 20 // 1GB block cache
	opts.IndexCacheSize = 512 << 20  // 512MB index cache

	// lsm optimization
	opts.BaseTableSize = 16 << 20  // 16MB base table size
	opts.BaseLevelSize = 256 << 20 // 256MB base level size
	opts.LevelSizeMultiplier = 20  // reduce compression frequency

	// value storage optimization
	opts.ValueThreshold = 1024 // 1KB threshold
	opts.VLogPercentile = 0.99 // 99% dynamic threshold
	opts.NumCompactors = 2     // minimize background compression

	// performance optimization
	opts.Compression = options.ZSTD
	opts.ZSTDCompressionLevel = 1
	opts.VerifyValueChecksum = false // disable checksum for performance

	opts.ReadOnly = readOnly
	db, err := badger.Open(opts)
	if err != nil {
		logger.Errorf("failed to open badger db: %v", err)
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	logger.Info("badgerdb kv store initialized successfully")
	return &BKV{
		db:       db,
		closed:   false,
		notifier: notifier,
	}, nil
}

// Get retrieves value by key
func (s *BKV) Get(key string, value interface{}) error {
	if err := s.checkClosed(); err != nil {
		logger.Debug("get operation failed: store closed")
		return err
	}

	logger.Debugf("getting value for key: %s", key)

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	return s.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte(key))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				logger.Debugf("key not found: %s", key)
				return ErrKeyNotFound
			}
			return err
		}

		err = item.Value(func(val []byte) error {
			return json.Unmarshal(val, value)
		})

		if err != nil {
			logger.Errorf("failed to unmarshal value for key %s: %v", key, err)
			return fmt.Errorf("failed to unmarshal value: %w", err)
		}

		logger.Debugf("successfully retrieved value for key: %s", key)
		if s.notifier != nil {
			s.notifier("get", key, value)
		}
		return nil
	})
}

// Set stores key-value pair
func (s *BKV) Set(key string, value interface{}) error {
	if err := s.checkClosed(); err != nil {
		logger.Debug("set operation failed: store closed")
		return err
	}

	logger.Debugf("setting value for key: %s", key)

	data, err := json.Marshal(value)
	if err != nil {
		logger.Errorf("failed to marshal value for key %s: %v", key, err)
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	logger.Debugf("successfully set value for key: %s", key)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	err = s.db.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte(key), data)
	})

	if err != nil {
		logger.Errorf("failed to set value for key %s: %v", key, err)
		return fmt.Errorf("failed to set value: %w", err)
	}
	if s.notifier != nil {
		s.notifier("set", key, value)
	}
	return nil
}

// Del deletes specified key
func (s *BKV) Del(key string) error {
	if err := s.checkClosed(); err != nil {
		logger.Debug("delete operation failed: store closed")
		return err
	}

	logger.Debugf("deleting key: %s", key)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	err := s.db.Update(func(txn *badger.Txn) error {
		return txn.Delete([]byte(key))
	})
	if err != nil {
		logger.Errorf("failed to delete key %s: %v", key, err)
		return fmt.Errorf("failed to delete key: %w", err)
	}
	if s.notifier != nil {
		s.notifier("del", key, nil)
	}
	logger.Debugf("successfully deleted key: %s", key)
	return nil
}

// List lists all keys matching prefix
func (s *BKV) List(prefix string) ([]string, error) {
	if err := s.checkClosed(); err != nil {
		logger.Debug("list operation failed: store closed")
		return nil, err
	}

	logger.Debugf("listing keys with prefix: %s", prefix)

	var keys []string

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	err := s.db.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefixBytes := []byte(prefix)
		for it.Seek(prefixBytes); it.ValidForPrefix(prefixBytes); it.Next() {
			item := it.Item()
			key := string(item.Key())
			keys = append(keys, key)
		}

		return nil
	})

	if err != nil {
		logger.Errorf("failed to list keys with prefix %s: %v", prefix, err)
		return nil, fmt.Errorf("failed to list keys: %w", err)
	}

	logger.Debugf("successfully listed %d keys with prefix: %s", len(keys), prefix)
	return keys, nil
}

// Scan scans keys with given prefix, starting from startKey (inclusive), up to limit keys.
// Returns the matched keys and the next key for pagination (empty if no more).
// If startKey is empty, scanning starts from the first key with the prefix.
func (s *BKV) Scan(prefix, startKey string, limit int) ([]string, string, error) {
	if err := s.checkClosed(); err != nil {
		logger.Debug("scan operation failed: store closed")
		return nil, "", err
	}

	if limit <= 0 {
		return []string{}, "", nil
	}

	logger.Debugf("scanning keys with prefix: %s, startKey: %s, limit: %d", prefix, startKey, limit)

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var keys []string
	var nextKey string

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // 只要 key，不要 value
		opts.PrefetchSize = 1000

		it := txn.NewIterator(opts)
		defer it.Close()

		prefixBytes := []byte(prefix)
		seekKey := []byte(startKey)
		if startKey == "" {
			seekKey = prefixBytes
		}

		it.Seek(seekKey)

		count := 0
		prefixLen := len(prefixBytes)

		for it.Valid() && count < limit {
			item := it.Item()
			key := item.Key()

			// 检查是否仍在 prefix 范围内
			if len(key) < prefixLen || !bytes.Equal(key[:prefixLen], prefixBytes) {
				break
			}

			keys = append(keys, string(key))
			count++
			it.Next()
		}

		// 获取 nextKey（用于下一页）
		if it.Valid() {
			next := it.Item().Key()
			// 确保 next 仍在 prefix 范围内才返回，否则返回空（表示结束）
			if len(next) >= prefixLen && bytes.Equal(next[:prefixLen], prefixBytes) {
				nextKey = string(next)
			}
		}

		return nil
	})

	if err != nil {
		logger.Errorf("failed to scan keys with prefix %s: %v", prefix, err)
		return nil, "", fmt.Errorf("scan failed: %w", err)
	}

	logger.Debugf("scan returned %d keys, nextKey: %s", len(keys), nextKey)
	return keys, nextKey, nil
}

// CountByPrefix counts the number of keys matching the given prefix
func (s *BKV) CountByPrefix(prefix string) (int, error) {
	if err := s.checkClosed(); err != nil {
		logger.Debug("count operation failed: store closed")
		return 0, fmt.Errorf("count %s: %w", prefix, err)
	}

	logger.Debugf("counting keys with prefix: %s", prefix)

	var count int

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	err := s.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // 只需要计算数量，不需要值

		it := txn.NewIterator(opts)
		defer it.Close()

		prefixBytes := []byte(prefix)
		for it.Seek(prefixBytes); it.ValidForPrefix(prefixBytes); it.Next() {
			count++
		}

		return nil
	})

	if err != nil {
		logger.Errorf("failed to count keys with prefix %s: %v", prefix, err)
		return 0, fmt.Errorf("count %s: iteration: %w", prefix, err)
	}

	logger.Debugf("counted %d keys with prefix: %s", count, prefix)
	return count, nil
}

func (s *BKV) ClearAll() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return s.db.DropAll()
}

// Close closes the kv store
func (s *BKV) Close() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.closed {
		logger.Debug("close operation failed: store already closed")
		return errors.New("store already closed")
	}

	logger.Info("closing badgerdb kv store")

	if err := s.db.Close(); err != nil {
		logger.Errorf("failed to close badger db: %v", err)
		return fmt.Errorf("failed to close badger db: %w", err)
	}

	logger.Info("badgerdb kv store closed successfully")

	s.closed = true
	return nil
}

// checkClosed checks if store is closed
func (s *BKV) checkClosed() error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if s.closed {
		return errors.New("store is closed")
	}

	return nil
}
