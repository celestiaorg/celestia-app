package fibre

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	pebbledb "github.com/cockroachdb/pebble/v2"
	"github.com/stretchr/testify/require"
)

func TestValueSeparation(t *testing.T) {
	dir := t.TempDir()
	opts := &pebbledb.Options{
		FormatMajorVersion: pebbledb.FormatValueSeparation,
	}
	opts.Experimental.ValueSeparationPolicy = func() pebbledb.ValueSeparationPolicy {
		return pebbledb.ValueSeparationPolicy{
			Enabled:               true,
			MinimumSize:           4096,
			MaxBlobReferenceDepth: 4,
			TargetGarbageRatio:    0.3,
			RewriteMinimumAge:     0,
		}
	}
	db, err := pebbledb.Open(dir, opts)
	require.NoError(t, err)

	for i := range 100 {
		key := []byte(fmt.Sprintf("/shard/%064d", i))
		val := make([]byte, 100_000)
		_, _ = rand.Read(val)
		require.NoError(t, db.Set(key, val, pebbledb.NoSync))
	}
	require.NoError(t, db.Flush())

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	var blobCount int
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".blob" {
			blobCount++
		}
	}
	t.Logf("Blob files: %d", blobCount)
	require.Greater(t, blobCount, 0, "expected blob files from value separation")
	require.NoError(t, db.Close())
}
