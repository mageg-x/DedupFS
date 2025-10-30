package kvstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/cockroachdb/pebble"
)

// PKV is pebble based kvstore implementation
type PKV struct {
	db     *pebble.DB
	closed bool
	mutex  sync.RWMutex
}

// GetPKVStore creates a new pebble store instance
func GetPKVStore(dbPath string, readOnly bool) (KVStore, error) {
	mutex.Lock()
	defer mutex.Unlock()

	if instance != nil && !instance.(*PKV).closed {
		logger.Debug("kv store already initialized")
		return instance, nil
	}

	logger.Debugf("initializing pebble kv store at path: %s", dbPath)

	pebbleOpts := &pebble.Options{
		// Memory optimization
		MemTableSize:                uint64(512 << 20), // 512MB
		MemTableStopWritesThreshold: 4,

		// Cache optimization
		Cache: pebble.NewCache(1 << 30), // 1GB

		// LSM optimization
		L0CompactionThreshold: 20,
		L0StopWritesThreshold: 40,
		LBaseMaxBytes:         256 << 20, // 256MB
		Levels: []pebble.LevelOptions{
			{TargetFileSize: 16 << 20, Compression: pebble.ZstdCompression},
			{Compression: pebble.ZstdCompression},
			{Compression: pebble.ZstdCompression},
		},

		// Performance optimization
		DisableWAL:            false,
		FlushDelayDeleteRange: 0,
		FlushDelayRangeKey:    0,
		MaxOpenFiles:          1000,

		// ReadOnly mode
		ReadOnly: readOnly,
	}

	// Ensure compression is set for all levels
	for i := range pebbleOpts.Levels {
		pebbleOpts.Levels[i].Compression = pebble.ZstdCompression
	}

	db, err := pebble.Open(dbPath, pebbleOpts)
	if err != nil {
		logger.Errorf("failed to open pebble db: %v", err)
		return nil, fmt.Errorf("failed to open pebble db: %w", err)
	}

	logger.Info("pebble kv store initialized successfully")

	instance = &PKV{
		db:     db,
		closed: false,
	}
	return instance, nil
}

// Get retrieves value by key
func (s *PKV) Get(key string, value interface{}) error {
	if err := s.checkClosed(); err != nil {
		logger.Debug("get operation failed: store closed")
		return fmt.Errorf("get %s: %w", key, err)
	}

	logger.Debugf("getting value for key: %s", key)

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	data, closer, err := s.db.Get([]byte(key))
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			logger.Debugf("key not found: %s", key)
			return fmt.Errorf("get %s: %w", key, ErrKeyNotFound)
		}
		logger.Errorf("failed to get value for key %s: %v", key, err)
		return fmt.Errorf("get %s: %w", key, err)
	}
	defer closer.Close()

	err = json.Unmarshal(data, value)
	if err != nil {
		logger.Errorf("failed to unmarshal value for key %s: %v", key, err)
		return fmt.Errorf("get %s: unmarshal: %w", key, err)
	}

	logger.Debugf("successfully retrieved value for key: %s", key)
	return nil
}

// Set stores key-value pair
func (s *PKV) Set(key string, value interface{}) error {
	if err := s.checkClosed(); err != nil {
		logger.Debug("set operation failed: store closed")
		return fmt.Errorf("set %s: %w", key, err)
	}

	logger.Debugf("setting value for key: %s", key)

	data, err := json.Marshal(value)
	if err != nil {
		logger.Errorf("failed to marshal value for key %s: %v", key, err)
		return fmt.Errorf("set %s: marshal: %w", key, err)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if err := s.db.Set([]byte(key), data, pebble.Sync); err != nil {
		logger.Errorf("failed to write value for key %s: %v", key, err)
		return fmt.Errorf("set %s: db write: %w", key, err)
	}

	logger.Debugf("successfully set value for key: %s", key)
	return nil
}

// Del deletes specified key
func (s *PKV) Del(key string) error {
	if err := s.checkClosed(); err != nil {
		logger.Debug("delete operation failed: store closed")
		return fmt.Errorf("delete %s: %w", key, err)
	}

	logger.Debugf("deleting key: %s", key)

	s.mutex.Lock()
	defer s.mutex.Unlock()

	if err := s.db.Delete([]byte(key), pebble.Sync); err != nil {
		logger.Errorf("failed to delete key %s: %v", key, err)
		return fmt.Errorf("delete %s: %w", key, err)
	}

	logger.Debugf("successfully deleted key: %s", key)
	return nil
}

// List lists all keys matching prefix
func (s *PKV) List(prefix string) ([]string, error) {
	if err := s.checkClosed(); err != nil {
		logger.Debug("list operation failed: store closed")
		return nil, fmt.Errorf("list %s: %w", prefix, err)
	}

	logger.Debugf("listing keys with prefix: %s", prefix)

	var keys []string

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: getUpperBound([]byte(prefix)),
	})
	if err != nil {
		logger.Errorf("failed to list keys with prefix %s: %v", prefix, err)
		return nil, fmt.Errorf("list %s: create iterator: %w", prefix, err)
	}
	defer iter.Close()

	prefixBytes := []byte(prefix)
	for iter.SeekGE([]byte(prefix)); iter.Valid(); iter.Next() {
		key := iter.Key()
		if !bytes.HasPrefix(key, prefixBytes) {
			break
		}
		keys = append(keys, string(key))
	}

	if err := iter.Error(); err != nil {
		logger.Errorf("failed to list keys with prefix %s: %v", prefix, err)
		return nil, fmt.Errorf("list %s: iteration: %w", prefix, err)
	}

	logger.Debugf("successfully listed %d keys with prefix: %s", len(keys), prefix)
	return keys, nil
}

// Scan scans keys with given prefix, starting from startKey (inclusive), up to limit keys.
// Returns the matched keys and the next key for pagination (empty if no more).
// If startKey is empty, scanning starts from the first key with the prefix.
func (s *PKV) Scan(prefix, startKey string, limit int) ([]string, string, error) {
	if err := s.checkClosed(); err != nil {
		logger.Debug("scan operation failed: store closed")
		return nil, "", fmt.Errorf("scan %s: %w", prefix, err)
	}

	if limit <= 0 {
		return []string{}, "", nil
	}

	logger.Debugf("scanning keys with prefix: %s, startKey: %s, limit: %d", prefix, startKey, limit)

	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var keys []string
	var nextKey string

	// Determine the start key for iteration
	start := []byte(prefix)
	if startKey != "" && bytes.HasPrefix([]byte(startKey), []byte(prefix)) {
		start = []byte(startKey)
	}

	iter, err := s.db.NewIter(&pebble.IterOptions{
		LowerBound: []byte(prefix),
		UpperBound: getUpperBound([]byte(prefix)),
	})
	if err != nil {
		logger.Errorf("failed to create iterator for scan: %v", err)
		return nil, "", fmt.Errorf("scan %s: create iterator: %w", prefix, err)
	}
	defer iter.Close()

	prefixBytes := []byte(prefix)
	count := 0

	// Seek to start position and iterate
	for iter.SeekGE(start); iter.Valid() && count < limit; iter.Next() {
		key := iter.Key()

		// Check if still in prefix range
		if !bytes.HasPrefix(key, prefixBytes) {
			break
		}

		keys = append(keys, string(key))
		count++
	}

	// Get nextKey for pagination if there are more results
	if iter.Valid() {
		next := iter.Key()
		// Only return nextKey if it's still in prefix range
		if bytes.HasPrefix(next, prefixBytes) {
			nextKey = string(next)
		}
	}

	if err := iter.Error(); err != nil {
		logger.Errorf("failed to scan keys with prefix %s: %v", prefix, err)
		return nil, "", fmt.Errorf("scan %s: iteration: %w", prefix, err)
	}

	logger.Debugf("scan returned %d keys, nextKey: %s", len(keys), nextKey)
	return keys, nextKey, nil
}

// CreateSnapshot creates a snapshot of the kv store in a separate goroutine
func (s *PKV) CreateSnapshot(path string) error {
	// 首先检查store是否关闭，但不持有锁
	if err := s.checkClosed(); err != nil {
		logger.Debug("create snapshot operation failed: store closed")
		return fmt.Errorf("create snapshot: %w", err)
	}

	logger.Infof("starting snapshot creation for pebble kv store to path: %s", path)

	// 存储db引用，避免在goroutine中直接访问s.db（可能被修改）
	var db *pebble.DB
	s.mutex.RLock()
	db = s.db
	s.mutex.RUnlock()

	// Create a channel to receive the snapshot result
	done := make(chan error, 1)

	// Start a new goroutine to create the snapshot
	go func(db *pebble.DB) {
		// Create directory if it doesn't exist
		if err := os.MkdirAll(path, 0755); err != nil {
			logger.Errorf("failed to create snapshot directory: %v", err)
			done <- fmt.Errorf("failed to create snapshot directory: %w", err)
			return
		}

		snapshotPath := path + "/snapshot"
		if err := os.MkdirAll(snapshotPath, 0755); err != nil {
			logger.Errorf("failed to create snapshot subdirectory: %v", err)
			done <- fmt.Errorf("failed to create snapshot subdirectory: %w", err)
			return
		}

		// For Pebble, we'll use the Checkpoint functionality
		err := db.Checkpoint(snapshotPath, nil)
		if err != nil {
			logger.Errorf("failed to create pebble snapshot: %v", err)
			done <- fmt.Errorf("failed to create pebble snapshot: %w", err)
			return
		}

		logger.Infof("snapshot created successfully at: %s", path)
		done <- nil
	}(db)

	// Wait for the snapshot operation to complete
	return <-done
}

// Close closes the kv store
func (s *PKV) Close() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	if s.closed {
		logger.Debug("close operation failed: store already closed")
		return errors.New("store already closed")
	}

	logger.Info("closing pebble kv store")

	if err := s.db.Close(); err != nil {
		logger.Errorf("failed to close pebble db: %v", err)
		return fmt.Errorf("failed to close pebble db: %w", err)
	}

	logger.Info("pebble kv store closed successfully")

	s.closed = true

	// Clear the global instance
	mutex.Lock()
	defer mutex.Unlock()
	instance = nil

	return nil
}

// checkClosed checks if store is closed
func (s *PKV) checkClosed() error {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	if s.closed {
		logger.Errorf("operation failed: store closed")
		return errors.New("store is closed")
	}

	return nil
}

// getUpperBound returns the upper bound for prefix iteration
// This is a safer version of the original incrementBytes function
func getUpperBound(prefix []byte) []byte {
	if len(prefix) == 0 {
		return nil // No upper bound for empty prefix
	}

	// Create a copy to avoid modifying the original
	result := make([]byte, len(prefix))
	copy(result, prefix)

	// Increment the last byte, handling carry-over
	for i := len(result) - 1; i >= 0; i-- {
		if result[i] < 0xFF {
			result[i]++
			return result[:i+1] // Truncate after the incremented byte
		}
	}

	// If all bytes were 0xFF, return nil (no upper bound)
	return nil
}
