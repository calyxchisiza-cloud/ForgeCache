# ForgeCache

Phase 1 is an in-memory key-value store. A program stores a copy of a byte slice, reads a copy back, and deletes it.

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

The cache is not safe for concurrent callers yet. LRU eviction, TTL, a mutex, and persistence are later phases. See [docs/architecture.md](docs/architecture.md).

## Run

```
go run ./cmd/demo
```

## Test

```
go test ./...
go test -race ./...
```
