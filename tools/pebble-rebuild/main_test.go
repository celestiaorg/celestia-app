package main

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cockroachdb/pebble"
)

// TestRebuildRoundTrip writes a source store, rebuilds it into a fresh store and
// asserts the rebuild copied every key faithfully (same count + byte total),
// which is exactly the invariant the --verify gate checks before the operator is
// allowed to delete the original store.
func TestRebuildRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	const n = 20000
	var wantBytes int64
	db, err := pebble.Open(src, &pebble.Options{})
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	for i := 0; i < n; i++ {
		k := []byte(fmt.Sprintf("k%08d", i))
		v := []byte(fmt.Sprintf("val-%d-payload", i))
		if err := db.Set(k, v, pebble.NoSync); err != nil {
			t.Fatalf("set: %v", err)
		}
		wantBytes += int64(len(k) + len(v))
	}
	if err := db.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	res, err := rebuild(src, dst, 0, 4<<20, 3600, false)
	if err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if res.keys != n {
		t.Fatalf("copied keys = %d, want %d", res.keys, n)
	}
	if res.bytes != wantBytes {
		t.Fatalf("copied bytes = %d, want %d", res.bytes, wantBytes)
	}

	// The independent verify path must agree.
	gotKeys, gotBytes, err := scanStore(dst, false, 3600)
	if err != nil {
		t.Fatalf("scanStore: %v", err)
	}
	if gotKeys != n || gotBytes != wantBytes {
		t.Fatalf("verify dst keys=%d bytes=%d, want keys=%d bytes=%d", gotKeys, gotBytes, n, wantBytes)
	}

	// Rebuilt store must stay at a compatible format (openable by older binaries).
	if res.dstFormat != pebble.FormatMostCompatible.String() {
		t.Errorf("dst format = %q, want %q (no format ratcheting)", res.dstFormat, pebble.FormatMostCompatible.String())
	}
}
