# lsm-kv-engine

Pure Go embedded Log-Structured Merge-tree (LSM) key-value engine with write-ahead log (WAL), memtable, SSTables, Bloom filters, and crash recovery. Applications requiring durable, embedded key-value storage often rely on complex Cgo bindings or monolithic engines that complicate cross-compilation and fail to provide predictable crash recovery under sudden power cuts. This library provides a clean, zero-dependency, crash-resilient LSM-tree storage engine with deterministic snapshot isolation and verifiable durability.

## Installation

`go get github.com/Trysac/u401-lsm-kv-engine`

## Usage

```go
package main
import "github.com/Trysac/u401-lsm-kv-engine"
func main() {
	db, _ := lsmkvengine.Open(".data", lsmkvengine.Options{})
	defer db.Close()
	_ = db.Put([]byte("user:101"), []byte("alice"))
	val, _ := db.Get([]byte("user:101"))
	fmt.Println(string(val))
}
```

## Public API

- `Open(dir string, opts Options) (*DB, error)`: Initializes the database at the provided path.
- `Put(key, value []byte) error`: Writes a key-value pair.
- `Get(key []byte) ([]byte, error)`: Retrieves a value. Returns `*LSMError` with code 'not_found' if absent.
- `Delete(key []byte) error`: Deletes a key via tombstone.
- `Write(b *Batch) error`: Atomically commits a batch.
- `NewIterator(lo, hi []byte) (Iterator, error)`: Scans a key range.
- `Close() error`: Flushes memtables and closes files.

## Limits and Options

- **Limits**: Keys capped at 64 KiB, Values at 4 MiB. Exceeding these returns `key_too_large` or `val_too_large` errors.
- **Memtable**: Default 4 MiB cap before flushing to L0.
- **Consistency**: Configurable `SyncPolicy` (SyncAlways, SyncEveryN, SyncNever).

## Why not existing alternatives?

Alternatives often rely on complex Cgo bindings or monolithic architectures. This engine provides a pure-Go, isolated implementation that avoids runtime panics on corrupted logs and handles concurrent background compaction without snapshot inconsistencies.

## Security

See [docs/SECURITY.md](docs/SECURITY.md) for the threat model and limits.