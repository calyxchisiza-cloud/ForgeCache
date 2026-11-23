# ForgeCache architecture

ForgeCache v1.0 stores values in a Go map, keeps recency in a doubly linked list written in this package, and drops a value at its deadline. `container/list` is not used. One mutex covers the map and the list unless `NewSharded` splits them. `Open` can restore values from a snapshot and an append-only log.

## Now

| Piece | Behavior |
| --- | --- |
| Storage | `map[string]*entry` plus a doubly linked list. The head is the most recently used entry. The tail is the least recently used. |
| Set | Copy the value. Reject an empty key, a key over 1024 bytes, a negative TTL, and an entry larger than the byte limit. A positive TTL stores an absolute deadline. A new or replaced entry moves to the head. If the limit would be exceeded, entries are removed from the tail first. |
| Get | Copy the value and move the entry to the head. A missing or expired key is a miss. Expiration is removed on access. |
| Delete | Unlink the entry and remove the key. A missing or expired key returns false. |
| Expire / TTL | `Expire` replaces the deadline. `TTL` returns the time remaining, or `-1` when the key does not expire. |
| Capacity | `WithMaxBytes` counts `len(key)+len(value)`. Zero means no limit. `Len` and `Bytes` report the current totals. |
| Janitor | `Sweep` removes every expired entry. A background pass calls it about once a second unless `WithJanitor` changes the interval. |
| Lock | One mutex. Every operation, including the janitor, holds it for the whole change. A shared lock would not make `Get` concurrent, because a hit updates the list. |
| Recovery | `Open` loads the newest snapshot, replays log records with a higher sequence, and skips expired entries. `Set`, `Delete`, `Expire`, and `Clear` append a record. Evictions and reads are not logged. |
| Stats | Hits and misses count `Get` and `GetOrLoad`. Evictions count capacity removals. Expirations count deadline removals. Operations per second use the wall clock since `New`. p95 and p99 use the latest 256 samples. |
| Load | `GetOrLoad` calls the load function once while other goroutines wait for that key. |
| Shards | `NewSharded` gives each shard its own mutex, map, and recency list. The byte limit is split. Eviction stays inside the shard. |

An entry records the value, the time it was stored, the time it was last read, the deadline, and the list links. The deadline is the zero time when the value does not expire.

A distributed cache, consistent hashing, and a network interface are outside this version.

ForgeCache does not replace the database that owns the data. It keeps a copy that is faster to read.
