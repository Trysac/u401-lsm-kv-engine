package main

import (
	"path/filepath"
	"testing"
)

func TestCLICommands(t *testing.T) {
	tmpDir := t.TempDir()
	dbDir := filepath.Join(tmpDir, "testdb")
	t.Setenv("LSM_DIR", dbDir)

	// Usage error (no args)
	if code := run(nil); code != 2 {
		t.Fatalf("expected 2 for no args, got %d", code)
	}

	// Unknown subcommand
	if code := run([]string{"unknown"}); code != 2 {
		t.Fatalf("expected 2 for unknown cmd, got %d", code)
	}

	// Missing args
	if code := run([]string{"put"}); code != 2 {
		t.Fatalf("expected 2 for put with missing args, got %d", code)
	}
	if code := run([]string{"get"}); code != 2 {
		t.Fatalf("expected 2 for get with missing args, got %d", code)
	}
	if code := run([]string{"scan"}); code != 2 {
		t.Fatalf("expected 2 for scan with missing args, got %d", code)
	}

	// Stats
	if code := run([]string{"stats"}); code != 0 {
		t.Fatalf("expected 0 for stats, got %d", code)
	}

	// Put
	if code := run([]string{"put", "key1", "val1"}); code != 0 {
		t.Fatalf("expected 0 for put key1, got %d", code)
	}
	if code := run([]string{"put", "key2", "val2"}); code != 0 {
		t.Fatalf("expected 0 for put key2, got %d", code)
	}

	// Get
	if code := run([]string{"get", "key1"}); code != 0 {
		t.Fatalf("expected 0 for get key1, got %d", code)
	}
	// Get missing key
	if code := run([]string{"get", "nonexistent"}); code != 1 {
		t.Fatalf("expected 1 for get nonexistent, got %d", code)
	}

	// Scan
	if code := run([]string{"scan", "key1", "key3"}); code != 0 {
		t.Fatalf("expected 0 for scan, got %d", code)
	}

	// Check
	if code := run([]string{"check"}); code != 0 {
		t.Fatalf("expected 0 for check, got %d", code)
	}
}
