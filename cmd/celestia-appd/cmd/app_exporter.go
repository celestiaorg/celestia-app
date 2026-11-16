package cmd

import (
	"fmt"
	"io"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	dbm "github.com/cosmos/cosmos-db"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
)

// FlagAllowUpgradeWindow allows an export at the last height before a scheduled upgrade.
const FlagAllowUpgradeWindow = "allow-upgrade-window"

// addExportFlags adds celestia-app flags to the export command.
func addExportFlags(rootCmd *cobra.Command) error {
	exportCmd, _, err := rootCmd.Find([]string{"export"})
	if err != nil {
		return fmt.Errorf("failed to find export command: %w", err)
	}
	exportCmd.Flags().Bool(FlagAllowUpgradeWindow, false, "Allow an export at the last height before a scheduled upgrade. "+
		"Use only if that height changed no validators (validators_hash == next_validators_hash in the header of the upgrade height)")
	return nil
}

func appExporter(
	logger log.Logger,
	db dbm.DB,
	traceStore io.Writer,
	height int64,
	forZeroHeight bool,
	jailWhiteList []string,
	appOptions servertypes.AppOptions,
	modulesToExport []string,
) (servertypes.ExportedApp, error) {
	application := app.NewForExport(logger, db, traceStore, 0, 0, appOptions)
	application.SetAllowUpgradeWindowExport(cast.ToBool(appOptions.Get(FlagAllowUpgradeWindow)))
	var err error
	if height == -1 {
		err = application.LoadLatestVersion()
	} else {
		err = application.LoadHeight(height)
	}
	if err != nil {
		return servertypes.ExportedApp{}, err
	}
	return application.ExportAppStateAndValidators(forZeroHeight, jailWhiteList, modulesToExport)
}
