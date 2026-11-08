package lsmkvengine

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"sync"
)

const (
	skipMaxLevel = 16
	skipP        = 0.5
)

type memNode struct {
	key     []byte
	val     []byte
	deleted bool
	forward []*memNode
}

// MemTable is a concurrent-safe in-memory skiplist.
type MemTable struct {
	mu    sync.RWMutex
	head  *memNode
	level int
	size  int
}

// NewMemTable creates an empty MemTable.
func NewMemTable() *MemTable {
	head := &memNode{
		forward: make([]*memNode, skipMaxLevel),
	}
	return &MemTable{
		head:  head,
		level: 1,
		size:  0,
	}
}

func (m *MemTable) randomLevel() int {
	var buf [4]byte
	lvl := 1
	for lvl < skipMaxLevel {
		if _, err := rand.Read(buf[:]); err != nil {
			break
		}
		val := binary.LittleEndian.Uint32(buf[:])
		if float64(val)/float64(^uint32(0)) < skipP {
			lvl++
		} else {
			break
		}
	}
	return lvl
}

// Put inserts or updates a key in the memtable.
func (m *MemTable) Put(key, val []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	update := make([]*memNode, skipMaxLevel)
	curr := m.head
	for i := m.level - 1; i >= 0; i-- {
		for curr.forward[i] != nil && bytes.Compare(curr.forward[i].key, key) < 0 {
			curr = curr.forward[i]
		}
		update[i] = curr
	}
	curr = curr.forward[0]

	if curr != nil && bytes.Equal(curr.key, key) {
		m.size -= len(curr.val)
		curr.val = append([]byte(nil), val...)
		curr.deleted = false
		m.size += len(curr.val)
		return
	}

	lvl := m.randomLevel()
	if lvl > m.level {
		for i := m.level; i < lvl; i++ {
			update[i] = m.head
		}
		m.level = lvl
	}

	newNode := &memNode{
		key:     append([]byte(nil), key...),
		val:     append([]byte(nil), val...),
		deleted: false,
		forward: make([]*memNode, lvl),
	}
	for i := 0; i < lvl; i++ {
		newNode.forward[i] = update[i].forward[i]
		update[i].forward[i] = newNode
	}
	m.size += len(key) + len(val) + 32
}

// Delete marks a key as deleted (tombstone) in the memtable.
func (m *MemTable) Delete(key []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	update := make([]*memNode, skipMaxLevel)
	curr := m.head
	for i := m.level - 1; i >= 0; i-- {
		for curr.forward[i] != nil && bytes.Compare(curr.forward[i].key, key) < 0 {
			curr = curr.forward[i]
		}
		update[i] = curr
	}
	curr = curr.forward[0]

	if curr != nil && bytes.Equal(curr.key, key) {
		m.size -= len(curr.val)
		curr.val = nil
		curr.deleted = true
		return
	}

	lvl := m.randomLevel()
	if lvl > m.level {
		for i := m.level; i < lvl; i++ {
			update[i] = m.head
		}
		m.level = lvl
	}

	newNode := &memNode{
		key:     append([]byte(nil), key...),
		val:     nil,
		deleted: true,
		forward: make([]*memNode, lvl),
	}
	for i := 0; i < lvl; i++ {
		newNode.forward[i] = update[i].forward[i]
		update[i].forward[i] = newNode
	}
	m.size += len(key) + 32
}

// Get searches for a key in the memtable.
// Returns (value, found, isDeleted).
func (m *MemTable) Get(key []byte) ([]byte, bool, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	curr := m.head
	for i := m.level - 1; i >= 0; i-- {
		for curr.forward[i] != nil && bytes.Compare(curr.forward[i].key, key) < 0 {
			curr = curr.forward[i]
		}
	}
	curr = curr.forward[0]
	if curr != nil && bytes.Equal(curr.key, key) {
		if curr.deleted {
			return nil, true, true
		}
		res := make([]byte, len(curr.val))
		copy(res, curr.val)
		return res, true, false
	}
	return nil, false, false
}

// Size returns estimated byte usage.
func (m *MemTable) Size() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.size
}

// Entry represents a key-value record in order.
type MemEntry struct {
	Key     []byte
	Val     []byte
	Deleted bool
}

// All returns all entries in ascending order.
func (m *MemTable) All() []MemEntry {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var res []MemEntry
	curr := m.head.forward[0]
	for curr != nil {
		res = append(res, MemEntry{
			Key:     append([]byte(nil), curr.key...),
			Val:     append([]byte(nil), curr.val...),
			Deleted: curr.deleted,
		})
		curr = curr.forward[0]
	}
	return res
}
