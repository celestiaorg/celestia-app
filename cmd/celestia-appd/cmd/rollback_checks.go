package cmd

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	cometbftdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtos "github.com/cometbft/cometbft/libs/os"
	"github.com/cometbft/cometbft/state"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// addRollbackAppVersionCheck refuses a rollback of state this binary does not run natively,
// and a passthrough rollback of state on another app version.
func addRollbackAppVersionCheck(rootCmd *cobra.Command) error {
	rollbackCmd, _, err := rootCmd.Find([]string{"rollback"})
	if err != nil {
		return fmt.Errorf("failed to find rollback command: %w", err)
	}
	rollbackCmd.PreRunE = checkRollbackAppVersion

	// Only the multiplexer build has passthrough.
	for _, c := range rootCmd.Commands() {
		if c.Name() == "passthrough" {
			c.PreRunE = func(_ *cobra.Command, args []string) error {
				return checkPassthroughRollback(app.NodeHome, args)
			}
		}
	}
	return nil
}

func checkRollbackAppVersion(cmd *cobra.Command, _ []string) error {
	version, err := stateAppVersion(server.GetServerContextFromCmd(cmd).Config)
	if err != nil {
		return err
	}
	switch {
	case version == 0:
		return nil
	case version < appconsts.Version:
		return fmt.Errorf(`node is on app version %d, binary on %d: use "celestia-appd passthrough v%d rollback" (one height only)`,
			version, appconsts.Version, version)
	case version > appconsts.Version:
		return fmt.Errorf("node is on app version %d, binary on %d: use a newer celestia-appd", version, appconsts.Version)
	}
	return nil
}

// checkPassthroughRollback refuses "passthrough vN rollback" unless the node at home is on app version N.
// An older app rewrites the state without the stores it does not know, and the node can't start.
func checkPassthroughRollback(home string, args []string) error {
	if len(args) < 2 || !strings.EqualFold(args[1], "rollback") {
		return nil
	}
	want, err := strconv.ParseUint(strings.TrimPrefix(args[0], "v"), 10, 64)
	if err != nil {
		return nil // passthrough reports it
	}
	cfg, err := cometConfig(home)
	if err != nil {
		return err
	}
	version, err := stateAppVersion(cfg)
	if err != nil {
		return err
	}
	if version != 0 && version != want {
		return fmt.Errorf("node is on app version %d, not %d", version, want)
	}
	return nil
}

// cometConfig reads home/config/config.toml, or returns the defaults if it does not exist.
func cometConfig(home string) (*cmtcfg.Config, error) {
	cfg := cmtcfg.DefaultConfig()
	path := filepath.Join(home, "config", "config.toml")
	if cmtos.FileExists(path) {
		v := viper.New()
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		if err := v.Unmarshal(cfg); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
	}
	cfg.SetRoot(home)
	return cfg, nil
}

// stateAppVersion reads Version.Consensus.App from state.db and closes it; 0 if there is no state.
func stateAppVersion(cfg *cmtcfg.Config) (uint64, error) {
	if !cmtos.FileExists(filepath.Join(cfg.DBDir(), "state.db")) {
		return 0, nil
	}
	db, err := cometbftdb.NewDB("state", cometbftdb.BackendType(cfg.DBBackend), cfg.DBDir())
	if err != nil {
		return 0, err
	}
	defer db.Close()

	st, err := state.NewStore(db, state.StoreOptions{}).Load()
	if err != nil {
		return 0, fmt.Errorf("load CometBFT state: %w", err)
	}
	return st.Version.Consensus.App, nil
}
