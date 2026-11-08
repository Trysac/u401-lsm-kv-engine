package lsmkvengine_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/Trysac/u194-golden-files"
	"github.com/Trysac/u401-lsm-kv-engine"
	"github.com/Trysac/u401-lsm-kv-engine/internal/metrics"
)

func TestKeyValSizeLimits(t *testing.T) {
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/limitsdb", lsmkvengine.Options{FSRef: fs})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	largeKey := make([]byte, lsmkvengine.MaxKeyBytes+1)
	err = db.Put(largeKey, []byte("ok"))
	if !errors.Is(err, lsmkvengine.ErrKeyTooLarge) {
		t.Fatalf("expected ErrKeyTooLarge, got %v", err)
	}

	largeVal := make([]byte, lsmkvengine.MaxValueBytes+1)
	err = db.Put([]byte("key"), largeVal)
	if !errors.Is(err, lsmkvengine.ErrValTooLarge) {
		t.Fatalf("expected ErrValTooLarge, got %v", err)
	}
}

func TestGoldenFilesOutput(t *testing.T) {
	g := goldenfiles.Golden{Dir: "testdata"}
	g.Assert(t, "sstable_record_sample", []byte("test_golden_lsm_record_payload\n"))
}

func TestMetricsExport(t *testing.T) {
	m := metrics.NewEngineMetrics()
	if m.Handler() == nil {
		t.Fatalf("expected valid metrics handler")
	}
}

func TestDefaultOSFS(t *testing.T) {
	dir, err := os.MkdirTemp("", "osfs_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	// Exercise DefaultOSFS methods directly
	osfs := lsmkvengine.DefaultFS
	testFile := dir + "/osfs_test.txt"
	f, err := osfs.Create(testFile)
	if err != nil {
		t.Fatalf("osfs Create failed: %v", err)
	}
	_, _ = f.Write([]byte("test data"))
	_ = f.Close()

	info, err := osfs.Stat(testFile)
	if err != nil || info.Size() != 9 {
		t.Fatalf("osfs Stat failed: %v", err)
	}

	renamedFile := dir + "/osfs_renamed.txt"
	if err := osfs.Rename(testFile, renamedFile); err != nil {
		t.Fatalf("osfs Rename failed: %v", err)
	}

	entries, err := osfs.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		t.Fatalf("osfs ReadDir failed: %v", err)
	}

	if err := osfs.Remove(renamedFile); err != nil {
		t.Fatalf("osfs Remove failed: %v", err)
	}

	db, err := lsmkvengine.Open(dir, lsmkvengine.Options{})
	if err != nil {
		t.Fatalf("OS Open failed: %v", err)
	}
	_ = db.Put([]byte("hello"), []byte("world"))
	v, err := db.Get([]byte("hello"))
	if err != nil || !bytes.Equal(v, []byte("world")) {
		t.Fatalf("failed to read OS fs key: %v", err)
	}
	_ = db.Close()
}

func TestMemFSFullCoverage(t *testing.T) {
	m := lsmkvengine.NewMemFS()
	_ = m.MkdirAll("/a/b", 0755)
	_ = m.MkdirAll(".", 0755)
	_ = m.MkdirAll("/", 0755)

	f, err := m.Create("/a/b/file1.txt")
	if err != nil {
		t.Fatal(err)
	}
	n, err := f.Write([]byte("hello world"))
	if err != nil || n != 11 {
		t.Fatalf("write failed: %v", err)
	}
	st, err := f.Stat()
	if err != nil || st.Size() != 11 || st.Name() != "file1.txt" || st.IsDir() || st.Mode() == 0 || st.ModTime().IsZero() == false || st.Sys() != nil {
		t.Fatalf("f.Stat error: %v, info: %+v", err, st)
	}
	_ = f.Sync()

	// Read & ReadAt
	buf := make([]byte, 5)
	nr, err := f.ReadAt(buf, 0)
	if err != nil || string(buf) != "hello" || nr != 5 {
		t.Fatalf("ReadAt failed: %v", err)
	}
	// ReadAt past EOF
	_, err = f.ReadAt(buf, 100)
	if err == nil {
		t.Fatalf("expected EOF on ReadAt past EOF")
	}
	// ReadAt partial read reaching EOF
	largeBuf := make([]byte, 20)
	nr, err = f.ReadAt(largeBuf, 5)
	if err == nil || nr != 6 {
		t.Fatalf("expected EOF on partial ReadAt, got nr=%d, err=%v", nr, err)
	}
	// Read
	rf, err := m.OpenFile("/a/b/file1.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	rbuf := make([]byte, 5)
	nr, err = rf.Read(rbuf)
	if err != nil || string(rbuf) != "hello" || nr != 5 {
		t.Fatalf("Read failed: %v", err)
	}
	// Read until EOF
	rest := make([]byte, 20)
	_, _ = rf.Read(rest)
	_, err = rf.Read(rbuf)
	if err == nil {
		t.Fatalf("expected EOF on Read")
	}
	_ = rf.Close()
	_, err = rf.Read(rbuf)
	if err == nil {
		t.Fatalf("expected ErrClosed")
	}

	// Overwrite via Write
	wf, err := m.OpenFile("/a/b/file1.txt", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	// Overwrite first 5 bytes
	_, _ = wf.Write([]byte("HELLO"))
	// Overwrite past current length
	_, _ = wf.Write([]byte(" WORLD EXTRA LONG DATA"))
	_ = wf.Sync()
	_ = wf.Close()
	_, err = wf.Write([]byte("more"))
	if err == nil {
		t.Fatalf("expected ErrClosed on closed write")
	}
	_, err = wf.ReadAt(buf, 0)
	if err == nil {
		t.Fatalf("expected ErrClosed on closed ReadAt")
	}
	if err := wf.Sync(); err == nil {
		t.Fatalf("expected ErrClosed on closed Sync")
	}

	// Append flag
	af, err := m.OpenFile("/a/b/file1.txt", os.O_RDWR|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = af.Write([]byte("!!"))
	_ = af.Close()

	// Stat from MemFS
	mst, err := m.Stat("/a/b/file1.txt")
	if err != nil || mst.Name() != "file1.txt" {
		t.Fatalf("m.Stat failed: %v", err)
	}
	_, err = m.Stat("/a/b/nonexistent")
	if err == nil {
		t.Fatalf("expected error on nonexistent file stat")
	}

	// ReadDir from MemFS
	entries, err := m.ReadDir("/a/b")
	if err != nil || len(entries) == 0 {
		t.Fatalf("m.ReadDir failed: %v", err)
	}
	de := entries[0]
	if de.Name() != "file1.txt" || de.IsDir() || de.Type() != 0 {
		t.Fatalf("unexpected dir entry: %+v", de)
	}
	dinfo, err := de.Info()
	if err != nil || dinfo.Name() != "file1.txt" {
		t.Fatalf("de.Info failed: %v", err)
	}

	// Rename & Remove
	if err := m.Rename("/a/b/file1.txt", "/a/b/file2.txt"); err != nil {
		t.Fatalf("m.Rename failed: %v", err)
	}
	if err := m.Rename("/a/b/nonexistent", "/a/b/file3.txt"); err == nil {
		t.Fatalf("expected error renaming nonexistent")
	}
	if err := m.Remove("/a/b/file2.txt"); err != nil {
		t.Fatalf("m.Remove failed: %v", err)
	}
	if err := m.Remove("/a/b/nonexistent"); err == nil {
		t.Fatalf("expected error removing nonexistent")
	}

	// OpenFile with non-existent without O_CREATE
	_, err = m.OpenFile("/nonexistent", os.O_RDONLY, 0)
	if err == nil {
		t.Fatalf("expected error opening non-existent without O_CREATE")
	}
	// OpenFile with O_TRUNC
	tf, _ := m.OpenFile("/trunc.txt", os.O_RDWR|os.O_CREATE, 0)
	_, _ = tf.Write([]byte("hello"))
	_ = tf.Sync()
	_ = tf.Close()
	tf2, _ := m.OpenFile("/trunc.txt", os.O_RDWR|os.O_TRUNC, 0)
	st2, _ := tf2.Stat()
	if st2.Size() != 0 {
		t.Fatalf("expected truncated file to be 0 size, got %d", st2.Size())
	}
	_ = tf2.Close()
}

func TestSSTableAndWALErrors(t *testing.T) {
	fs := lsmkvengine.NewMemFS()
	// SSTable corrupt cases
	// 1. File smaller than footer
	f, _ := fs.Create("/small.sst")
	_, _ = f.Write([]byte("too small"))
	_ = f.Close()
	fSmall, _ := fs.OpenFile("/small.sst", os.O_RDONLY, 0)
	_, err := lsmkvengine.OpenSSTableReader(fSmall)
	if err == nil {
		t.Fatalf("expected corrupt_sstable on small file")
	}

	// 2. Corrupted magic
	f2, _ := fs.Create("/badmagic.sst")
	badFooter := make([]byte, 48)
	_, _ = f2.Write(badFooter)
	_ = f2.Close()
	fBad, _ := fs.OpenFile("/badmagic.sst", os.O_RDONLY, 0)
	_, err = lsmkvengine.OpenSSTableReader(fBad)
	if err == nil {
		t.Fatalf("expected corrupt_sstable on bad magic")
	}

	// 3. Reader Close
	f3, _ := fs.Create("/valid.sst")
	w := lsmkvengine.NewSSTableWriter(f3, 10)
	_ = w.Add([]byte("k1"), []byte("v1"), false)
	_ = w.Finish()
	_ = f3.Close()
	fValid, _ := fs.OpenFile("/valid.sst", os.O_RDONLY, 0)
	r, err := lsmkvengine.OpenSSTableReader(fValid)
	if err != nil {
		t.Fatalf("OpenSSTableReader failed: %v", err)
	}
	_ = r.Close()

	// 4. Batch Clear & Len
	b := lsmkvengine.NewBatch()
	b.Put([]byte("a"), []byte("b"))
	b.Delete([]byte("c"))
	if b.Len() != 2 {
		t.Fatalf("expected batch len 2, got %d", b.Len())
	}
	b.Clear()
	if b.Len() != 0 {
		t.Fatalf("expected batch len 0 after clear")
	}
	// Write empty batch
	db, _ := lsmkvengine.Open("/emptybatchdb", lsmkvengine.Options{FSRef: fs})
	_ = db.Write(b)
	_ = db.Write(nil)
	_ = db.Close()
}

func TestManifestAndRecoveryCoverage(t *testing.T) {
	fs := lsmkvengine.NewMemFS()
	// Corrupt manifest
	mf, _ := fs.Create("/corrupt_m/MANIFEST")
	_, _ = mf.Write([]byte("{invalid json}"))
	_ = mf.Close()
	_, err := lsmkvengine.OpenManifest(fs, "/corrupt_m")
	if err == nil {
		t.Fatalf("expected error on corrupt manifest")
	}

	// Unreadable manifest (e.g. directory instead of file in real OS, or closed handle)
	_, err = lsmkvengine.ReadAll(fs, "/nonexistent_manifest/MANIFEST")
	if err == nil {
		t.Fatalf("expected error reading nonexistent file")
	}

	// Open Manifest with corrupted SSTable reference in L0
	mCorrupt, _ := lsmkvengine.OpenManifest(fs, "/corrupt_sst_ref")
	_ = mCorrupt.AddSSTable(0, 9999) // SSTable 9999 does not exist
	_, err = lsmkvengine.Open("/corrupt_sst_ref", lsmkvengine.Options{FSRef: fs})
	if err == nil {
		t.Fatalf("expected error opening DB with missing SSTable in manifest")
	}

	// WAL with deleted record recovered on Open
	db, err := lsmkvengine.Open("/wal_del", lsmkvengine.Options{
		FSRef:           fs,
		SyncMode:        lsmkvengine.SyncEveryN,
		SyncEveryNCount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = db.Put([]byte("delme"), []byte("value"))
	_ = db.Delete([]byte("delme"))
	_ = db.Close()

	// Reopen to verify WAL replay of Delete record
	dbReopened, err := lsmkvengine.Open("/wal_del", lsmkvengine.Options{FSRef: fs})
	if err != nil {
		t.Fatal(err)
	}
	_, err = dbReopened.Get([]byte("delme"))
	if !errors.Is(err, lsmkvengine.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for deleted key on reopen, got %v", err)
	}
	_ = dbReopened.Close()
}

func TestIteratorAndEdgeCases(t *testing.T) {
	fs := lsmkvengine.NewMemFS()
	db, _ := lsmkvengine.Open("/itdb", lsmkvengine.Options{
		FSRef:         fs,
		MemtableBytes: 128,
	})

	for i := 0; i < 20; i++ {
		_ = db.Put([]byte(string(rune('a'+i))), []byte("val"))
	}
	// Delete an item to exercise tombstone in MergeSources
	_ = db.Delete([]byte("c"))

	it, err := db.NewIterator([]byte("b"), []byte("f"))
	if err != nil {
		t.Fatal(err)
	}
	var scanned []string
	for it.Valid() {
		scanned = append(scanned, string(it.Key()))
		_ = it.Value()
		it.Next()
	}
	_ = it.Close()
	// Next, Key, Value on invalid iterator
	it.Next()
	if it.Key() != nil || it.Value() != nil || it.Valid() {
		t.Fatalf("expected nil key/value on invalid iterator")
	}
	_ = it.Close() // double close

	_ = db.Close()
	// Operations after DB closed
	if err := db.Put([]byte("k"), []byte("v")); !errors.Is(err, lsmkvengine.ErrClosed) {
		t.Fatalf("expected ErrClosed on Put")
	}
	if err := db.Delete([]byte("k")); !errors.Is(err, lsmkvengine.ErrClosed) {
		t.Fatalf("expected ErrClosed on Delete")
	}
	if err := db.Write(lsmkvengine.NewBatch()); !errors.Is(err, lsmkvengine.ErrClosed) {
		t.Fatalf("expected ErrClosed on Write")
	}
	if _, err := db.Get([]byte("k")); !errors.Is(err, lsmkvengine.ErrClosed) {
		t.Fatalf("expected ErrClosed on Get")
	}
	if _, err := db.NewIterator(nil, nil); !errors.Is(err, lsmkvengine.ErrClosed) {
		t.Fatalf("expected ErrClosed on NewIterator")
	}
	_ = db.Close() // double close
}

func TestLSMErrorsAndFormatting(t *testing.T) {
	errWithCause := lsmkvengine.ErrNotFound
	var lsmErr *lsmkvengine.LSMError
	if errors.As(errWithCause, &lsmErr) {
		_ = lsmErr.Error()
		_ = lsmErr.Unwrap()
		_ = lsmErr.Is(lsmkvengine.ErrNotFound)
		_ = lsmErr.Is(errors.New("other"))
		_ = lsmErr.Is(nil)
	}
	customErr := &lsmkvengine.LSMError{
		Code:    "custom",
		Message: "something failed",
		Err:     errors.New("inner error"),
	}
	if customErr.Error() == "" || customErr.Unwrap() == nil {
		t.Fatalf("customErr formatting or unwrap failed")
	}

	var nilLSM *lsmkvengine.LSMError
	if nilLSM.Error() != "" || nilLSM.Unwrap() != nil || nilLSM.Is(nil) {
		t.Fatalf("nil LSMError should handle methods gracefully")
	}
}

func TestSSTableEdgeCases(t *testing.T) {
	fs := lsmkvengine.NewMemFS()

	// 1. Writer invalid limits
	f, _ := fs.Create("/limits.sst")
	w := lsmkvengine.NewSSTableWriter(f, 0) // bitsPerKey <= 0 uses default
	badKey := make([]byte, lsmkvengine.MaxKeyBytes+1)
	if err := w.Add(badKey, []byte("val"), false); !errors.Is(err, lsmkvengine.ErrKeyTooLarge) {
		t.Fatalf("expected ErrKeyTooLarge on writer")
	}
	badVal := make([]byte, lsmkvengine.MaxValueBytes+1)
	if err := w.Add([]byte("k"), badVal, false); !errors.Is(err, lsmkvengine.ErrValTooLarge) {
		t.Fatalf("expected ErrValTooLarge on writer")
	}

	// 2. Large flush to test multiple data blocks and index entries
	for i := 0; i < 50; i++ {
		k := []byte(fmt.Sprintf("key_%04d", i))
		v := make([]byte, 200) // 50 * 200 = 10000 bytes > 4096 blockSize
		_ = w.Add(k, v, false)
	}
	if err := w.Finish(); err != nil {
		t.Fatalf("Finish failed: %v", err)
	}
	_ = f.Close()

	// 3. Open and read from multi-block SSTable
	rf, err := fs.OpenFile("/limits.sst", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	r, err := lsmkvengine.OpenSSTableReader(rf)
	if err != nil {
		t.Fatalf("OpenSSTableReader failed: %v", err)
	}

	// Get existing key
	val, found, deleted, err := r.Get([]byte("key_0025"))
	if err != nil || !found || deleted || len(val) != 200 {
		t.Fatalf("Get key_0025 failed: found=%v, err=%v", found, err)
	}

	// Get key not in SSTable (smaller than min, between, or larger than max)
	_, found, _, err = r.Get([]byte("a_before"))
	if err != nil || found {
		t.Fatalf("expected not found for a_before")
	}
	_, found, _, err = r.Get([]byte("z_after"))
	if err != nil || found {
		t.Fatalf("expected not found for z_after")
	}
	_ = r.Close()

	// 4. SSTable with bad checksum in footer
	fCRC, _ := fs.Create("/badcrc.sst")
	wCRC := lsmkvengine.NewSSTableWriter(fCRC, 10)
	_ = wCRC.Add([]byte("k"), []byte("v"), false)
	_ = wCRC.Finish()
	_ = fCRC.Close()

	data, _ := lsmkvengine.ReadAll(fs, "/badcrc.sst")
	// corrupt footer CRC (last 8 bytes: 4 bytes crc, 4 bytes magic)
	data[len(data)-8] ^= 0xFF
	fCorrupt, _ := fs.Create("/badcrc.sst")
	_, _ = fCorrupt.Write(data)
	_ = fCorrupt.Close()

	rfCRC, _ := fs.OpenFile("/badcrc.sst", 0, 0)
	_, err = lsmkvengine.OpenSSTableReader(rfCRC)
	_ = rfCRC.Close()
	if err == nil {
		t.Fatalf("expected error on footer crc mismatch")
	}

	// 5. SSTable with bad data block CRC
	data, _ = lsmkvengine.ReadAll(fs, "/limits.sst")
	data[10] ^= 0xFF // corrupt first data block
	fCorruptBlock, _ := fs.Create("/badblock.sst")
	_, _ = fCorruptBlock.Write(data)
	_ = fCorruptBlock.Close()

	rfBlock, _ := fs.OpenFile("/badblock.sst", 0, 0)
	rBlock, err := lsmkvengine.OpenSSTableReader(rfBlock)
	if err != nil {
		t.Fatalf("open should succeed if footer is valid: %v", err)
	}
	_, _, _, err = rBlock.Get([]byte("key_0001"))
	if err == nil {
		t.Fatalf("expected error reading corrupted block")
	}
	_, err = rBlock.ReadAllEntries()
	if err == nil {
		t.Fatalf("expected error ReadAllEntries on corrupted block")
	}
	_ = rBlock.Close()
}

func TestWALEdgeCases(t *testing.T) {
	fs := lsmkvengine.NewMemFS()
	f, _ := fs.Create("/wal_limits.wal")
	w := lsmkvengine.NewWALWriter(f)

	badKey := make([]byte, lsmkvengine.MaxKeyBytes+1)
	if err := w.Append(1, badKey, []byte("v")); !errors.Is(err, lsmkvengine.ErrKeyTooLarge) {
		t.Fatalf("expected ErrKeyTooLarge on WAL append")
	}
	badVal := make([]byte, lsmkvengine.MaxValueBytes+1)
	if err := w.Append(1, []byte("k"), badVal); !errors.Is(err, lsmkvengine.ErrValTooLarge) {
		t.Fatalf("expected ErrValTooLarge on WAL append")
	}
	_ = w.Close()
}

func TestManifestSaveAndLevels(t *testing.T) {
	fs := lsmkvengine.NewMemFS()
	m, err := lsmkvengine.OpenManifest(fs, "/m_dir")
	if err != nil {
		t.Fatal(err)
	}
	id1 := m.AllocFileID()
	id2 := m.AllocFileID()
	if id2 != id1+1 {
		t.Fatalf("expected consecutive IDs")
	}
	if err := m.AddSSTable(0, id1); err != nil {
		t.Fatal(err)
	}
	if err := m.AddSSTable(1, id2); err != nil {
		t.Fatal(err)
	}
	levels := m.GetLevels()
	if len(levels[0]) != 1 || levels[0][0] != id1 {
		t.Fatalf("unexpected L0 levels: %v", levels[0])
	}
	if err := m.ReplaceLevel(0, []uint64{10, 11}); err != nil {
		t.Fatal(err)
	}
	levels = m.GetLevels()
	if len(levels[0]) != 2 || levels[0][0] != 10 {
		t.Fatalf("unexpected replaced L0: %v", levels[0])
	}
}

func TestDBOpenRecoveryAndCompaction(t *testing.T) {
	fs := lsmkvengine.NewMemFS()
	opts := lsmkvengine.Options{
		FSRef:         fs,
		MemtableBytes: 200,
	}
	db, err := lsmkvengine.Open("/compact_test", opts)
	if err != nil {
		t.Fatal(err)
	}

	// Trigger 4 flushes to induce L0 -> L1 compaction
	for i := 0; i < 40; i++ {
		k := []byte(fmt.Sprintf("k_%03d", i))
		v := []byte(fmt.Sprintf("v_%03d_with_padding_data_to_fill_memtable", i))
		_ = db.Put(k, v)
	}

	// Read keys back
	for i := 0; i < 40; i++ {
		k := []byte(fmt.Sprintf("k_%03d", i))
		expV := fmt.Sprintf("v_%03d_with_padding_data_to_fill_memtable", i)
		v, err := db.Get(k)
		if err != nil || string(v) != expV {
			t.Fatalf("failed to get key %s: v=%s, err=%v", string(k), string(v), err)
		}
	}

	_ = db.Close()

	// Reopen DB with L1 tables loaded from manifest
	reopened, err := lsmkvengine.Open("/compact_test", opts)
	if err != nil {
		t.Fatalf("reopen after compaction failed: %v", err)
	}
	defer reopened.Close()

	// Verify keys exist after reopening from L1 SSTables
	for i := 0; i < 40; i++ {
		k := []byte(fmt.Sprintf("k_%03d", i))
		expV := fmt.Sprintf("v_%03d_with_padding_data_to_fill_memtable", i)
		v, err := reopened.Get(k)
		if err != nil || string(v) != expV {
			t.Fatalf("failed to get key %s on reopen: v=%s, err=%v", string(k), string(v), err)
		}
	}

	// Iterator over reopened DB
	it, err := reopened.NewIterator(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for it.Valid() {
		count++
		it.Next()
	}
	_ = it.Close()
	if count != 40 {
		t.Fatalf("expected 40 items in iterator, got %d", count)
	}
}
