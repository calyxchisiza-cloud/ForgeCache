# ForgeCache architecture

Phase 1 stores values in a Go map. Lookup, replacement, and deletion are the whole engine. Later phases add an LRU list, expiration, one mutex around the map, and optional disk recovery.

## Now

| Piece | Behavior |
| --- | --- |
| Storage | `map[string]entry` |
| Set | Copy the value. Reject an empty key, a key over 1024 bytes, a negative TTL, and a positive TTL. |
| Get | Copy the value. A missing key is a miss. A read updates the entry's access time. |
| Delete | Remove the key. A missing key returns false. |

An entry records the value, the time it was stored, and the time it was last read. It does not yet record an expiration or an LRU link.

## Later

- Phase 2: a doubly linked list, written here, evicts the least recently used entry when a byte capacity is exceeded.
- Phase 3: a zero TTL stays "no expiration". A positive TTL becomes an absolute deadline. Expired values are misses, and a background pass removes them.
- Phase 4: one mutex covers the map and the list. `go test -race` covers concurrent callers.
- Phase 5: an append-only log and snapshots can restore values after a restart. Access order is not preserved.
- Phase 6: hit and miss counters, benchmarks, and optional lock sharding.

ForgeCache does not replace the database that owns the data. It keeps a copy that is faster to read.
