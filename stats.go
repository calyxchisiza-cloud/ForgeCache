package forgecache

import (
	"math"
	"sort"
	"time"
)

const sampleCap = 256

// flight is one shared load for a key.
type flight struct {
	done    chan struct{}
	waiters int
	val     []byte
	err     error
}

// Stats is a point-in-time view of the cache.
// Keys and Bytes include entries the janitor has not removed yet.
// OpsPerSec is the average since the cache was created, using the wall clock.
// P95 and P99 come from the most recent samples, at most 256 per cache.
type Stats struct {
	Hits        uint64
	Misses      uint64
	HitRatio    float64
	Evictions   uint64
	Expirations uint64
	Keys        int
	Bytes       int
	OpsPerSec   float64
	P95         time.Duration
	P99         time.Duration
}

// Stats returns counters and recent latencies for Set, Get, and Delete.
func (c *Cache) Stats() Stats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.statsLocked()
}

func (c *Cache) statsLocked() Stats {
	samples := append([]time.Duration(nil), c.samples[:c.sampleN]...)
	st := Stats{
		Hits:        c.hits,
		Misses:      c.misses,
		Evictions:   c.evictions,
		Expirations: c.expirations,
		Keys:        len(c.items),
		Bytes:       c.bytes,
		P95:         percentile(samples, 95),
		P99:         percentile(samples, 99),
	}
	if total := st.Hits + st.Misses; total > 0 {
		st.HitRatio = float64(st.Hits) / float64(total)
	}
	if c.ops > 0 {
		st.OpsPerSec = float64(c.ops) / rateWindow(c.started).Seconds()
	}
	return st
}

func rateWindow(started time.Time) time.Duration {
	elapsed := time.Since(started)
	if elapsed < time.Nanosecond {
		return time.Nanosecond
	}
	return elapsed
}

func (c *Cache) observe(start time.Time) {
	c.ops++
	c.samples[c.sampleAt] = time.Since(start)
	c.sampleAt = (c.sampleAt + 1) % len(c.samples)
	if c.sampleN < len(c.samples) {
		c.sampleN++
	}
}

func percentile(samples []time.Duration, p float64) time.Duration {
	if len(samples) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), samples...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	idx := int(math.Ceil(p/100*float64(len(ordered)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(ordered) {
		idx = len(ordered) - 1
	}
	return ordered[idx]
}
