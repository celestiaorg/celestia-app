//go:build fibre

package app

import (
	"cosmossdk.io/log"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/fibre"
	fibrekeeper "github.com/celestiaorg/celestia-app/v8/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/celestia-app/v8/x/valaddr"
	valaddrkeeper "github.com/celestiaorg/celestia-app/v8/x/valaddr/keeper"
	valaddrtypes "github.com/celestiaorg/celestia-app/v8/x/valaddr/types"
	"github.com/celestiaorg/go-square/v4/tx"
	tmbytes "github.com/cometbft/cometbft/libs/bytes"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/runtime"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

// fibreKeepers holds the keepers for the fibre and valaddr modules.
// It is embedded in the App struct.
type fibreKeepers struct {
	FibreKeeper   *fibrekeeper.Keeper
	ValAddrKeeper valaddrkeeper.Keeper
}

func fibreEncodingRegisters() []module.AppModuleBasic {
	return []module.AppModuleBasic{
		fibre.AppModule{},
		valaddr.AppModule{},
	}
}

func fibreBeginBlockers() []string {
	return []string{fibretypes.ModuleName, valaddrtypes.ModuleName}
}

func fibreEndBlockers() []string {
	return []string{fibretypes.ModuleName, valaddrtypes.ModuleName}
}

func fibreInitGenesisModules() []string {
	return []string{fibretypes.ModuleName, valaddrtypes.ModuleName}
}

func fibreStoreKeys() []string {
	return []string{valaddrtypes.StoreKey, fibretypes.StoreKey}
}

func fibreMaccPerms() map[string][]string {
	return map[string][]string{
		fibretypes.ModuleName: nil,
	}
}

func fibreUpgradeStoreKeys() []string {
	return []string{fibretypes.StoreKey, valaddrtypes.StoreKey}
}

func (app *App) initFibreKeepers(encodingConfig encoding.Config, keys map[string]*storetypes.KVStoreKey, logger log.Logger, govModuleAddr string) {
	app.ValAddrKeeper = valaddrkeeper.NewKeeper(
		encodingConfig.Codec,
		runtime.NewKVStoreService(keys[valaddrtypes.StoreKey]),
		logger,
		app.StakingKeeper,
	)

	app.FibreKeeper = fibrekeeper.NewKeeper(
		encodingConfig.Codec,
		keys[fibretypes.StoreKey],
		app.BankKeeper,
		app.StakingKeeper,
		govModuleAddr,
	)
}

func (app *App) fibreAppModules(encodingConfig encoding.Config) []module.AppModule {
	return []module.AppModule{
		valaddr.NewAppModule(encodingConfig.Codec, app.ValAddrKeeper),
		fibre.NewAppModule(encodingConfig.Codec, *app.FibreKeeper),
	}
}

// classifyFibreTx checks if the decoded SDK transaction contains exactly one
// MsgPayForFibre and no other messages. Returns true if it is a valid
// pay-for-fibre transaction.
func classifyFibreTx(sdkTx sdk.Tx) bool {
	msgs := sdkTx.GetMsgs()
	if len(msgs) != 1 {
		return false
	}
	_, ok := msgs[0].(*fibretypes.MsgPayForFibre)
	return ok
}

// countFibreMsgs returns the number of MsgPayForFibre messages in a transaction.
func countFibreMsgs(sdkTx sdk.Tx) int {
	count := 0
	for _, msg := range sdkTx.GetMsgs() {
		if _, ok := msg.(*fibretypes.MsgPayForFibre); ok {
			count++
		}
	}
	return count
}

// maxPayForFibreMessages returns the maximum number of PayForFibre messages
// allowed in a block.
func maxPayForFibreMessages() int {
	return appconsts.MaxPayForFibreMessages
}

// processFibreTxsForSquare processes pay-for-fibre transactions: synthesize
// system blob, validate, append to builder. Returns the raw tx bytes of
// accepted fibre txs.
func processFibreTxsForSquare(fsb *FilteredSquareBuilder, ctx sdk.Context, payForFibreTxs [][]byte) [][]byte {
	logger := ctx.Logger().With("app/filtered-square-builder")
	dec := fsb.txConfig.TxDecoder()
	var pffMessageCount int
	fibreTxs := make([][]byte, 0, len(payForFibreTxs))

	for _, rawTx := range payForFibreTxs {
		// TryParseFibreTx parses the MsgPayForFibre proto fields and builds the system blob.
		// separateTxs guarantees rawTx contains exactly one MsgPayForFibre, so fibreTx is always non-nil.
		fibreTx, err := tx.TryParseFibreTx(rawTx)
		if err != nil {
			logger.Error("synthesizing fibre tx", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
			continue
		}

		sdkTx, err := dec(rawTx)
		if err != nil {
			logger.Error("decoding pay-for-fibre transaction", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
			continue
		}

		if pffMessageCount+len(sdkTx.GetMsgs()) > appconsts.MaxPayForFibreMessages {
			logger.Debug("skipping pay-for-fibre tx because the max PayForFibre message count was reached", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()))
			continue
		}

		ctx = ctx.WithTxBytes(rawTx)

		ok, err := fsb.builder.AppendFibreTx(fibreTx)
		if err != nil {
			logger.Error("appending pay-for-fibre transaction to builder", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
			continue
		}
		if !ok {
			logger.Debug("skipping pay-for-fibre tx because it was too large to fit in the square", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()))
			continue
		}

		ctx, err = fsb.handler(ctx, sdkTx, false)
		if err != nil {
			logger.Error(
				"filtering already checked pay-for-fibre transaction",
				"tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()),
				"error", err,
				"msgs", msgTypes(sdkTx),
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_pay_for_fibre_txs")
			if revertErr := fsb.builder.RevertLastPayForFibreTx(); revertErr != nil {
				logger.Error("reverting last pay-for-fibre transaction", "error", revertErr)
			}
			continue
		}

		pffMessageCount += len(sdkTx.GetMsgs())
		fibreTxs = append(fibreTxs, rawTx)
	}

	return fibreTxs
}

// FibreModuleAccountNames returns the module account names for fibre modules.
func FibreModuleAccountNames() []string {
	return []string{fibretypes.ModuleName}
}
