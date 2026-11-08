package lsmkvengine

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
)

// Size limit defaults.
const (
	MaxKeyBytes     = 64 * 1024       // 64 KiB
	MaxValueBytes   = 4 * 1024 * 1024 // 4 MiB
	DefaultMemtable = 4 * 1024 * 1024 // 4 MiB
)

// SyncPolicy controls fsync durability on writes.
type SyncPolicy int

const (
	SyncAlways SyncPolicy = iota
	SyncEveryN
	SyncNever
)

// Options configures the DB.
type Options struct {
	MemtableBytes   int
	SyncMode        SyncPolicy
	SyncEveryNCount int
	BloomBitsPerKey int
	MaxOpenFiles    int
	FSRef           FileSystem
}

// DB is the pure-Go LSM engine.
type DB struct {
	mu         sync.RWMutex
	dir        string
	opts       Options
	fs         FileSystem
	manifest   *Manifest
	mem        *MemTable
	imm        *MemTable
	wal        *WALWriter
	walFile    File
	l0Tables   []*SSTableReader
	l1Tables   []*SSTableReader
	tableFiles map[uint64]*SSTableReader
	writeOps   atomic.Int64
	closed     atomic.Bool
	lockFile   File
	flushMu    sync.Mutex
}

// Open initializes or loads an LSM DB at dir.
func Open(dir string, opts Options) (*DB, error) {
	if opts.MemtableBytes <= 0 {
		opts.MemtableBytes = DefaultMemtable
	}
	if opts.BloomBitsPerKey <= 0 {
		opts.BloomBitsPerKey = 10
	}
	if opts.FSRef == nil {
		opts.FSRef = DefaultFS
	}
	if opts.SyncEveryNCount <= 0 {
		opts.SyncEveryNCount = 100
	}

	fs := opts.FSRef
	if err := fs.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}

	// Single-process directory lockfile
	lockPath := fmt.Sprintf("%s/LOCK", dir)
	lockFile, err := fs.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o666)
	if err != nil {
		return nil, err
	}

	manifest, err := OpenManifest(fs, dir)
	if err != nil {
		_ = lockFile.Close()
		return nil, err
	}

	db := &DB{
		dir:        dir,
		opts:       opts,
		fs:         fs,
		manifest:   manifest,
		mem:        NewMemTable(),
		tableFiles: make(map[uint64]*SSTableReader),
		lockFile:   lockFile,
	}

	// Load SSTables recorded in Manifest
	levels := manifest.GetLevels()
	for _, id := range levels[0] {
		r, err := db.openTable(id)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		db.l0Tables = append(db.l0Tables, r)
	}
	for _, id := range levels[1] {
		r, err := db.openTable(id)
		if err != nil {
			_ = db.Close()
			return nil, err
		}
		db.l1Tables = append(db.l1Tables, r)
	}

	// Recover active WAL if present
	walPath := fmt.Sprintf("%s/CURRENT.wal", dir)
	walRead, err := fs.OpenFile(walPath, os.O_RDONLY, 0)
	if err == nil {
		entries, werr := ReadWALEntries(walRead)
		_ = walRead.Close()
		if werr != nil {
			_ = db.Close()
			return nil, werr
		}
		for _, e := range entries {
			if e.Type == recordPut {
				db.mem.Put(e.Key, e.Value)
			} else if e.Type == recordDelete {
				db.mem.Delete(e.Key)
			}
		}
	}

	// Open WAL for append writes
	walFile, err := fs.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o666)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	db.walFile = walFile
	db.wal = NewWALWriter(walFile)

	return db, nil
}

func (db *DB) openTable(id uint64) (*SSTableReader, error) {
	if r, exists := db.tableFiles[id]; exists {
		r.IncRef()
		return r, nil
	}
	path := fmt.Sprintf("%s/%06d.sst", db.dir, id)
	f, err := db.fs.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, newLSMError("corrupt_sstable", "failed to open sstable file", err)
	}
	r, err := OpenSSTableReader(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	db.tableFiles[id] = r
	return r, nil
}

func (db *DB) validateBounds(k, v []byte) error {
	if len(k) > MaxKeyBytes {
		return ErrKeyTooLarge
	}
	if len(v) > MaxValueBytes {
		return ErrValTooLarge
	}
	return nil
}

// Put writes a key-value pair.
func (db *DB) Put(key, value []byte) error {
	if db.closed.Load() {
		return ErrClosed
	}
	if err := db.validateBounds(key, value); err != nil {
		return err
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	if err := db.wal.Append(recordPut, key, value); err != nil {
		return err
	}
	db.applySyncPolicy()
	db.mem.Put(key, value)

	if db.mem.Size() >= db.opts.MemtableBytes {
		if err := db.freezeAndFlush(); err != nil {
			return err
		}
	}
	return nil
}

// Delete marks a key as deleted.
func (db *DB) Delete(key []byte) error {
	if db.closed.Load() {
		return ErrClosed
	}
	if err := db.validateBounds(key, nil); err != nil {
		return err
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	if err := db.wal.Append(recordDelete, key, nil); err != nil {
		return err
	}
	db.applySyncPolicy()
	db.mem.Delete(key)

	if db.mem.Size() >= db.opts.MemtableBytes {
		if err := db.freezeAndFlush(); err != nil {
			return err
		}
	}
	return nil
}

// Write applies an atomic batch of operations.
func (db *DB) Write(b *Batch) error {
	if db.closed.Load() {
		return ErrClosed
	}
	if b == nil || len(b.ops) == 0 {
		return nil
	}
	for _, op := range b.ops {
		if err := db.validateBounds(op.Key, op.Value); err != nil {
			return err
		}
	}

	db.mu.Lock()
	defer db.mu.Unlock()

	for _, op := range b.ops {
		if err := db.wal.Append(op.Type, op.Key, op.Value); err != nil {
			return err
		}
		if op.Type == recordPut {
			db.mem.Put(op.Key, op.Value)
		} else if op.Type == recordDelete {
			db.mem.Delete(op.Key)
		}
	}
	db.applySyncPolicy()

	if db.mem.Size() >= db.opts.MemtableBytes {
		if err := db.freezeAndFlush(); err != nil {
			return err
		}
	}
	return nil
}

func (db *DB) applySyncPolicy() {
	switch db.opts.SyncMode {
	case SyncAlways:
		_ = db.wal.Sync()
	case SyncEveryN:
		ops := db.writeOps.Add(1)
		if ops%int64(db.opts.SyncEveryNCount) == 0 {
			_ = db.wal.Sync()
		}
	case SyncNever:
		// no-op
	}
}

// Get retrieves a key's value.
func (db *DB) Get(key []byte) ([]byte, error) {
	if db.closed.Load() {
		return nil, ErrClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()

	// 1. Memtable
	if val, found, deleted := db.mem.Get(key); found {
		if deleted {
			return nil, ErrNotFound
		}
		return val, nil
	}

	// 2. Immutable memtable
	if db.imm != nil {
		if val, found, deleted := db.imm.Get(key); found {
			if deleted {
				return nil, ErrNotFound
			}
			return val, nil
		}
	}

	// 3. L0 SSTables (newest to oldest)
	for i := len(db.l0Tables) - 1; i >= 0; i-- {
		r := db.l0Tables[i]
		val, found, deleted, err := r.Get(key)
		if err != nil {
			return nil, err
		}
		if found {
			if deleted {
				return nil, ErrNotFound
			}
			return val, nil
		}
	}

	// 4. L1 SSTables
	for _, r := range db.l1Tables {
		val, found, deleted, err := r.Get(key)
		if err != nil {
			return nil, err
		}
		if found {
			if deleted {
				return nil, ErrNotFound
			}
			return val, nil
		}
	}

	return nil, ErrNotFound
}

// NewIterator creates a range iterator [lo, hi).
func (db *DB) NewIterator(lo, hi []byte) (Iterator, error) {
	if db.closed.Load() {
		return nil, ErrClosed
	}
	db.mu.RLock()
	defer db.mu.RUnlock()

	var sources [][]MemEntry
	var readers []*SSTableReader

	// Memtable
	sources = append(sources, db.mem.All())

	// Immutable memtable
	if db.imm != nil {
		sources = append(sources, db.imm.All())
	}

	// L0 (newest to oldest)
	for i := len(db.l0Tables) - 1; i >= 0; i-- {
		r := db.l0Tables[i]
		r.IncRef()
		readers = append(readers, r)
		entries, err := r.ReadAllEntries()
		if err != nil {
			for _, reader := range readers {
				_ = reader.DecRef()
			}
			return nil, err
		}
		sources = append(sources, entries)
	}

	// L1
	for _, r := range db.l1Tables {
		r.IncRef()
		readers = append(readers, r)
		entries, err := r.ReadAllEntries()
		if err != nil {
			for _, reader := range readers {
				_ = reader.DecRef()
			}
			return nil, err
		}
		sources = append(sources, entries)
	}

	merged := MergeSources(lo, hi, sources)
	return &SliceIterator{
		entries: merged,
		idx:     0,
		hi:      append([]byte(nil), hi...),
		readers: readers,
	}, nil
}

// freezeAndFlush flushes active memtable to L0 SSTable.
func (db *DB) freezeAndFlush() error {
	db.flushMu.Lock()
	defer db.flushMu.Unlock()

	db.imm = db.mem
	db.mem = NewMemTable()

	fileID := db.manifest.AllocFileID()
	sstPath := fmt.Sprintf("%s/%06d.sst", db.dir, fileID)
	sstFile, err := db.fs.Create(sstPath)
	if err != nil {
		return err
	}

	w := NewSSTableWriter(sstFile, db.opts.BloomBitsPerKey)
	for _, e := range db.imm.All() {
		if err := w.Add(e.Key, e.Val, e.Deleted); err != nil {
			_ = sstFile.Close()
			return err
		}
	}
	if err := w.Finish(); err != nil {
		_ = sstFile.Close()
		return err
	}
	if err := sstFile.Sync(); err != nil {
		_ = sstFile.Close()
		return err
	}
	_ = sstFile.Close()

	r, err := db.openTable(fileID)
	if err != nil {
		return err
	}
	db.l0Tables = append(db.l0Tables, r)
	if err := db.manifest.AddSSTable(0, fileID); err != nil {
		return err
	}

	// Clear imm and rotate WAL
	db.imm = nil
	_ = db.wal.Close()
	walPath := fmt.Sprintf("%s/CURRENT.wal", db.dir)
	walFile, err := db.fs.OpenFile(walPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	db.walFile = walFile
	db.wal = NewWALWriter(walFile)

	// Trigger L0 -> L1 compaction if threshold reached (>= 4 tables in L0)
	if len(db.l0Tables) >= 4 {
		if err := db.compactL0ToL1(); err != nil {
			return err
		}
	}
	return nil
}

// compactL0ToL1 compacts all L0 tables into sorted, non-overlapping L1 tables.
func (db *DB) compactL0ToL1() error {
	if len(db.l0Tables) == 0 {
		return nil
	}

	var sources [][]MemEntry
	for i := len(db.l0Tables) - 1; i >= 0; i-- {
		e, err := db.l0Tables[i].ReadAllEntries()
		if err != nil {
			return err
		}
		sources = append(sources, e)
	}
	for _, r := range db.l1Tables {
		e, err := r.ReadAllEntries()
		if err != nil {
			return err
		}
		sources = append(sources, e)
	}

	merged := MergeSources(nil, nil, sources)

	// Write new L1 table(s)
	l1FileID := db.manifest.AllocFileID()
	l1Path := fmt.Sprintf("%s/%06d.sst", db.dir, l1FileID)
	l1File, err := db.fs.Create(l1Path)
	if err != nil {
		return err
	}
	w := NewSSTableWriter(l1File, db.opts.BloomBitsPerKey)
	for _, e := range merged {
		if err := w.Add(e.Key, e.Val, e.Deleted); err != nil {
			_ = l1File.Close()
			return err
		}
	}
	if err := w.Finish(); err != nil {
		_ = l1File.Close()
		return err
	}
	if err := l1File.Sync(); err != nil {
		_ = l1File.Close()
		return err
	}
	_ = l1File.Close()

	newL1Reader, err := db.openTable(l1FileID)
	if err != nil {
		return err
	}

	// Old tables are released via DecRef
	oldL0 := db.l0Tables
	oldL1 := db.l1Tables

	db.l0Tables = nil
	db.l1Tables = []*SSTableReader{newL1Reader}

	_ = db.manifest.ReplaceLevel(0, nil)
	_ = db.manifest.ReplaceLevel(1, []uint64{l1FileID})

	for _, r := range oldL0 {
		_ = r.DecRef()
	}
	for _, r := range oldL1 {
		_ = r.DecRef()
	}
	return nil
}

// Close closes open files and flushes writes.
func (db *DB) Close() error {
	if db.closed.Swap(true) {
		return nil
	}
	db.mu.Lock()
	defer db.mu.Unlock()

	var errs []error
	if db.wal != nil {
		if err := db.wal.Sync(); err != nil {
			errs = append(errs, err)
		}
		if err := db.wal.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, r := range db.l0Tables {
		if err := r.DecRef(); err != nil {
			errs = append(errs, err)
		}
	}
	for _, r := range db.l1Tables {
		if err := r.DecRef(); err != nil {
			errs = append(errs, err)
		}
	}
	if db.lockFile != nil {
		if err := db.lockFile.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
