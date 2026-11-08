// Package lsmkvengine provides a pure-Go, crash-resilient Log-Structured Merge-tree
// (LSM) embedded key-value storage engine.
//
// It features write-ahead logging (WAL) protected by CRC32C checksums, skiplist memtable,
// immutable SSTables with Bloom filters and restart blocks, manifest versioning,
// leveled compaction, and snapshot-isolated range iterators.
package lsmkvengine
