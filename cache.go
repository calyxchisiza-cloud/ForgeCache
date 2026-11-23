// Package forgecache is an in-memory key-value store.
// Phase 2 evicts the least recently used entry when a byte limit is set.
// It does not expire values or synchronize access.
// A positive TTL is rejected until expiration exists.
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

	// ErrValueTooLarge means this entry alone exceeds the byte limit.
	ErrValueTooLarge = errors.New("entry exceeds cache capacity")
)

// Option configures a cache at creation.
type Option func(*Cache)

// WithMaxBytes limits stored key and value bytes.
// Zero or negative leaves the cache unlimited. The count is len(key)+len(value)
// for each entry. It does not measure Go's heap overhead.
func WithMaxBytes(n int) Option {
	return func(c *Cache) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// Cache stores values by key. The most recently used entry is at the head.
// It is not safe for concurrent use.
type Cache struct {
	items    map[string]*entry
	head     *entry
	tail     *entry
	bytes    int
	maxBytes int
}

type entry struct {
	key      string
	value    []byte
	created  time.Time
	accessed time.Time
	prev     *entry
	next     *entry
}

// New returns an empty cache. Without WithMaxBytes the cache does not evict.
func New(opts ...Option) *Cache {
	c := &Cache{items: make(map[string]*entry)}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Set stores a copy of value under key.
// A zero TTL means the value does not expire. A negative TTL is rejected.
// A positive TTL is rejected until expiration is implemented.
// Setting an existing key replaces its value and marks it most recently used.
// When a byte limit is set, least recently used entries are removed until the
// new entry fits. An entry larger than the limit is rejected and nothing is evicted.
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
	cloned := cloneBytes(value)
	size := entrySize(key, cloned)
	if c.maxBytes > 0 && size > c.maxBytes {
		return ErrValueTooLarge
	}
	now := time.Now()
	if e, ok := c.items[key]; ok {
		c.bytes -= entrySize(e.key, e.value)
		c.unlink(e)
		c.evictFor(size)
		e.value = cloned
		e.created = now
		e.accessed = now
		c.pushFront(e)
		c.bytes += size
		return nil
	}
	c.evictFor(size)
	e := &entry{key: key, value: cloned, created: now, accessed: now}
	c.items[key] = e
	c.pushFront(e)
	c.bytes += size
	return nil
}

// Get returns a copy of the value stored at key and marks it most recently used.
// A missing key returns nil, false.
func (c *Cache) Get(key string) ([]byte, bool) {
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e.accessed = time.Now()
	c.moveToFront(e)
	return cloneBytes(e.value), true
}

// Delete removes key. It returns false when the key was not stored.
func (c *Cache) Delete(key string) bool {
	e, ok := c.items[key]
	if !ok {
		return false
	}
	c.unlink(e)
	delete(c.items, key)
	c.bytes -= entrySize(e.key, e.value)
	return true
}

// Len returns the number of stored entries.
func (c *Cache) Len() int {
	return len(c.items)
}

// Bytes returns the estimated bytes in stored keys and values.
func (c *Cache) Bytes() int {
	return c.bytes
}

func (c *Cache) evictFor(size int) {
	if c.maxBytes <= 0 {
		return
	}
	for c.bytes+size > c.maxBytes && c.tail != nil {
		c.evictLRU()
	}
}

func (c *Cache) evictLRU() {
	e := c.tail
	if e == nil {
		return
	}
	c.unlink(e)
	delete(c.items, e.key)
	c.bytes -= entrySize(e.key, e.value)
}

func (c *Cache) pushFront(e *entry) {
	e.prev = nil
	e.next = c.head
	if c.head != nil {
		c.head.prev = e
	} else {
		c.tail = e
	}
	c.head = e
}

func (c *Cache) unlink(e *entry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else {
		c.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else {
		c.tail = e.prev
	}
	e.prev = nil
	e.next = nil
}

func (c *Cache) moveToFront(e *entry) {
	if c.head == e {
		return
	}
	c.unlink(e)
	c.pushFront(e)
}

func entrySize(key string, value []byte) int {
	return len(key) + len(value)
}

func cloneBytes(b []byte) []byte {
	out := make([]byte, len(b))
	copy(out, b)
	return out
}
