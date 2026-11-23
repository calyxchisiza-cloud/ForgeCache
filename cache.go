// Package forgecache is an in-memory key-value store.
// It expires values, evicts the least recently used entry, lets goroutines
// share a cache, and can restore values from a snapshot and a log.
// One mutex covers the map, the recency list, and the byte count together.
// NewSharded gives each shard its own mutex.
package forgecache

import (
	"errors"
	"os"
	"sync"
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

	// ErrValueTooLarge means this entry alone exceeds the byte limit.
	ErrValueTooLarge = errors.New("entry exceeds cache capacity")

	// ErrNoPersistence means Snapshot was called on a memory-only cache.
	ErrNoPersistence = errors.New("cache is not opened on a directory")

	// ErrCorruptLog means a complete record failed its checksum.
	ErrCorruptLog = errors.New("cache log is corrupt")

	// ErrValueUnloggable means the value is too large to store in one record.
	ErrValueUnloggable = errors.New("value exceeds the persistence record limit")

	// ErrNilLoad means GetOrLoad was called without a load function.
	ErrNilLoad = errors.New("load function is nil")
)

// Clock supplies the instant used for expiration.
// Now may be called concurrently and must not call back into the cache.
type Clock interface {
	Now() time.Time
}

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

// WithClock selects the clock used for expiration deadlines.
// The default clock is time.Now.
func WithClock(clock Clock) Option {
	return func(c *Cache) {
		if clock != nil {
			c.clock = clock
		}
	}
}

// WithJanitor sets how often the background pass removes expired entries.
// Zero or negative disables the pass. The default is one second.
// Access checks still expire entries when the pass is disabled.
func WithJanitor(interval time.Duration) Option {
	return func(c *Cache) {
		if interval > 0 {
			c.janitorEvery = interval
		} else {
			c.janitorEvery = 0
		}
	}
}

// Cache stores values by key. The most recently used entry is at the head.
//
// Every method takes mu for the whole operation. Get cannot use a shared lock:
// it moves the entry and may delete an expired one. That keeps the map and the
// list in agreement, and it also means every caller waits on this one lock.
// Splitting the lock waits until a benchmark shows that wait is the limit.
type Cache struct {
	mu           sync.Mutex
	items        map[string]*entry
	head         *entry
	tail         *entry
	bytes        int
	maxBytes     int
	clock        Clock
	janitorEvery time.Duration
	stop         chan struct{}
	closeOnce    sync.Once
	dir          string
	aof          *os.File
	seq          uint64
	syncWrites   bool
	started      time.Time
	hits         uint64
	misses       uint64
	evictions    uint64
	expirations  uint64
	ops          uint64
	samples      [sampleCap]time.Duration
	sampleN      int
	sampleAt     int
	inflight     map[string]*flight
}

type entry struct {
	key      string
	value    []byte
	created  time.Time
	accessed time.Time
	expires  time.Time
	prev     *entry
	next     *entry
}

// New returns an empty cache. Without WithMaxBytes the cache does not evict.
// A background pass removes expired entries about once a second.
func New(opts ...Option) *Cache {
	c := &Cache{
		items:        make(map[string]*entry),
		janitorEvery: time.Second,
		stop:         make(chan struct{}),
		syncWrites:   true,
		started:      time.Now(),
		inflight:     make(map[string]*flight),
	}
	for _, opt := range opts {
		opt(c)
	}
	if c.janitorEvery > 0 {
		go c.janitor()
	}
	return c
}

// Close stops the background expiration pass and closes the log file.
// Stored values remain readable. Later mutations stay in memory only.
func (c *Cache) Close() {
	c.closeOnce.Do(func() {
		close(c.stop)
		c.mu.Lock()
		if c.aof != nil {
			_ = c.aof.Sync()
			_ = c.aof.Close()
			c.aof = nil
		}
		c.mu.Unlock()
	})
}

// Set stores a copy of value under key.
// A zero TTL means the value does not expire. A negative TTL is rejected.
// A positive TTL expires at the current clock plus that duration.
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
	cloned := cloneBytes(value)
	size := entrySize(key, cloned)
	start := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.observe(start)
	if c.maxBytes > 0 && size > c.maxBytes {
		return ErrValueTooLarge
	}
	now := c.now()
	var expires time.Time
	if ttl > 0 {
		expires = now.Add(ttl)
	}
	if err := c.appendRecord(opSet, key, cloned, expires); err != nil {
		return err
	}
	c.put(key, cloned, now, expires)
	return nil
}

// put stores a value that has already been logged. The caller holds c.mu.
func (c *Cache) put(key string, cloned []byte, now time.Time, expires time.Time) {
	size := entrySize(key, cloned)
	if e, ok := c.items[key]; ok {
		if c.expired(e, now) {
			c.dropExpired(e)
		} else {
			c.bytes -= entrySize(e.key, e.value)
			c.unlink(e)
			c.evictFor(size)
			e.value = cloned
			e.created = now
			e.accessed = now
			e.expires = expires
			c.pushFront(e)
			c.bytes += size
			return
		}
	}
	c.evictFor(size)
	e := &entry{
		key:      key,
		value:    cloned,
		created:  now,
		accessed: now,
		expires:  expires,
	}
	c.items[key] = e
	c.pushFront(e)
	c.bytes += size
}

// Get returns a copy of the value stored at key and marks it most recently used.
// A missing or expired key returns nil, false. An expired entry is removed.
func (c *Cache) Get(key string) ([]byte, bool) {
	start := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.observe(start)
	e, ok := c.live(key)
	if !ok {
		c.misses++
		return nil, false
	}
	c.hits++
	e.accessed = c.now()
	c.moveToFront(e)
	return cloneBytes(e.value), true
}

// Delete removes key.
// It returns false when the key is missing or expired.
// A log failure leaves the key in place and returns the error.
func (c *Cache) Delete(key string) (bool, error) {
	start := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	defer c.observe(start)
	e, ok := c.items[key]
	if !ok {
		return false, nil
	}
	if c.expired(e, c.now()) {
		c.dropExpired(e)
		return false, nil
	}
	if err := c.appendRecord(opDelete, key, nil, time.Time{}); err != nil {
		return false, err
	}
	c.remove(e)
	return true, nil
}

// Clear removes every entry.
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.appendRecord(opClear, "", nil, time.Time{}); err != nil {
		return err
	}
	c.items = make(map[string]*entry)
	c.head = nil
	c.tail = nil
	c.bytes = 0
	return nil
}

// Len returns the number of stored entries, including ones not yet swept.
func (c *Cache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// Bytes returns the estimated bytes in stored keys and values.
func (c *Cache) Bytes() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.bytes
}

func (c *Cache) now() time.Time {
	if c.clock == nil {
		return time.Now()
	}
	return c.clock.Now()
}

func (c *Cache) expired(e *entry, now time.Time) bool {
	return !e.expires.IsZero() && !now.Before(e.expires)
}

// live returns the entry when it is present and unexpired.
// An expired entry is removed. The caller holds c.mu.
func (c *Cache) live(key string) (*entry, bool) {
	e, ok := c.items[key]
	if !ok {
		return nil, false
	}
	if c.expired(e, c.now()) {
		c.dropExpired(e)
		return nil, false
	}
	return e, true
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
	c.remove(e)
	c.evictions++
}

func (c *Cache) dropExpired(e *entry) {
	c.remove(e)
	c.expirations++
}

func (c *Cache) remove(e *entry) {
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
