# Performance

`Stats` is the operational view of one cache.

| Field | Meaning |
| --- | --- |
| Hits | `Get` or `GetOrLoad` found a live value. Waiters on a shared load count as hits. |
| Misses | `Get` missed, or `GetOrLoad` had to call `load`. One shared load is one miss. |
| HitRatio | Hits divided by hits plus misses. Zero when nothing has been read. |
| Evictions | Entries removed because a new one would pass the byte limit. |
| Expirations | Entries removed because the deadline passed. |
| Keys, Bytes | What is stored now, including entries the janitor has not swept. Bytes are `len(key)+len(value)`. |
| OpsPerSec | `Set`, `Get`, and `Delete` calls divided by wall-clock time since `New`. |
| P95, P99 | Latencies of the most recent 256 of those calls. |

`NewSharded` adds the same counters across shards. Percentiles are computed again from the combined samples.

## Benchmarks

```
go test -bench=. -benchmem ./...
```

`BenchmarkMapGet` reads an ordinary Go map. `BenchmarkCacheGet` reads one key from ForgeCache. `BenchmarkCacheSetEvict` writes into a cache small enough to evict. `BenchmarkShardedGet` reads through eight shards.

A Go map does less work: it does not take a lock, copy the value, or move a list node. Run both on the same machine before describing a difference. These benchmarks do not include Redis or Memcached.
