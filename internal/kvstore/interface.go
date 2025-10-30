package kvstore

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/mageg-x/dedupfs/internal/log"
)

var (
	// get logger instance for logging
	logger   = log.GetLogger("dedupfs")
	instance KVStore
	mutex    sync.Mutex

	// Common errors for kv store operations
	ErrKeyNotFound      = errors.New("key not found")
	ErrKeyAlreadyExists = errors.New("key already exists")
)

// KVStore is a generic type kv store interface
type KVStore interface {
	// Get retrieves value by key, deserializes result into value
	Get(key string, value interface{}) error
	// Set stores key-value pair, serializes value to json
	Set(key string, value interface{}) error
	// Del deletes specified key
	Del(key string) error
	// List lists all keys matching prefix
	List(prefix string) ([]string, error)
	// Scan scans the kv store for keys matching prefix, starting from startKey
	Scan(prefix, startKey string, limit int) (keys []string, nextKey string, err error)
	// Close closes the kv store
	Close() error
	// CreateSnapshot creates a snapshot of the kv store
	CreateSnapshot(path string) error
}

func GetKVStore(dbPath string, readOnly bool) (KVStore, error) {
	dbPath = dbPath + "/pebble"
	// 创建目录
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		logger.Errorf("failed to create directory: %v", err)
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	return GetPKVStore(dbPath, readOnly)
}
