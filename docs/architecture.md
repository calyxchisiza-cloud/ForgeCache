# ForgeCache architecture

Phase 2 stores values in a Go map and keeps recency in a doubly linked list written in this package. `container/list` is not used. Later phases add expiration, one mutex around the map and the list, and optional disk recovery.

## Now

| Piece | Behavior |
| --- | --- |
| Storage | `map[string]*entry` plus a doubly linked list. The head is the most recently used entry. The tail is the least recently used. |
| Set | Copy the value. Reject an empty key, a key over 1024 bytes, a negative TTL, a positive TTL, and an entry larger than the byte limit. A new or replaced entry moves to the head. If the limit would be exceeded, entries are removed from the tail first. |
| Get | Copy the value and move the entry to the head. A missing key is a miss. |
| Delete | Unlink the entry and remove the key. A missing key returns false. |
| Capacity | `WithMaxBytes` counts `len(key)+len(value)`. Zero means no limit. `Len` and `Bytes` report the current totals. |

An entry records the value, the time it was stored, the time it was last read, and the list links. It does not yet record an expiration.

## Later

- Phase 3: a zero TTL stays "no expiration". A positive TTL becomes an absolute deadline. Expired values are misses, and a background pass removes them.
- Phase 4: one mutex covers the map and the list. `go test -race` covers concurrent callers.
- Phase 5: an append-only log and snapshots can restore values after a restart. Access order is not preserved.
- Phase 6: hit and miss counters, benchmarks, and optional lock sharding.

ForgeCache does not replace the database that owns the data. It keeps a copy that is faster to read.
