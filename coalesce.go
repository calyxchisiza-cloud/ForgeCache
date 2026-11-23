package forgecache

import "time"

// GetOrLoad returns the stored value, or calls load once when the key is missing.
// Concurrent callers for the same key share that one call. load must not call
// back into this cache. A hit moves the entry to the front, the same as Get.
func (c *Cache) GetOrLoad(key string, ttl time.Duration, load func() ([]byte, error)) ([]byte, error) {
	if load == nil {
		return nil, ErrNilLoad
	}
	if key == "" {
		return nil, ErrEmptyKey
	}
	if len(key) > MaxKeyBytes {
		return nil, ErrKeyTooLong
	}
	if ttl < 0 {
		return nil, ErrNegativeTTL
	}
	c.mu.Lock()
	if e, ok := c.live(key); ok {
		c.hits++
		e.accessed = c.now()
		c.moveToFront(e)
		val := cloneBytes(e.value)
		c.mu.Unlock()
		return val, nil
	}
	if f, ok := c.inflight[key]; ok {
		f.waiters++
		c.mu.Unlock()
		<-f.done
		if f.err != nil {
			return nil, f.err
		}
		c.mu.Lock()
		c.hits++
		c.mu.Unlock()
		return cloneBytes(f.val), nil
	}
	f := &flight{done: make(chan struct{})}
	c.inflight[key] = f
	c.misses++
	c.mu.Unlock()

	val, err := load()
	var stored []byte
	if err == nil {
		c.mu.Lock()
		stored, err = c.storeLoaded(key, val, ttl)
		c.mu.Unlock()
	}
	c.mu.Lock()
	if err == nil {
		f.val = stored
	}
	f.err = err
	delete(c.inflight, key)
	c.mu.Unlock()
	close(f.done)
	if err != nil {
		return nil, err
	}
	return cloneBytes(stored), nil
}

// storeLoaded writes a value produced by GetOrLoad. The caller holds c.mu.
func (c *Cache) storeLoaded(key string, value []byte, ttl time.Duration) ([]byte, error) {
	cloned := cloneBytes(value)
	size := entrySize(key, cloned)
	if c.maxBytes > 0 && size > c.maxBytes {
		return nil, ErrValueTooLarge
	}
	now := c.now()
	var expires time.Time
	if ttl > 0 {
		expires = now.Add(ttl)
	}
	if err := c.appendRecord(opSet, key, cloned, expires); err != nil {
		return nil, err
	}
	c.put(key, cloned, now, expires)
	return cloned, nil
}
