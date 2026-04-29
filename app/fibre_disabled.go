//go:build !fibre

package app

import (
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// fibreKeepers is empty when the fibre build tag is not set.
type fibreKeepers struct{} //nolint:unused

func fibreEncodingRegisters() []module.AppModuleBasic {
	return nil
}

func fibreBeginBlockers() []string {
	return nil
}

func fibreEndBlockers() []string {
	return nil
}

func fibreInitGenesisModules() []string {
	return nil
}

func fibreStoreKeys() []string {
	return nil
}

func fibreMaccPerms() map[string][]string {
	return nil
}

func fibreUpgradeStoreKeys() []string {
	return nil
}

func (app *App) initFibreKeepers(_ encoding.Config, _ map[string]*storetypes.KVStoreKey, _ log.Logger, _ string) {
}

func (app *App) fibreAppModules(_ encoding.Config) []module.AppModule {
	return nil
}

func countMsgPayForFibre(_ sdk.Tx) int {
	return 0
}

func maxPayForFibreMessages() int {
	return 0
}

func processFibreTxsForSquare(_ *FilteredSquareBuilder, _ sdk.Context, _ [][]byte) [][]byte {
	return nil
}

func FibreModuleAccountNames() []string {
	return nil
}
