# Architecture

## Components
- **MemTable**: Skiplist implementation supporting concurrent reads and single-writer mutations with tombstones for deletions.
- **WAL**: Append-only log file with CRC32C checksum headers, tolerating truncated trailing writes during crash recovery.
- **SSTable**: Immutable data files structured with chunked data blocks, restart markers, index blocks, and embedded Bloom filters.
- **Manifest**: Tracks active SSTable generations and level distributions.
- **Compaction**: Leveled merge sort combining L0 tables into L1 while shedding shadowed keys.
