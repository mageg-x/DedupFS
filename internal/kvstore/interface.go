package kvstore

import (
	"errors"
	"fmt"
	"os"

	"github.com/mageg-x/dedupfs/internal/log"
)

var (
	// get logger instance for logging
	logger = log.GetLogger("dedupfs")

	// Common errors for kv store operations
	ErrKeyNotFound      = errors.New("key not found")
	ErrKeyAlreadyExists = errors.New("key already exists")
)

type Notifier func(op, key string, value interface{})

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
	// CountByPrefix counts the number of keys matching the given prefix
	CountByPrefix(prefix string) (int, error)
	// Close closes the kv store
	Close() error
}

func NewKVStore(dbPath string, readOnly bool, notifier Notifier) (KVStore, error) {
	// 创建目录
	if err := os.MkdirAll(dbPath, 0755); err != nil {
		logger.Errorf("failed to create directory: %v", err)
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	return NewPKVStore(dbPath, readOnly, notifier)
}
