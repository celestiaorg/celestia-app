package appd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPruneStaleBinaries(t *testing.T) {
	home := t.TempDir()
	original := nodeHome
	nodeHome = home
	t.Cleanup(func() { nodeHome = original })

	binDir := filepath.Join(home, "bin")
	for _, name := range []string{"v9.0.8", "v9.0.7", "v3.12.0", ".v9.0.8.tmp-123", ".backup.stale"} {
		require.NoError(t, os.MkdirAll(filepath.Join(binDir, name), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(binDir, name, "celestia-appd"), []byte("binary"), 0o700))
	}
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "README.md"), nil, 0o600))

	external := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(external, "celestia-appd"), nil, 0o700))
	require.NoError(t, os.Symlink(external, filepath.Join(binDir, "v1.0.0")))

	removed, err := PruneStaleBinaries([]string{"v9.0.8", "v3.12.0"})
	require.NoError(t, err)
	require.Equal(t, []string{filepath.Join(binDir, "v9.0.7")}, removed)
	require.NoDirExists(t, filepath.Join(binDir, "v9.0.7"))
	for _, name := range []string{"v9.0.8", "v3.12.0", ".v9.0.8.tmp-123", ".backup.stale"} {
		require.FileExists(t, filepath.Join(binDir, name, "celestia-appd"))
	}
	require.FileExists(t, filepath.Join(binDir, "README.md"))
	require.FileExists(t, filepath.Join(external, "celestia-appd"))
	_, err = os.Lstat(filepath.Join(binDir, "v1.0.0"))
	require.NoError(t, err)
	removed, err = PruneStaleBinaries([]string{"v9.0.8", "v3.12.0"})
	require.NoError(t, err)
	require.Empty(t, removed)
}

func TestPruneStaleBinariesMissingDir(t *testing.T) {
	original := nodeHome
	nodeHome = t.TempDir()
	t.Cleanup(func() { nodeHome = original })

	removed, err := PruneStaleBinaries([]string{"v9.0.8"})
	require.NoError(t, err)
	require.Empty(t, removed)
}

func TestPruneStaleBinariesReadError(t *testing.T) {
	original := nodeHome
	nodeHome = t.TempDir()
	t.Cleanup(func() { nodeHome = original })
	require.NoError(t, os.WriteFile(filepath.Join(nodeHome, "bin"), nil, 0o600))
	removed, err := PruneStaleBinaries(nil)
	require.Error(t, err)
	require.Empty(t, removed)
}

func TestPruneStaleBinariesContinuesAfterRemoveError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	original := nodeHome
	nodeHome = t.TempDir()
	t.Cleanup(func() { nodeHome = original })
	binDir := filepath.Join(nodeHome, "bin")
	blocked := filepath.Join(binDir, "v3.0.0")
	require.NoError(t, os.MkdirAll(filepath.Join(blocked, "lib"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(blocked, "lib", "celestia-appd"), nil, 0o700))
	require.NoError(t, os.Chmod(filepath.Join(blocked, "lib"), 0o500))
	staged := filepath.Join(binDir, ".v3.0.0.stale")
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(staged, "lib"), 0o755) })
	removable := filepath.Join(binDir, "v4.0.0")
	require.NoError(t, os.MkdirAll(removable, 0o755))

	removed, err := PruneStaleBinaries(nil)
	require.ErrorContains(t, err, staged)
	require.Equal(t, []string{removable}, removed)
	require.NoDirExists(t, removable)
	// A partially removed version must not look like a complete extraction.
	require.NoDirExists(t, blocked)
	require.FileExists(t, filepath.Join(staged, "lib", "celestia-appd"))

	require.NoError(t, os.Chmod(filepath.Join(staged, "lib"), 0o755))
	removed, err = PruneStaleBinaries(nil)
	require.NoError(t, err)
	require.Equal(t, []string{staged}, removed)
	require.NoDirExists(t, staged)
}
