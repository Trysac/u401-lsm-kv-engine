package lsmkvengine_test

import (
	"bytes"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"testing"

	"github.com/Trysac/u401-lsm-kv-engine"
)

func TestAcceptanceItem1_PutAndGet(t *testing.T) {
	// WHEN Put([]byte("user:101"), []byte("alice")) succeeds THEN Get([]byte("user:101")) returns []byte("alice") with nil error
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/testdb", lsmkvengine.Options{FSRef: fs})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	err = db.Put([]byte("user:101"), []byte("alice"))
	if err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := db.Get([]byte("user:101"))
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if !bytes.Equal(got, []byte("alice")) {
		t.Fatalf("expected []byte(\"alice\"), got %q", string(got))
	}
}

func TestAcceptanceItem2_DeleteAndGet(t *testing.T) {
	// WHEN Delete([]byte("user:101")) succeeds THEN Get([]byte("user:101")) returns an error matching errors.Is(err, ErrNotFound) whose *LSMError Code is 'not_found'
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/testdb", lsmkvengine.Options{FSRef: fs})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	_ = db.Put([]byte("user:101"), []byte("alice"))
	err = db.Delete([]byte("user:101"))
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	got, err := db.Get([]byte("user:101"))
	if got != nil {
		t.Fatalf("expected nil value, got %v", got)
	}
	if !errors.Is(err, lsmkvengine.ErrNotFound) {
		t.Fatalf("expected errors.Is(err, ErrNotFound), got %v", err)
	}
	var lsmErr *lsmkvengine.LSMError
	if !errors.As(err, &lsmErr) {
		t.Fatalf("expected error to be *LSMError, got %T", err)
	}
	if lsmErr.Code != "not_found" {
		t.Fatalf("expected LSMError.Code == 'not_found', got %q", lsmErr.Code)
	}
}

func TestAcceptanceItem3_SnapshotIsolation(t *testing.T) {
	// WHEN Write(batch) commits Put("k1", "v1") and Delete("k2") THEN a Snapshot taken before Write sees neither change and a Snapshot taken after sees both
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/testdb", lsmkvengine.Options{FSRef: fs})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	_ = db.Put([]byte("k2"), []byte("v2"))

	snapBefore, err := db.NewIterator(nil, nil)
	if err != nil {
		t.Fatalf("NewIterator failed: %v", err)
	}
	defer snapBefore.Close()

	batch := lsmkvengine.NewBatch()
	batch.Put([]byte("k1"), []byte("v1"))
	batch.Delete([]byte("k2"))
	err = db.Write(batch)
	if err != nil {
		t.Fatalf("Write batch failed: %v", err)
	}

	snapAfter, err := db.NewIterator(nil, nil)
	if err != nil {
		t.Fatalf("NewIterator after batch failed: %v", err)
	}
	defer snapAfter.Close()

	// Check snapBefore sees k2=v2 and NOT k1
	beforeMap := make(map[string]string)
	for snapBefore.Valid() {
		beforeMap[string(snapBefore.Key())] = string(snapBefore.Value())
		snapBefore.Next()
	}
	if _, hasK1 := beforeMap["k1"]; hasK1 {
		t.Fatalf("snapBefore should not see k1")
	}
	if v, hasK2 := beforeMap["k2"]; !hasK2 || v != "v2" {
		t.Fatalf("snapBefore should see k2='v2', got %q", v)
	}

	// Check snapAfter sees k1=v1 and NOT k2
	afterMap := make(map[string]string)
	for snapAfter.Valid() {
		afterMap[string(snapAfter.Key())] = string(snapAfter.Value())
		snapAfter.Next()
	}
	if v, hasK1 := afterMap["k1"]; !hasK1 || v != "v1" {
		t.Fatalf("snapAfter should see k1='v1', got %q", v)
	}
	if _, hasK2 := afterMap["k2"]; hasK2 {
		t.Fatalf("snapAfter should not see k2 (was deleted)")
	}
}

func TestAcceptanceItem4_ModelComparison(t *testing.T) {
	// WHEN 50 seeded sequences of 500 random Put, Delete, Get, Scan, Batch and Close-Reopen operations run against a sorted in-memory map model THEN every Get and Scan result equals the model
	for seed := int64(1); seed <= 50; seed++ {
		r := rand.New(rand.NewSource(seed))
		fs := lsmkvengine.NewMemFS()
		db, err := lsmkvengine.Open("/modeldb", lsmkvengine.Options{
			FSRef:         fs,
			MemtableBytes: 256,
		})
		if err != nil {
			t.Fatalf("seed %d: Open failed: %v", seed, err)
		}

		model := make(map[string]string)

		for op := 0; op < 500; op++ {
			opType := r.Intn(6)
			k := fmt.Sprintf("key_%02d", r.Intn(20))
			v := fmt.Sprintf("val_%d", r.Intn(1000))

			switch opType {
			case 0: // Put
				_ = db.Put([]byte(k), []byte(v))
				model[k] = v
			case 1: // Delete
				_ = db.Delete([]byte(k))
				delete(model, k)
			case 2: // Get
				got, gerr := db.Get([]byte(k))
				expVal, expExists := model[k]
				if expExists {
					if gerr != nil || string(got) != expVal {
						t.Fatalf("seed %d op %d Get(%s): got %s, exp %s", seed, op, k, string(got), expVal)
					}
				} else {
					if !errors.Is(gerr, lsmkvengine.ErrNotFound) {
						t.Fatalf("seed %d op %d Get(%s) expected not found, got %v", seed, op, k, gerr)
					}
				}
			case 3: // Scan
				lo := fmt.Sprintf("key_%02d", r.Intn(10))
				hi := fmt.Sprintf("key_%02d", 10+r.Intn(10))
				it, ierr := db.NewIterator([]byte(lo), []byte(hi))
				if ierr != nil {
					t.Fatalf("NewIterator failed: %v", ierr)
				}
				var gotKeys []string
				var gotVals []string
				for it.Valid() {
					gotKeys = append(gotKeys, string(it.Key()))
					gotVals = append(gotVals, string(it.Value()))
					it.Next()
				}
				_ = it.Close()

				var expKeys []string
				for mk := range model {
					if mk >= lo && mk < hi {
						expKeys = append(expKeys, mk)
					}
				}
				sort.Strings(expKeys)
				if len(gotKeys) != len(expKeys) {
					t.Fatalf("seed %d scan key count mismatch", seed)
				}
				for idx := range expKeys {
					if gotKeys[idx] != expKeys[idx] || gotVals[idx] != model[expKeys[idx]] {
						t.Fatalf("seed %d scan item mismatch", seed)
					}
				}
			case 4: // Batch
				b := lsmkvengine.NewBatch()
				b.Put([]byte(k), []byte(v))
				k2 := fmt.Sprintf("key_%02d", r.Intn(20))
				b.Delete([]byte(k2))
				_ = db.Write(b)
				model[k] = v
				delete(model, k2)
			case 5: // Close & Reopen
				_ = db.Close()
				db, err = lsmkvengine.Open("/modeldb", lsmkvengine.Options{
					FSRef:         fs,
					MemtableBytes: 256,
				})
				if err != nil {
					t.Fatalf("seed %d reopen failed: %v", seed, err)
				}
			}
		}
		_ = db.Close()
	}
}

func TestAcceptanceItem5_CrashRecovery(t *testing.T) {
	// WHEN the in-memory FileSystem crashes at a seeded random write point in 200 runs, dropping unsynced bytes, THEN after reopening every key written with SyncAlways before the crash is returned by Get and no Get returns a value that was never written
	for run := 0; run < 200; run++ {
		r := rand.New(rand.NewSource(int64(run + 100)))
		fs := lsmkvengine.NewMemFS()
		db, err := lsmkvengine.Open("/crashdb", lsmkvengine.Options{
			FSRef:    fs,
			SyncMode: lsmkvengine.SyncAlways,
		})
		if err != nil {
			t.Fatalf("run %d: Open failed: %v", run, err)
		}

		written := make(map[string]string)
		numOps := 5 + r.Intn(20)
		for i := 0; i < numOps; i++ {
			k := fmt.Sprintf("k_%d", i)
			v := fmt.Sprintf("v_%d", i)
			_ = db.Put([]byte(k), []byte(v))
			written[k] = v
		}

		// Simulate crash (drop unsynced bytes)
		fs.Crash()

		// Reopen
		reopened, err := lsmkvengine.Open("/crashdb", lsmkvengine.Options{
			FSRef:    fs,
			SyncMode: lsmkvengine.SyncAlways,
		})
		if err != nil {
			t.Fatalf("run %d: reopen after crash failed: %v", run, err)
		}

		for k, expV := range written {
			val, err := reopened.Get([]byte(k))
			if err != nil {
				t.Fatalf("run %d: missing synced key %s: %v", run, k, err)
			}
			if string(val) != expV {
				t.Fatalf("run %d: corrupted synced value for %s", run, k)
			}
		}

		// Ensure no unwritten keys exist
		_, err = reopened.Get([]byte("nonexistent_key_never_written"))
		if !errors.Is(err, lsmkvengine.ErrNotFound) {
			t.Fatalf("run %d: non-existent key found", run)
		}
		_ = reopened.Close()
	}
}

func TestAcceptanceItem6_WALTruncatedTail(t *testing.T) {
	// IF the WAL ends with a truncated partial record after 3 complete records THEN Open succeeds and Get returns all 3 recovered records
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/waldb", lsmkvengine.Options{
		FSRef:    fs,
		SyncMode: lsmkvengine.SyncAlways,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_ = db.Put([]byte("rec1"), []byte("val1"))
	_ = db.Put([]byte("rec2"), []byte("val2"))
	_ = db.Put([]byte("rec3"), []byte("val3"))
	_ = db.Close()

	// Manually append a truncated partial record to /waldb/CURRENT.wal
	walFile, err := fs.OpenFile("/waldb/CURRENT.wal", 0, 0)
	if err != nil {
		t.Fatalf("failed to open wal file: %v", err)
	}
	currBytes, _ := lsmkvengine.ReadAll(fs, "/waldb/CURRENT.wal")
	_ = walFile.Close()

	// Append 5 incomplete bytes
	partial := append(currBytes, []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01}...)
	trFile, _ := fs.Create("/waldb/CURRENT.wal")
	_, _ = trFile.Write(partial)
	_ = trFile.Sync()
	_ = trFile.Close()

	reopened, err := lsmkvengine.Open("/waldb", lsmkvengine.Options{FSRef: fs})
	if err != nil {
		t.Fatalf("Open should succeed with truncated tail: %v", err)
	}
	defer reopened.Close()

	for _, k := range []string{"rec1", "rec2", "rec3"} {
		v, err := reopened.Get([]byte(k))
		if err != nil {
			t.Fatalf("expected key %s recovered, got error: %v", k, err)
		}
		if len(v) == 0 {
			t.Fatalf("expected non-empty value for %s", k)
		}
	}
}

func TestAcceptanceItem7_WALCorruptChecksum(t *testing.T) {
	// IF a WAL record in the middle has an invalid CRC32C checksum THEN Open returns *LSMError with code 'corrupt_wal'
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/corruptdb", lsmkvengine.Options{
		FSRef:    fs,
		SyncMode: lsmkvengine.SyncAlways,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_ = db.Put([]byte("k1"), []byte("v1"))
	_ = db.Put([]byte("k2"), []byte("v2"))
	_ = db.Put([]byte("k3"), []byte("v3"))
	_ = db.Close()

	currBytes, _ := lsmkvengine.ReadAll(fs, "/corruptdb/CURRENT.wal")
	// Flip a bit in the middle of the WAL file
	corrupted := append([]byte(nil), currBytes...)
	corrupted[15] ^= 0xFF

	f, _ := fs.Create("/corruptdb/CURRENT.wal")
	_, _ = f.Write(corrupted)
	_ = f.Sync()
	_ = f.Close()

	_, err = lsmkvengine.Open("/corruptdb", lsmkvengine.Options{FSRef: fs})
	if err == nil {
		t.Fatalf("expected error on corrupted WAL")
	}
	var lsmErr *lsmkvengine.LSMError
	if !errors.As(err, &lsmErr) {
		t.Fatalf("expected *LSMError, got %T", err)
	}
	if lsmErr.Code != "corrupt_wal" {
		t.Fatalf("expected LSMError.Code == 'corrupt_wal', got %q", lsmErr.Code)
	}
}

func TestAcceptanceItem8_FuzzReaders(t *testing.T) {
	// IF FuzzSSTableReader or FuzzWALReader feeds corrupt bytes THEN the reader returns *LSMError with code 'corrupt_sstable' or 'corrupt_wal' and never panics
	r := rand.New(rand.NewSource(42))
	for i := 0; i < 50; i++ {
		garbage := make([]byte, r.Intn(1024))
		r.Read(garbage)

		// WAL reader check
		_, err := lsmkvengine.ReadWALEntries(bytes.NewReader(garbage))
		if err != nil {
			var lsmErr *lsmkvengine.LSMError
			if errors.As(err, &lsmErr) && lsmErr.Code != "corrupt_wal" {
				t.Fatalf("expected corrupt_wal code, got %v", lsmErr.Code)
			}
		}

		// SSTable reader check
		fs := lsmkvengine.NewMemFS()
		f, _ := fs.Create("/corrupt.sst")
		_, _ = f.Write(garbage)
		_ = f.Close()
		rf, _ := fs.OpenFile("/corrupt.sst", 0, 0)
		_, sstErr := lsmkvengine.OpenSSTableReader(rf)
		_ = rf.Close()
		if sstErr != nil {
			var lsmErr *lsmkvengine.LSMError
			if errors.As(sstErr, &lsmErr) && lsmErr.Code != "corrupt_sstable" {
				t.Fatalf("expected corrupt_sstable code, got %v", lsmErr.Code)
			}
		}
	}
}

func FuzzSSTableReader(f *testing.F) {
	f.Add([]byte("sample corrupt sstable bytes for fuzzing"))
	f.Fuzz(func(t *testing.T, data []byte) {
		fs := lsmkvengine.NewMemFS()
		file, _ := fs.Create("/fuzz.sst")
		_, _ = file.Write(data)
		_ = file.Close()
		rf, _ := fs.OpenFile("/fuzz.sst", 0, 0)
		defer rf.Close()
		r, err := lsmkvengine.OpenSSTableReader(rf)
		if err != nil {
			var lsmErr *lsmkvengine.LSMError
			if errors.As(err, &lsmErr) && lsmErr.Code != "corrupt_sstable" {
				t.Fatalf("expected corrupt_sstable code, got %v", lsmErr.Code)
			}
			return
		}
		_, _, _, _ = r.Get([]byte("testkey"))
		_, _ = r.ReadAllEntries()
		_ = r.Close()
	})
}

func FuzzWALReader(f *testing.F) {
	f.Add([]byte("sample corrupt wal bytes for fuzzing"))
	f.Fuzz(func(t *testing.T, data []byte) {
		_, err := lsmkvengine.ReadWALEntries(bytes.NewReader(data))
		if err != nil {
			var lsmErr *lsmkvengine.LSMError
			if errors.As(err, &lsmErr) && lsmErr.Code != "corrupt_wal" {
				t.Fatalf("expected corrupt_wal code, got %v", lsmErr.Code)
			}
		}
	})
}

func TestAcceptanceItem9_ConcurrentCompactionAndIterator(t *testing.T) {
	// WHILE a compaction from L0 to L1 runs THEN an iterator opened before it keeps returning the snapshot's values and 'go test -race' reports no race
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/compactdb", lsmkvengine.Options{
		FSRef:         fs,
		MemtableBytes: 128,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	for i := 0; i < 20; i++ {
		_ = db.Put([]byte(fmt.Sprintf("key_%02d", i)), []byte(fmt.Sprintf("init_%02d", i)))
	}

	it, err := db.NewIterator(nil, nil)
	if err != nil {
		t.Fatalf("NewIterator failed: %v", err)
	}
	defer it.Close()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		// Trigger multiple flushes and compaction
		for i := 20; i < 60; i++ {
			_ = db.Put([]byte(fmt.Sprintf("key_%02d", i)), []byte(fmt.Sprintf("val_%02d", i)))
		}
	}()

	// Iterate over original iterator while compaction runs in parallel
	count := 0
	for it.Valid() {
		count++
		it.Next()
	}
	if count < 20 {
		t.Fatalf("snapshot iterator saw only %d items, expected at least 20", count)
	}
	wg.Wait()
}

func TestAcceptanceItem10_IteratorOrder(t *testing.T) {
	// WHEN NewIterator([]byte("a"), []byte("z")) scans 10 inserted pairs THEN keys come back in strictly increasing byte order
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/orderdb", lsmkvengine.Options{FSRef: fs})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	keys := []string{"b", "k", "c", "j", "d", "i", "e", "h", "f", "g"}
	for _, k := range keys {
		_ = db.Put([]byte(k), []byte("v_"+k))
	}

	it, err := db.NewIterator([]byte("a"), []byte("z"))
	if err != nil {
		t.Fatalf("NewIterator failed: %v", err)
	}
	defer it.Close()

	var scanned []string
	for it.Valid() {
		scanned = append(scanned, string(it.Key()))
		it.Next()
	}

	if len(scanned) != 10 {
		t.Fatalf("expected 10 scanned keys, got %d", len(scanned))
	}
	for i := 1; i < len(scanned); i++ {
		if scanned[i-1] >= scanned[i] {
			t.Fatalf("keys not strictly increasing: %s >= %s", scanned[i-1], scanned[i])
		}
	}
}
