package lsmkvengine_test

import (
	"bytes"
	"testing"

	"github.com/Trysac/u401-lsm-kv-engine"
)

func TestEngineBasic(t *testing.T) {
	fs := lsmkvengine.NewMemFS()
	db, err := lsmkvengine.Open("/basicdb", lsmkvengine.Options{FSRef: fs})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	if err := db.Put([]byte("hello"), []byte("world")); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	got, err := db.Get([]byte("hello"))
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if !bytes.Equal(got, []byte("world")) {
		t.Fatalf("expected 'world', got %q", string(got))
	}
}
