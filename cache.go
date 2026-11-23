// Package forgecache is an in-memory key-value store.
// Phase 1 stores, retrieves, and deletes values. It does not evict, expire,
// or synchronize access. A positive TTL is rejected until expiration exists.
package forgecache

import (
	"errors"
	"time"
)

// MaxKeyBytes is the largest accepted key, measured in bytes.
const MaxKeyBytes = 1024

var (
	// ErrEmptyKey means Set was called with an empty key.
	ErrEmptyKey = errors.New("cache key is empty")

	// ErrKeyTooLong means the key is longer than MaxKeyBytes.
	ErrKeyTooLong = errors.New("cache key exceeds 1024 bytes")

	// ErrNegativeTTL means the caller passed a negative duration.
	ErrNegativeTTL = errors.New("ttl must not be negative")

	// ErrTTLUnsupported means the caller asked for expiration.
	// A zero TTL stores the value with no expiration.
	ErrTTLUnsupported = errors.New("ttl expiration is not implemented")
)

// Cache stores values by key. It is not safe for concurrent use.
type Cache struct {
	items map[string]entry
}

type entry struct {
	value    []byte
	created  time.Time
	accessed time.Time
}

// New returns an empty cache.
func New() *Cache {
	return &Cache{items: make(map[string]entry)}
}

// Set stores a copy of value under key.
// A zero TTL means the value does not expire. A negative TTL is rejected.
// A positive TTL is rejected until expiration is implemented.
// Setting an existing key replaces its value.
func (c *Cache) Set(key string, value []byte, ttl time.Duration) error {
	if key == "" {
		return ErrEmptyKey
	}
	if len(key) > MaxKeyBytes {
		return ErrKeyTooLong
	}
	if ttl < 0 {
		return ErrNegativeTTL
	}
	if ttl > 0 {
		return ErrTTLUnsupported
	}
	now := time.Now()
	c.items[key] = entry{
		value:    cloneBytes(value),
		created:  now,
		accessed: now,
	}
	return nil
}

// Get returns a copy of the value stored at key.
// A missing key returns nil, false.
func (c *Cache) Get(key string) ([]byte, bool) {
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e.accessed = time.Now()
	c.items[key] = e
	return cloneBytes(e.value), true
}

// Delete removes key. It returns false when the key was not stored.
func (c *Cache) Delete(key string) bool {
	if _, ok := c.items[key]; !ok {
		return false
	}
	delete(c.items, key)
	return true
}

func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
