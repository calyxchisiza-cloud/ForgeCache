# ForgeCache

Phase 2 is an in-memory key-value store with a byte limit. A program stores a copy of a byte slice, reads a copy back, and deletes it. When `WithMaxBytes` is set, the least recently used entry is removed to make room.

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

A zero TTL stores the value with no expiration. A negative TTL is rejected. A positive TTL is rejected until expiration is implemented. Keys must be non-empty and at most 1024 bytes. The cache copies values on `Set` and `Get`, so a caller cannot change what is stored by editing its own slice.

`New()` does not evict. `New(forgecache.WithMaxBytes(n))` counts `len(key)+len(value)` and drops the least recently used entry when a new one would pass `n`. An entry larger than `n` is rejected and the cache is left unchanged. A read or a replacement makes that key the most recently used.

The cache is not safe for concurrent callers yet. TTL, a mutex, and persistence are later phases. See [docs/architecture.md](docs/architecture.md).

## Run

```
go run ./cmd/demo
```

## Test

```
go test ./...
go test -race ./...
```
