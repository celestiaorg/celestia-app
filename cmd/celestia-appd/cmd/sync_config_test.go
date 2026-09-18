package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/server"
	servercmd "github.com/cosmos/cosmos-sdk/server/cmd"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestMergeConfig(t *testing.T) {
	reference := []byte("# Name\nmoniker = 'default'\n# RPC\n[rpc]\n# Limit\nlimit = 20\n# Enabled\nenabled = true\n# Address\naddress = 'localhost'\n# Storage\n[storage]\n# Compact\ncompact = false\n")
	for _, original := range []string{
		"# operator\nmoniker='mine'\n[rpc] # rpc note\nlimit=0 # disabled\nenabled=false\naddress=''\nunknown='keep'\n",
		"moniker='mine'\n[\"rpc\"]\n\"limit\"=0\nenabled=false\naddress=''",
		"MONIKER='mine'\n[RPC]\nLIMIT=0\nENABLED=false\nADDRESS=''\n",
		"moniker='mine'\n[rpc]\nlimit=0\nenabled=false\naddress=''\nnotes='''\n[storage]\nnot a heading\n'''\n",
	} {
		t.Run(original, func(t *testing.T) {
			updated, added, err := mergeConfig([]byte(original), reference)
			require.NoError(t, err)
			require.Equal(t, []string{"storage.compact = false"}, added)
			var before, after map[string]any
			require.NoError(t, toml.Unmarshal([]byte(original), &before))
			require.NoError(t, toml.Unmarshal(updated, &after))
			before["storage"] = map[string]any{"compact": false}
			require.Equal(t, before, after)
			for _, comment := range []string{"# operator", "# rpc note", "# disabled"} {
				if strings.Contains(original, comment) {
					require.Contains(t, string(updated), comment)
				}
			}
			require.Contains(t, string(updated), "# Storage\n[storage]\n")
			require.Contains(t, string(updated), "# Compact\ncompact = false")
			second, added, err := mergeConfig(updated, reference)
			require.NoError(t, err)
			require.Empty(t, added)
			require.Equal(t, updated, second)
		})
	}
	t.Run("missing fields and sections", func(t *testing.T) {
		updated, added, err := mergeConfig([]byte("# personal\n[rpc]\nlimit=7 # custom\n"), reference)
		require.NoError(t, err)
		require.Equal(t, []string{"moniker = 'default'", "rpc.enabled = true", "rpc.address = 'localhost'", "storage.compact = false"}, added)
		require.Contains(t, string(updated), "# personal\n[rpc]\n")
		require.Contains(t, string(updated), "limit = 7  # custom\n")
		require.Contains(t, string(updated), "# Enabled\nenabled = true")
	})
	for _, original := range []string{"[rpc", "[rpc]\nlimit=1\nlimit=2", "rpc=3", "rpc={limit=0}", "RPC.limit=0", "[rpc.custom]\nlimit=0", "[[rpc]]\nlimit=0", "[rpc]\nLIMIT=0\nlimit=1"} {
		_, _, err := mergeConfig([]byte(original), reference)
		require.Error(t, err, original)
	}
}

func TestSyncConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("# operator\nmoniker='validator'\n[rpc]\nmax_concurrent_heavy_requests=3\n")
	require.NoError(t, os.WriteFile(path, original, 0o640))
	added, backup, err := syncConfigFile(path, true)
	require.NoError(t, err)
	require.NotEmpty(t, added)
	require.Empty(t, backup)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, data)
	added, backup, err = syncConfigFile(path, false)
	require.NoError(t, err)
	require.Contains(t, added, "storage.compact = false")
	require.Contains(t, added, fmt.Sprintf("storage.compaction_interval = %d", app.DefaultConsensusConfig().Storage.CompactionInterval))
	saved, err := os.ReadFile(backup)
	require.NoError(t, err)
	require.Equal(t, original, saved)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "max_concurrent_heavy_requests = 3")
	cfg, err := loadCometBFTConfig(path, t.TempDir())
	require.NoError(t, err)
	require.Equal(t, 3, cfg.RPC.MaxConcurrentHeavyRequests)
	require.Equal(t, app.DefaultConsensusConfig().P2P.SendRate, cfg.P2P.SendRate)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
	added, backup, err = syncConfigFile(path, false)
	require.NoError(t, err)
	require.Empty(t, added)
	require.Empty(t, backup)
	after, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, info.ModTime(), after.ModTime())
}

func TestSyncConfigRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"invalid", "readonly", "symlink", "hardlink"} {
		t.Run(kind, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			original := []byte("moniker='mine'\n")
			if kind == "invalid" {
				original = []byte("[broken")
			}
			require.NoError(t, os.WriteFile(path, original, 0o600))
			switch kind {
			case "readonly":
				require.NoError(t, os.Chmod(path, 0o400))
			case "symlink":
				link := path + ".link"
				require.NoError(t, os.Symlink(path, link))
				path = link
			case "hardlink":
				require.NoError(t, os.Link(path, path+".link"))
			}
			_, _, err := syncConfigFile(path, false)
			require.Error(t, err)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, data)
		})
	}
}

func TestSyncConfigStartup(t *testing.T) {
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, "config"), 0o700))
	path := filepath.Join(home, "config", "config.toml")
	cfg := app.DefaultConsensusConfig()
	cmtcfg.WriteConfigFile(path, cfg)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data = bytes.ReplaceAll(data, []byte("max_concurrent_heavy_requests = 20"), nil)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	sctx := server.NewDefaultContext()
	sctx.Config = cfg.SetRoot(home)
	// Runtime overrides must never be persisted.
	sctx.Config.RPC.MaxConcurrentHeavyRequests = 999
	cmd := &cobra.Command{}
	cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))
	require.NoError(t, syncConfigOnStart(cmd, log.NewNopLogger()))
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "max_concurrent_heavy_requests = 20")
	require.NotContains(t, string(data), "999")
	require.Equal(t, 999, sctx.Config.RPC.MaxConcurrentHeavyRequests)
	require.NoError(t, os.WriteFile(path, []byte("[broken"), 0o600))
	require.NoError(t, syncConfigOnStart(cmd, log.NewNopLogger()))
}

func TestSyncConfigCommandDryRun(t *testing.T) {
	if os.Getenv("CELESTIA_SYNC_CONFIG_TEST") == "1" {
		os.Args = append([]string{os.Args[0]}, os.Args[slices.Index(os.Args, "--")+1:]...)
		if err := servercmd.Execute(NewRootCmd(), app.EnvPrefix, app.NodeHome); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	for _, readonly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readonly=%t", readonly), func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "node")
			path := filepath.Join(home, "config", "config.toml")
			original := []byte("moniker='mine'\n")
			if readonly {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
				require.NoError(t, os.WriteFile(path, original, 0o400))
				require.NoError(t, os.Chmod(home, 0o500))
				t.Cleanup(func() { require.NoError(t, os.Chmod(home, 0o700)) })
			}
			child := exec.Command(os.Args[0], "-test.run=^TestSyncConfigCommandDryRun$", "--", "--trace", "--log-to-file", home+".log", "config", "sync", "--home", home, "--dry-run")
			child.Env = append(os.Environ(), "CELESTIA_SYNC_CONFIG_TEST=1")
			output, err := child.CombinedOutput()
			require.NoFileExists(t, home+".log")
			if !readonly {
				require.Error(t, err)
				require.NoDirExists(t, home)
				return
			}
			require.NoError(t, err, string(output))
			require.Contains(t, string(output), "storage.compact = false")
			require.NoDirExists(t, filepath.Join(home, "data"))
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, data)
			entries, err := os.ReadDir(filepath.Dir(path))
			require.NoError(t, err)
			require.Len(t, entries, 1)
		})
	}
}

func TestMergeConfigPreservesNaN(t *testing.T) {
	for _, value := range []string{"nan", "[nan]", "{value=nan}"} {
		original := []byte("custom=" + value + "\n")
		updated, added, err := mergeConfig(original, []byte("moniker='default'\n"))
		require.NoError(t, err, value)
		require.NotEmpty(t, added)
		require.Contains(t, string(updated), "nan")
	}
}

func TestMergeEmptyAndUnterminatedConfig(t *testing.T) {
	for _, original := range []string{"", "# comment", "[rpc]", "[rpc]\r\n# comment\r\n"} {
		updated, added, err := mergeConfig([]byte(original), []byte("[rpc]\n# Limit\nlimit=20\n"))
		require.NoError(t, err, original)
		require.Equal(t, []string{"rpc.limit = 20"}, added)
		for _, line := range strings.SplitAfter(original, "\n") {
			require.Contains(t, string(updated), strings.TrimSpace(line))
		}
	}
}

func TestSyncConfigPreservesEffectiveConfig(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "config.toml")
	original := []byte("# custom\nmoniker='validator'\n[p2p]\nsend_rate=123\n[mempool]\nttl-num-blocks=0\n[storage]\ncompact=false\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))
	before, err := loadCometBFTConfig(path, home)
	require.NoError(t, err)
	_, _, err = syncConfigFile(path, false)
	require.NoError(t, err)
	after, err := loadCometBFTConfig(path, home)
	require.NoError(t, err)
	// TOML represents an unset slice as an empty array; both mean no RPC servers.
	require.Empty(t, before.StateSync.RPCServers)
	require.Empty(t, after.StateSync.RPCServers)
	before.StateSync.RPCServers = after.StateSync.RPCServers
	require.Equal(t, before, after)
}

func TestSyncConfigWriteFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	original := []byte("moniker='mine'\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { require.NoError(t, os.Chmod(dir, 0o700)) })
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	_, _, err := syncConfigFile(path, false)
	require.Error(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, data)
}

func TestSyncConfigDoesNotPersistFlagsOrEnvironment(t *testing.T) {
	for _, source := range []string{"flag", "environment"} {
		t.Run(source, func(t *testing.T) {
			home := t.TempDir()
			require.NoError(t, os.Mkdir(filepath.Join(home, "config"), 0o700))
			path := filepath.Join(home, "config", "config.toml")
			require.NoError(t, os.WriteFile(path, []byte("moniker='mine'\n[rpc]\n"), 0o600))
			cmd := &cobra.Command{}
			cmd.Flags().String("home", home, "")
			cmd.Flags().Int("rpc.max_concurrent_heavy_requests", 20, "")
			executable, err := os.Executable()
			require.NoError(t, err)
			env := strings.NewReplacer(".", "_", "-", "_").Replace(strings.ToUpper(filepath.Base(executable))) + "_RPC_MAX_CONCURRENT_HEAVY_REQUESTS"
			t.Setenv(env, "77")
			want := 77
			if source == "flag" {
				require.NoError(t, cmd.Flags().Set("rpc.max_concurrent_heavy_requests", "88"))
				want = 88
			}
			sctx, err := server.InterceptConfigsAndCreateContext(cmd, "", app.DefaultAppConfig(), app.DefaultConsensusConfig())
			require.NoError(t, err)
			require.Equal(t, want, sctx.Config.RPC.MaxConcurrentHeavyRequests)
			cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))
			appPath := filepath.Join(home, "config", "app.toml")
			appBefore, err := os.ReadFile(appPath)
			require.NoError(t, err)
			require.NoError(t, syncConfigOnStart(cmd, log.NewNopLogger()))
			require.Equal(t, want, sctx.Config.RPC.MaxConcurrentHeavyRequests)
			persisted, err := loadCometBFTConfig(path, home)
			require.NoError(t, err)
			require.Equal(t, 20, persisted.RPC.MaxConcurrentHeavyRequests)
			appAfter, err := os.ReadFile(appPath)
			require.NoError(t, err)
			require.Equal(t, appBefore, appAfter)
		})
	}
}

func TestSyncConfigPreservesConcurrentEdit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("moniker='original'\n")
	require.NoError(t, os.WriteFile(path, original, 0o600))
	info, err := os.Stat(path)
	require.NoError(t, err)
	edited := []byte("moniker='edited'\n")
	require.NoError(t, os.WriteFile(path, edited, 0o600))
	_, err = replaceConfigFile(path, original, []byte("moniker='updated'\n"), info)
	require.ErrorContains(t, err, "config changed")
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, edited, contents)
}

func TestMergeConfigPreservesOrder(t *testing.T) {
	original := []byte("# storage first\n[storage]\ncompact=false # keep disabled\n# rpc second\n[rpc]\n# custom limit\nlimit=7\nunknown='keep'\n")
	reference := []byte("[rpc]\nlimit=20\n# New RPC setting\nenabled=true\n[storage]\ncompact=true\n")
	updated, _, err := mergeConfig(original, reference)
	require.NoError(t, err)
	previous := -1
	for _, text := range []string{"# storage first", "[storage]", "# keep disabled", "# rpc second", "[rpc]", "# custom limit", "limit = 7", "unknown = 'keep'", "# New RPC setting", "enabled = true"} {
		position := strings.Index(string(updated), text)
		require.Greater(t, position, previous, text)
		previous = position
	}
	// A complete file keeps its original whitespace, even if it is nonstandard.
	unchanged, added, err := mergeConfig(original, []byte("[rpc]\nlimit=20\n[storage]\ncompact=true\n"))
	require.NoError(t, err)
	require.Empty(t, added)
	require.Equal(t, original, unchanged)
}
