// Package lsmkvengine is a small, dependency-free Go library: Pure Go embedded log-structured merge-tree key-value engine with write-ahead log, memtable, SSTables, Bloom filters, and crash recovery.
//
// The public API is every exported identifier of this root package. Functions are pure: they do
// no I/O, keep no global mutable state and report invalid input through returned errors.
package lsmkvengine
