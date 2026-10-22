package cmd

import (
	"fmt"
	"path/filepath"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	cometbftdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	cmtos "github.com/cometbft/cometbft/libs/os"
	"github.com/cometbft/cometbft/state"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// addRollbackAppVersionCheck refuses a rollback of state this binary does not run natively.
func addRollbackAppVersionCheck(rootCmd *cobra.Command) error {
	rollbackCmd, _, err := rootCmd.Find([]string{"rollback"})
	if err != nil {
		return fmt.Errorf("failed to find rollback command: %w", err)
	}
	rollbackCmd.PreRunE = checkRollbackAppVersion
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
