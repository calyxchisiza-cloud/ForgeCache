# Persistence

ForgeCache can put a cache back in memory after the process exits. This is a copy of cached values, not a record of every read, and it is not a replacement for the database that owns the data.

## Files

`Open(dir)` creates the directory when it is missing.

| File | Role |
| --- | --- |
| `snapshot.bin` | Every unexpired entry at the last `Snapshot` call, plus the log sequence included in that image. |
| `snapshot.bin.old` | The previous image, used only when `snapshot.bin` is missing. |
| `append.log` | Mutations after that sequence. |

Startup loads the snapshot, replays records whose sequence is higher, then skips any entry whose deadline has already passed.

## Records

Each log record is binary. It holds a sequence number, an operation, and a checksum. `Set` and `Expire` store the key, the value, and an absolute deadline. A zero deadline means the value does not expire. `Delete` stores the key. `Clear` stores neither.

The checksum covers the record bytes that precede it. A record that stops before the checksum is treated as a torn write at the end of the file and is ignored. A complete record with the wrong checksum stops recovery with `ErrCorruptLog`.

Values are length-prefixed, so a value may contain any byte, including zero.

## Flushing

`DurabilitySync` is the default. The log is flushed after every mutation. `DurabilityOS` writes the record and lets the operating system flush it. `Close` and `Snapshot` still flush.

`Snapshot` writes `snapshot.bin.tmp`, flushes it, and renames it over `snapshot.bin`. It then truncates the log. Operations wait while that runs.

## What a restart does not keep

Reads are not logged, and neither are evictions. Replay inserts keys in log order and evicts again if a byte limit is set. The survivor set can differ from the set that was in memory when the process stopped. The recency list after `Open` is not the list from before the restart.
