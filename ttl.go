package forgecache

import "time"

// Exists reports whether key is stored and unexpired.
// An expired entry is removed.
func (c *Cache) Exists(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.live(key)
	return ok
}

// Expire sets the lifetime of an existing key, measured from the current clock.
// A zero TTL clears the deadline. A missing or expired key returns false.
func (c *Cache) Expire(key string, ttl time.Duration) (bool, error) {
	if ttl < 0 {
		return false, ErrNegativeTTL
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.live(key)
	if !ok {
		return false, nil
	}
	if ttl == 0 {
		e.expires = time.Time{}
	} else {
		e.expires = c.now().Add(ttl)
	}
	return true, nil
}

// TTL reports the remaining lifetime of key.
// The boolean is false when the key is missing or expired.
// A key with no deadline returns -1 and true.
func (c *Cache) TTL(key string) (time.Duration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.live(key)
	if !ok {
		return 0, false
	}
	if e.expires.IsZero() {
		return -1, true
	}
	return e.expires.Sub(c.now()), true
}

// Sweep removes expired entries and returns how many were removed.
func (c *Cache) Sweep() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	n := 0
	for e := c.head; e != nil; {
		next := e.next
		if c.expired(e, now) {
			c.remove(e)
			n++
		}
		e = next
	}
	return n
}

func (c *Cache) janitor() {
	ticker := time.NewTicker(c.janitorEvery)
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-ticker.C:
			c.Sweep()
		}
	}
}
