package cmd

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmtdbm "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedactTOML(t *testing.T) {
	input := `# a comment
moniker = "node-1"
persistent_peers = "abc@1.2.3.4:26656,def@5.6.7.8:26656"
seeds = "seed@9.9.9.9:26656"
external_address = "1.2.3.4:26656"
some_secret = "hunter2"
api_token = "tok_123"
pruning = "default"
[telemetry]
enabled = true`

	got := string(redactTOML([]byte(input)))

	// Comments and non-sensitive values are preserved.
	assert.Contains(t, got, `# a comment`)
	assert.Contains(t, got, `moniker = "node-1"`)
	assert.Contains(t, got, `pruning = "default"`)
	assert.Contains(t, got, `enabled = true`)
	assert.Contains(t, got, `[telemetry]`)

	// Sensitive values are blanked, keys preserved.
	for _, key := range []string{"persistent_peers", "seeds", "external_address", "some_secret", "api_token"} {
		assert.Contains(t, got, key+` = "[REDACTED]"`, "expected %q to be redacted", key)
	}
	// No real secret material leaks.
	assert.NotContains(t, got, "hunter2")
	assert.NotContains(t, got, "tok_123")
	assert.NotContains(t, got, "1.2.3.4")
}

func TestTailLines(t *testing.T) {
	t.Run("fewer lines than requested", func(t *testing.T) {
		path := writeTempFile(t, "a\nb\nc\n")
		lines, err := tailLines(path, 10)
		require.NoError(t, err)
		assert.Equal(t, []string{"a", "b", "c"}, lines)
	})

	t.Run("more lines than requested", func(t *testing.T) {
		path := writeTempFile(t, "a\nb\nc\nd\ne\n")
		lines, err := tailLines(path, 2)
		require.NoError(t, err)
		assert.Equal(t, []string{"d", "e"}, lines)
	})

	t.Run("spanning multiple chunks", func(t *testing.T) {
		var sb strings.Builder
		const total = 5000 // each line ~30 bytes => >64KiB, forces multiple chunks
		for i := range total {
			fmt.Fprintf(&sb, "line-%05d-padding-padding\n", i)
		}
		path := writeTempFile(t, sb.String())
		lines, err := tailLines(path, 3)
		require.NoError(t, err)
		require.Len(t, lines, 3)
		assert.Equal(t, "line-04999-padding-padding", lines[2])
		assert.Equal(t, "line-04997-padding-padding", lines[0])
	})
}

func TestHumanizeBytes(t *testing.T) {
	assert.Equal(t, "512 B", humanizeBytes(512))
	assert.Equal(t, "1.0 KB", humanizeBytes(1024))
	assert.Equal(t, "1.5 KB", humanizeBytes(1536))
	assert.Equal(t, "1.0 MB", humanizeBytes(1024*1024))
}

// TestAppHashMismatchCollect builds a temp node home with on-disk comet
// blockstore data, runs the collect command, and asserts the archive contents,
// key exclusions, and SHA256SUMS integrity.
func TestAppHashMismatchCollect(t *testing.T) {
	home := t.TempDir()
	dataDir := filepath.Join(home, "data")
	configDir := filepath.Join(home, "config")
	require.NoError(t, os.MkdirAll(dataDir, 0o755))
	require.NoError(t, os.MkdirAll(configDir, 0o755))

	const mismatchHeight = int64(100)
	appHashH := []byte{0xde, 0xad, 0xbe, 0xef}

	// Write config files (with secrets) and private key files (must be excluded).
	writeFile(t, filepath.Join(configDir, "config.toml"), "moniker = \"n1\"\npersistent_peers = \"p@1.2.3.4:26656\"\n")
	writeFile(t, filepath.Join(configDir, "app.toml"), "minimum-gas-prices = \"0utia\"\napi_token = \"tok_secret\"\n")
	writeFile(t, filepath.Join(configDir, "client.toml"), "chain-id = \"test\"\n")
	writeFile(t, filepath.Join(configDir, "priv_validator_key.json"), `{"private":"SECRET_KEY"}`)
	writeFile(t, filepath.Join(configDir, "node_key.json"), `{"priv_key":"SECRET_NODE_KEY"}`)

	// Populate the comet blockstore with contiguous blocks H-2, H-1, H.
	blockDB, err := cmtdbm.NewDB("blockstore", cmtdbm.GoLevelDBBackend, dataDir)
	require.NoError(t, err)
	blockStore := store.NewBlockStore(blockDB)
	for _, h := range []int64{mismatchHeight - 2, mismatchHeight - 1, mismatchHeight} {
		block := types.MakeBlock(h, types.Data{Txs: []types.Tx{{0x1, 0x2}}}, new(types.Commit), nil)
		block.ChainID = "test-chain"
		block.ProposerAddress = make([]byte, 20) // valid length so LoadBlockMeta succeeds
		if h == mismatchHeight {
			block.AppHash = appHashH
		} else {
			block.AppHash = []byte{byte(h)}
		}
		parts, err := block.MakePartSet(types.BlockPartSizeBytes)
		require.NoError(t, err)
		blockStore.SaveBlock(block, parts, &types.Commit{Height: h})
	}
	require.NoError(t, blockDB.Close())

	// Run the collect command via its RunE with a manually-injected server context.
	out := filepath.Join(t.TempDir(), "archive.tar.gz")
	cmd := appHashMismatchCollectCmd()
	var stderr strings.Builder
	cmd.SetOut(io.Discard)
	cmd.SetErr(&stderr)
	t.Cleanup(func() { t.Logf("collector stderr:\n%s", stderr.String()) })

	sctx := server.NewDefaultContext()
	sctx.Config.SetRoot(home)
	sctx.Config.DBBackend = "goleveldb"
	sctx.Viper = viper.New()
	sctx.Viper.Set("pruning", "default") // required by DefaultBaseappOptions when building the app
	ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
	cmd.SetContext(ctx)

	require.NoError(t, cmd.Flags().Set(flags.FlagHome, home))
	require.NoError(t, cmd.Flags().Set(flagHeight, fmt.Sprintf("%d", mismatchHeight)))
	require.NoError(t, cmd.Flags().Set(flagOutput, out))

	require.NoError(t, cmd.Execute())

	// Inspect the archive.
	entries, sums := readArchive(t, out)

	// Required entries are present.
	for _, name := range []string{
		"manifest.json", "mismatch.json", "store_hashes.json", "SHA256SUMS",
		"block_100.pb", "block_99.pb", "block_98.pb",
		"config/config.toml", "config/app.toml", "config/client.toml",
	} {
		_, ok := entries[name]
		assert.Truef(t, ok, "expected archive to contain %q", name)
	}

	// Private keys must never be included.
	for name := range entries {
		assert.NotContains(t, name, "priv_validator_key.json")
		assert.NotContains(t, name, "node_key.json")
	}

	// Config redaction reached the archive.
	assert.NotContains(t, string(entries["config/config.toml"]), "1.2.3.4")
	assert.Contains(t, string(entries["config/config.toml"]), `persistent_peers = "[REDACTED]"`)
	assert.NotContains(t, string(entries["config/app.toml"]), "tok_secret")

	// mismatch.json captured the block-H app hash.
	assert.Contains(t, string(entries["mismatch.json"]), strings.ToUpper("deadbeef"))

	// SHA256SUMS covers every other entry and each sum is correct.
	require.NotEmpty(t, sums)
	for name, data := range entries {
		if name == "SHA256SUMS" {
			continue
		}
		want := fmt.Sprintf("%x", sha256.Sum256(data))
		got, ok := sums[name]
		require.Truef(t, ok, "SHA256SUMS missing entry for %q", name)
		assert.Equalf(t, want, got, "checksum mismatch for %q", name)
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "f.txt")
	writeFile(t, path, content)
	return path
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

// readArchive returns a map of archive entry name -> contents, plus the parsed
// SHA256SUMS as name -> hex sum.
func readArchive(t *testing.T, path string) (map[string][]byte, map[string]string) {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	gz, err := gzip.NewReader(f)
	require.NoError(t, err)
	tr := tar.NewReader(gz)

	entries := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		data, err := io.ReadAll(tr)
		require.NoError(t, err)
		entries[hdr.Name] = data
	}

	sums := map[string]string{}
	for line := range strings.SplitSeq(string(entries["SHA256SUMS"]), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "  ", 2)
		require.Len(t, parts, 2, "malformed SHA256SUMS line: %q", line)
		sums[parts[1]] = parts[0]
	}
	return entries, sums
}
