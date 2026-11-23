package forgecache

import "time"

// Sharded is a set of independent caches. Each shard has its own lock and its
// own recency list, so an eviction in one shard does not remove a key in another.
// WithMaxBytes is divided across the shards. A shard is never given a larger
// limit than the total. When the limit is smaller than the shard count, the
// count is reduced until each shard has at least one byte.
type Sharded struct {
	parts []*Cache
}

// NewSharded returns n caches that share the options. n below 1 becomes 1.
func NewSharded(n int, opts ...Option) *Sharded {
	if n < 1 {
		n = 1
	}
	probe := &Cache{}
	for _, opt := range opts {
		opt(probe)
	}
	limit := probe.maxBytes
	if limit > 0 && n > limit {
		n = limit
	}
	parts := make([]*Cache, n)
	for i := 0; i < n; i++ {
		parts[i] = New(opts...)
		if limit > 0 {
			share := limit / n
			if i < limit%n {
				share++
			}
			parts[i].mu.Lock()
			parts[i].maxBytes = share
			parts[i].mu.Unlock()
		}
	}
	return &Sharded{parts: parts}
}

// Close stops every shard.
func (s *Sharded) Close() {
	for _, part := range s.parts {
		part.Close()
	}
}

// Set stores value on the shard selected by key.
func (s *Sharded) Set(key string, value []byte, ttl time.Duration) error {
	return s.part(key).Set(key, value, ttl)
}

// Get reads value from the shard selected by key.
func (s *Sharded) Get(key string) ([]byte, bool) {
	return s.part(key).Get(key)
}

// Delete removes key from its shard.
func (s *Sharded) Delete(key string) (bool, error) {
	return s.part(key).Delete(key)
}

// Exists reports whether the shard still holds key.
func (s *Sharded) Exists(key string) bool {
	return s.part(key).Exists(key)
}

// Expire sets the deadline on the shard that holds key.
func (s *Sharded) Expire(key string, ttl time.Duration) (bool, error) {
	return s.part(key).Expire(key, ttl)
}

// TTL reports the remaining lifetime on the shard that holds key.
func (s *Sharded) TTL(key string) (time.Duration, bool) {
	return s.part(key).TTL(key)
}

// GetOrLoad loads a missing key on its shard. Callers for that key share one load.
func (s *Sharded) GetOrLoad(key string, ttl time.Duration, load func() ([]byte, error)) ([]byte, error) {
	return s.part(key).GetOrLoad(key, ttl, load)
}

// Clear removes every entry from every shard.
func (s *Sharded) Clear() error {
	for _, part := range s.parts {
		if err := part.Clear(); err != nil {
			return err
		}
	}
	return nil
}

// Sweep removes expired entries from every shard.
func (s *Sharded) Sweep() int {
	n := 0
	for _, part := range s.parts {
		n += part.Sweep()
	}
	return n
}

// Len sums the entries in every shard.
func (s *Sharded) Len() int {
	n := 0
	for _, part := range s.parts {
		n += part.Len()
	}
	return n
}

// Bytes sums the estimated bytes in every shard.
func (s *Sharded) Bytes() int {
	n := 0
	for _, part := range s.parts {
		n += part.Bytes()
	}
	return n
}

// Stats sums the counters. Latency percentiles are computed from the combined samples.
func (s *Sharded) Stats() Stats {
	var (
		hits, misses, evictions, expirations, ops uint64
		keys, bytes                               int
		samples                                   []time.Duration
		started                                   time.Time
	)
	for _, part := range s.parts {
		part.mu.Lock()
		hits += part.hits
		misses += part.misses
		evictions += part.evictions
		expirations += part.expirations
		ops += part.ops
		keys += len(part.items)
		bytes += part.bytes
		samples = append(samples, part.samples[:part.sampleN]...)
		if started.IsZero() || part.started.Before(started) {
			started = part.started
		}
		part.mu.Unlock()
	}
	st := Stats{
		Hits:        hits,
		Misses:      misses,
		Evictions:   evictions,
		Expirations: expirations,
		Keys:        keys,
		Bytes:       bytes,
		P95:         percentile(samples, 95),
		P99:         percentile(samples, 99),
	}
	if total := hits + misses; total > 0 {
		st.HitRatio = float64(hits) / float64(total)
	}
	if ops > 0 && !started.IsZero() {
		st.OpsPerSec = float64(ops) / rateWindow(started).Seconds()
	}
	return st
}

func (s *Sharded) part(key string) *Cache {
	return s.parts[shardIndex(key, len(s.parts))]
}

func shardIndex(key string, n int) int {
	var h uint32 = 2166136261
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % uint32(n))
}
