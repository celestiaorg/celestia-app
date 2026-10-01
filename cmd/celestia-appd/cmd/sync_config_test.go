package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/pelletier/go-toml/v2"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func consensusReference(t *testing.T) []byte {
	reference, err := renderConsensusConfig()
	require.NoError(t, err)
	return reference
}

func TestMergeConfig(t *testing.T) {
	reference := []byte("# Name\nmoniker = 'default'\n# RPC\n[rpc]\n# Limit\nlimit = 20\n# Enabled\nenabled = true\n# Address\naddress = 'localhost'\n# Storage\n[storage]\n# Compact\ncompact = false\n")
	for _, original := range []string{
		"# operator\nmoniker='mine'\n[rpc] # rpc note\nlimit=0 # disabled\nenabled=false\naddress=''\nunknown='keep'\n",
		"moniker='mine'\nrpc.limit=0\nrpc.enabled=false\nrpc.address=''\n",
		"moniker='mine'\n[\"rpc\"]\n\"limit\"=0\nenabled=false\naddress=''",
		"MONIKER='mine'\n[RPC]\nLIMIT=0\nENABLED=false\nADDRESS=''\n",
		"moniker='mine'\n[rpc]\nlimit=0\nenabled=false\naddress=''\nnotes='''\n[storage]\nnot a heading\n'''\n",
	} {
		t.Run(original, func(t *testing.T) {
			updated, added, err := mergeConfig([]byte(original), reference)
			require.NoError(t, err)
			require.Equal(t, []string{"storage.compact"}, added)
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
		require.Equal(t, []string{"moniker", "rpc.enabled", "rpc.address", "storage.compact"}, added)
		require.Contains(t, string(updated), "# personal\n[rpc]\n")
		require.Contains(t, string(updated), "limit = 7  # custom\n")
		require.Contains(t, string(updated), "# Enabled\nenabled = true")
	})
	t.Run("dotted missing field", func(t *testing.T) {
		updated, _, err := mergeConfig([]byte("rpc.limit=0\n"), reference)
		require.NoError(t, err)
		require.Contains(t, string(updated), "rpc.enabled = true")
	})
	for _, original := range []string{"[rpc", "[rpc]\nlimit=1\nlimit=2", "rpc=3", "rpc={limit=0}"} {
		_, _, err := mergeConfig([]byte(original), reference)
		require.Error(t, err, original)
	}
}

func TestSyncConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("# operator\nmoniker='validator'\n[rpc]\nmax_concurrent_heavy_requests=3\n")
	require.NoError(t, os.WriteFile(path, original, 0o640))
	added, backup, err := syncConfigFile(path, consensusReference(t), true)
	require.NoError(t, err)
	require.NotEmpty(t, added)
	require.Empty(t, backup)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, data)
	added, backup, err = syncConfigFile(path, consensusReference(t), false)
	require.NoError(t, err)
	require.Contains(t, added, "storage.compact")
	require.Contains(t, added, "storage.compaction_interval")
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
	added, backup, err = syncConfigFile(path, consensusReference(t), false)
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
			_, _, err := syncConfigFile(path, consensusReference(t), false)
			require.Error(t, err)
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, data)
		})
	}
}

func TestSyncConfigCommandDryRun(t *testing.T) {
	home := filepath.Join(t.TempDir(), "absent")
	root := NewRootCmd()
	root.PersistentPreRunE = func(*cobra.Command, []string) error {
		t.Fatal("must not initialize config files")
		return nil
	}
	root.SetArgs([]string{"config", "sync", "--home", home, "--fibre-home", filepath.Join(home, "fibre"), "--dry-run"})
	var out bytes.Buffer
	root.SetOut(&out)
	require.Error(t, root.Execute())
	_, err := os.Stat(home)
	require.True(t, os.IsNotExist(err))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "config"), 0o700))
	path := filepath.Join(home, "config", "config.toml")
	require.NoError(t, os.WriteFile(path, []byte("moniker='mine'"), 0o600))
	require.NoError(t, root.Execute())
	require.True(t, strings.Contains(out.String(), "storage.compact"))
	entries, err := os.ReadDir(filepath.Dir(path))
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestMergeEmptyAndUnterminatedConfig(t *testing.T) {
	for _, original := range []string{"", "# comment", "[rpc]", "[rpc]\r\n# comment\r\n"} {
		updated, added, err := mergeConfig([]byte(original), []byte("[rpc]\n# Limit\nlimit=20\n"))
		require.NoError(t, err, original)
		require.Equal(t, []string{"rpc.limit"}, added)
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
	_, _, err = syncConfigFile(path, consensusReference(t), false)
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
	_, _, err := syncConfigFile(path, consensusReference(t), false)
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
			appPath := filepath.Join(home, "config", "app.toml")
			appBefore, err := os.ReadFile(appPath)
			require.NoError(t, err)
			syncCmd := syncConfigCmd()
			syncCmd.SetArgs([]string{"--home", home, "--fibre-home", t.TempDir()})
			require.NoError(t, syncCmd.Execute())
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

func TestSyncFibreConfigFile(t *testing.T) {
	// Fibre rejects unknown keys on load, so the fixture only uses real ones.
	original := []byte("# operator\nserver_listen_address = '0.0.0.0:9999' # custom\n")
	wantAdded := []string{"app_grpc_address", "max_connections", "object_storage.bucket"}
	reference, err := renderFibreConfig()
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), fibre.DefaultConfigFileName)
	require.NoError(t, os.WriteFile(path, original, 0o640))

	t.Log("dry run reports missing settings and writes nothing")
	added, backup, err := syncConfigFile(path, reference, true)
	require.NoError(t, err)
	require.Subset(t, added, wantAdded, "dry run should list the missing keys")
	require.Empty(t, backup, "dry run should not create a backup")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, original, data, "dry run should not modify the file")

	t.Log("real run adds settings and backs up the original")
	added, backup, err = syncConfigFile(path, reference, false)
	require.NoError(t, err)
	require.Subset(t, added, wantAdded, "real run should add the missing keys")
	saved, err := os.ReadFile(backup)
	require.NoError(t, err, "backup file should exist")
	require.Equal(t, original, saved, "backup should hold the original file")

	t.Log("file gains the missing keys; existing comments, values and permissions are kept")
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "\nmax_connections = ", "added key should be written to the file")
	require.Contains(t, string(data), "# operator", "operator comment should be kept")
	var before, after map[string]any
	require.NoError(t, toml.Unmarshal(original, &before))
	require.NoError(t, toml.Unmarshal(data, &after))
	require.True(t, configValuesPreserved(before, after), "existing values should be unchanged")
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "file permissions should be kept")

	t.Log("Fibre loads the synced file with the custom value and defaults")
	cfg := fibre.DefaultServerConfig()
	require.NoError(t, cfg.Load(path), "Fibre should load the synced file")
	require.Equal(t, "0.0.0.0:9999", cfg.ServerListenAddress, "custom value should survive sync")
	require.Equal(t, fibre.DefaultServerConfig().MaxConnections, cfg.MaxConnections, "added key should carry the default")

	t.Log("second sync is a no-op")
	added, second, err := syncConfigFile(path, reference, false)
	require.NoError(t, err)
	require.Empty(t, added, "nothing should be missing after a sync")
	require.Empty(t, second, "no backup should be written when nothing changes")

	t.Log("a later sync never overwrites an earlier backup")
	require.NoError(t, os.WriteFile(path, original, 0o640))
	_, second, err = syncConfigFile(path, reference, false)
	require.NoError(t, err)
	require.NotEqual(t, backup, second, "each sync should write a new backup")
	saved, err = os.ReadFile(backup)
	require.NoError(t, err, "earlier backup should still exist")
	require.Equal(t, original, saved, "earlier backup should be untouched")
}

func TestSyncConfigCommandAllFiles(t *testing.T) {
	for _, tc := range []struct {
		name      string
		useEnv    bool // pass the Fibre home via FIBRE_HOME instead of --fibre-home
		coreFile  bool // whether a config.toml exists in the node home
		fibreFile bool // whether a server_config.toml exists in the Fibre home
	}{
		{name: "fibre home from flag", coreFile: true, fibreFile: true},
		{name: "fibre home from env", useEnv: true, coreFile: true, fibreFile: true},
		{name: "no fibre file is skipped", coreFile: true},
		{name: "fibre-only host skips config.toml", fibreFile: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, fibreHome := t.TempDir(), t.TempDir()
			if tc.coreFile {
				require.NoError(t, os.Mkdir(filepath.Join(home, "config"), 0o700))
				require.NoError(t, os.WriteFile(filepath.Join(home, "config", "config.toml"), []byte("moniker='mine'\n"), 0o600))
			}
			if tc.fibreFile {
				require.NoError(t, os.Mkdir(filepath.Join(fibreHome, "config"), 0o700))
				require.NoError(t, os.WriteFile(fibre.DefaultConfigPath(fibreHome), []byte("server_listen_address='0.0.0.0:9999'\n"), 0o600))
			}

			args := []string{"config", "sync", "--home", home}
			if tc.useEnv {
				t.Setenv(fibre.EnvHome, fibreHome)
			} else {
				args = append(args, "--fibre-home", fibreHome)
			}
			root := NewRootCmd()
			root.SetArgs(args)
			var buf bytes.Buffer
			root.SetOut(&buf)
			require.NoError(t, root.Execute())
			out := buf.String()

			if tc.coreFile {
				require.Contains(t, out, "Added settings to config.toml: ", "config.toml should be synced")
			} else {
				require.Contains(t, out, "config.toml not found at "+filepath.Join(home, "config", "config.toml"), "missing config.toml should be reported")
				_, err := os.Stat(filepath.Join(home, "config"))
				require.True(t, os.IsNotExist(err), "sync should not create a node config dir")
			}
			if tc.fibreFile {
				require.Contains(t, out, "Added settings to server_config.toml: ", "Fibre file should be synced")
			} else {
				require.Contains(t, out, "server_config.toml not found at "+fibre.DefaultConfigPath(fibreHome), "missing Fibre file should be reported")
				_, err := os.Stat(filepath.Join(fibreHome, "config"))
				require.True(t, os.IsNotExist(err), "sync should not create a Fibre config dir")
			}
		})
	}
}

func TestSyncConfigInvalidFibreLeavesCoreUntouched(t *testing.T) {
	home, fibreHome := t.TempDir(), t.TempDir()
	corePath := filepath.Join(home, "config", "config.toml")
	coreOriginal := []byte("moniker='mine'\n") // stale: sync would add settings
	require.NoError(t, os.Mkdir(filepath.Join(home, "config"), 0o700))
	require.NoError(t, os.WriteFile(corePath, coreOriginal, 0o600))
	invalidFibre := []byte("[broken") // unterminated table header, not valid TOML
	require.NoError(t, os.Mkdir(filepath.Join(fibreHome, "config"), 0o700))
	require.NoError(t, os.WriteFile(fibre.DefaultConfigPath(fibreHome), invalidFibre, 0o600))

	root := NewRootCmd()
	root.SetArgs([]string{"config", "sync", "--home", home, "--fibre-home", fibreHome})
	root.SetOut(&bytes.Buffer{})
	require.Error(t, root.Execute(), "invalid Fibre file should fail the command")

	data, err := os.ReadFile(corePath)
	require.NoError(t, err)
	require.Equal(t, coreOriginal, data, "config.toml must not be written when the Fibre file is invalid")
}
