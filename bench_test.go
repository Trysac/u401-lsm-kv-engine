package lsmkvengine_test

import (
	"fmt"
	"testing"

	"github.com/Trysac/u401-lsm-kv-engine"
)

func BenchmarkPut(b *testing.B) {
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/benchdb", lsmkvengine.Options{
		FSRef:    fs,
		SyncMode: lsmkvengine.SyncNever,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = db.Put([]byte(fmt.Sprintf("k%d", i)), []byte("benchval"))
	}
}

func BenchmarkGet(b *testing.B) {
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/benchdb", lsmkvengine.Options{
		FSRef:    fs,
		SyncMode: lsmkvengine.SyncNever,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer db.Close()

	for i := 0; i < 1000; i++ {
		_ = db.Put([]byte(fmt.Sprintf("k%d", i)), []byte("benchval"))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = db.Get([]byte(fmt.Sprintf("k%d", i%1000)))
	}
}
