# ForgeCache

Phase 4 is an in-memory key-value store with a byte limit, expiration, and a lock. A program stores a copy of a byte slice, reads a copy back, and deletes it. Goroutines may share one cache. When `WithMaxBytes` is set, the least recently used entry is removed to make room. A positive TTL becomes an absolute deadline.

```go
cache := forgecache.New()

if err := cache.Set("name", []byte("Calyx"), 0); err != nil {
    log.Fatal(err)
}

value, found := cache.Get("name")
if found {
    fmt.Println(string(value))
}

cache.Delete("name")
```

A zero TTL stores the value with no expiration. A negative TTL is rejected. A positive TTL expires at `clock.Now().Add(ttl)`. `Get`, `Exists`, `Delete`, and `TTL` treat an expired key as missing and remove it, even when the background pass has not run. `TTL` returns `-1` for a key that has no deadline. `WithClock` replaces the clock so tests can move time without sleeping. `WithJanitor` sets the background interval. Zero disables it. `Close` stops that pass.

Keys must be non-empty and at most 1024 bytes. The cache copies values on `Set` and `Get`, so a caller cannot change what is stored by editing its own slice.

`New()` does not evict. `New(forgecache.WithMaxBytes(n))` counts `len(key)+len(value)` and drops the least recently used entry when a new one would pass `n`. An entry larger than `n` is rejected and the cache is left unchanged. A read or a replacement makes that key the most recently used.

One mutex covers the map, the recency list, and the byte count. `Get` takes that lock exclusively because a read moves the entry and may delete an expired one. Persistence is a later phase. See [docs/architecture.md](docs/architecture.md).

## Run

```
go run ./cmd/demo
```

## Test

```
go test ./...
go test -race ./...
```
