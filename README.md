# ForgeCache

ForgeCache v1.0 is an in-memory key-value store with a byte limit, expiration, a lock, optional recovery from disk, and counters. A program stores a copy of a byte slice, reads a copy back, and deletes it. Goroutines may share one cache. When `WithMaxBytes` is set, the least recently used entry is removed to make room. A positive TTL becomes an absolute deadline. `Open` restores a snapshot, then replays the log.

```go
cache := forgecache.New()

if err := cache.Set("name", []byte("Calyx"), 0); err != nil {
    log.Fatal(err)
}

value, found := cache.Get("name")
if found {
    fmt.Println(string(value))
}

if _, err := cache.Delete("name"); err != nil {
    log.Fatal(err)
}
```

A zero TTL stores the value with no expiration. A negative TTL is rejected. A positive TTL expires at `clock.Now().Add(ttl)`. `Get`, `Exists`, `Delete`, and `TTL` treat an expired key as missing and remove it, even when the background pass has not run. `TTL` returns `-1` for a key that has no deadline. `WithClock` replaces the clock so tests can move time without sleeping. `WithJanitor` sets the background interval. Zero disables it. `Close` stops that pass.

Keys must be non-empty and at most 1024 bytes. The cache copies values on `Set` and `Get`, so a caller cannot change what is stored by editing its own slice.

`New()` does not evict. `New(forgecache.WithMaxBytes(n))` counts `len(key)+len(value)` and drops the least recently used entry when a new one would pass `n`. An entry larger than `n` is rejected and the cache is left unchanged. A read or a replacement makes that key the most recently used.

One mutex covers the map, the recency list, and the byte count. `Get` takes that lock exclusively because a read moves the entry and may delete an expired one.

`Open(dir)` loads `snapshot.bin` when it exists, replays later records from `append.log`, and drops entries that are already expired. `Snapshot` replaces `snapshot.bin` and truncates the log. A trailing partial record is ignored. A bad checksum is an error. `WithDurability(forgecache.DurabilityOS)` skips the flush after each mutation. `Close` and `Snapshot` still flush. The log stores absolute deadlines, not remaining durations. Access order is not restored.

`Stats` reports hits, misses, hit ratio, evictions, expirations, stored bytes, operations per second, and a p95 and p99 from the latest samples. `GetOrLoad` runs one load function when several goroutines miss the same key. `NewSharded(n)` splits the cache into n locks. Each shard keeps its own recency list, and the byte limit is divided across them.

```
go test -bench=. -benchmem ./...
```

The benchmarks compare this cache with an ordinary Go map. They are measurements, not a claim that one is faster. See [docs/architecture.md](docs/architecture.md), [docs/persistence.md](docs/persistence.md), and [docs/performance.md](docs/performance.md).

## Run

```
go run ./cmd/demo
```

## Test

```
go test ./...
go test -race ./...
```
