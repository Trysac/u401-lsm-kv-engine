package lsmkvengine

import (
	"bytes"
	"sort"
)

// Iterator walks through key-value records within a range.
type Iterator interface {
	Valid() bool
	Key() []byte
	Value() []byte
	Next()
	Close() error
}

// SnapshotView captures a point-in-time view across memtables and sstables.
type SnapshotView struct {
	entries []MemEntry
	readers []*SSTableReader
}

// SliceIterator iterates over a pre-merged slice of entries.
type SliceIterator struct {
	entries []MemEntry
	idx     int
	hi      []byte
	closed  bool
	readers []*SSTableReader
}

// Valid returns true if the iterator is positioned at a valid item.
func (it *SliceIterator) Valid() bool {
	if it.closed || it.idx >= len(it.entries) {
		return false
	}
	if len(it.hi) > 0 && bytes.Compare(it.entries[it.idx].Key, it.hi) >= 0 {
		return false
	}
	return true
}

// Key returns the current entry key.
func (it *SliceIterator) Key() []byte {
	if !it.Valid() {
		return nil
	}
	res := make([]byte, len(it.entries[it.idx].Key))
	copy(res, it.entries[it.idx].Key)
	return res
}

// Value returns the current entry value.
func (it *SliceIterator) Value() []byte {
	if !it.Valid() {
		return nil
	}
	res := make([]byte, len(it.entries[it.idx].Val))
	copy(res, it.entries[it.idx].Val)
	return res
}

// Next advances to the next entry.
func (it *SliceIterator) Next() {
	if it.Valid() {
		it.idx++
	}
}

// Close releases pinned resources.
func (it *SliceIterator) Close() error {
	if it.closed {
		return nil
	}
	it.closed = true
	for _, r := range it.readers {
		_ = r.DecRef()
	}
	it.readers = nil
	it.entries = nil
	return nil
}

// MergeSources merges entries from youngest to oldest:
// memtable -> immutable memtable -> L0 tables (newest to oldest) -> L1 tables.
// A key's latest state wins. Tombstones hide older entries.
func MergeSources(lo, hi []byte, sources [][]MemEntry) []MemEntry {
	latest := make(map[string]MemEntry)
	var allKeys []string

	// Iterate backwards (oldest to newest) so newer entries overwrite older ones
	for i := len(sources) - 1; i >= 0; i-- {
		for _, entry := range sources[i] {
			if len(lo) > 0 && bytes.Compare(entry.Key, lo) < 0 {
				continue
			}
			if len(hi) > 0 && bytes.Compare(entry.Key, hi) >= 0 {
				continue
			}
			skey := string(entry.Key)
			if _, exists := latest[skey]; !exists {
				allKeys = append(allKeys, skey)
			}
			latest[skey] = entry
		}
	}

	sort.Strings(allKeys)
	var merged []MemEntry
	for _, k := range allKeys {
		e := latest[k]
		if !e.Deleted {
			merged = append(merged, e)
		}
	}
	return merged
}
