# Use Immutable SSTables with Bloom Filters and WAL Crash Recovery

Status: accepted (generated, pending human review)

## Context

Embedded key-value engines require high write throughput and safe crash recovery without incurring external Cgo dependencies or risk of corruption during sudden shutdowns.

## Decision

Implement an LSM architecture combining a CRC32C-guarded write-ahead log, skiplist memtable, immutable block-based SSTables with Bloom filters, and ref-counted snapshot iterators.

## Consequences

Provides zero-dependency embedded persistence with deterministic snapshot reads and verified durability against simulated power failures.
