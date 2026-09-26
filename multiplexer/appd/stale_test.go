package appd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStaleBinaries(t *testing.T) {
	home := t.TempDir()
	original := nodeHome
	nodeHome = home
	t.Cleanup(func() { nodeHome = original })

	binDir := filepath.Join(home, "bin")
	for _, name := range []string{"v9.0.8", "v9.0.7", "v3.12.0", ".v9.0.8.tmp-123"} {
		require.NoError(t, os.MkdirAll(filepath.Join(binDir, name), 0o755))
	}
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "README.md"), nil, 0o600))

	stale, err := StaleBinaries([]string{"v9.0.8"})
	require.NoError(t, err)
	require.ElementsMatch(t, []string{filepath.Join(binDir, "v9.0.7"), filepath.Join(binDir, "v3.12.0")}, stale)
	require.DirExists(t, filepath.Join(binDir, "v9.0.7"))
}

func TestStaleBinariesMissingDir(t *testing.T) {
	original := nodeHome
	nodeHome = t.TempDir()
	t.Cleanup(func() { nodeHome = original })

	stale, err := StaleBinaries([]string{"v9.0.8"})
	require.NoError(t, err)
	require.Empty(t, stale)
}
