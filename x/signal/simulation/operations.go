// Package simulation holds the x/signal operations for the Cosmos SDK
// simulation framework. Only simulation tests import it; a node never runs it.
package simulation

import (
	"math/rand"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/signal"
	"github.com/celestiaorg/celestia-app/v11/x/signal/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
)

const (
	weightMsgSignalVersion = 50
	weightMsgTryUpgrade    = 10
)

// txFee is the fee every simulated tx pays. GenAndDeliverTx sets the gas
// limit to simtestutil.DefaultGenTxGas (10M), so this is a gas price of
// 0.004 utia (appconsts.DefaultMinGasPrice), above the x/minfee network min
// gas price.
var txFee = sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, simtestutil.DefaultGenTxGas*4/1000))

// WeightedOperations returns the x/signal simulation operations.
func WeightedOperations(
	txGen client.TxConfig,
	ak simulation.AccountKeeper,
	bk simulation.BankKeeper,
	k *signal.Keeper,
	sk *stakingkeeper.Keeper,
) simulation.WeightedOperations {
	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightMsgSignalVersion, SimulateMsgSignalVersion(txGen, ak, bk, k, sk)),
		simulation.NewWeightedOperation(weightMsgTryUpgrade, SimulateMsgTryUpgrade(txGen, ak, bk, k)),
	}
}

// SimulateMsgSignalVersion makes a random validator signal for the next app
// version, or sometimes for the current one.
func SimulateMsgSignalVersion(
	txGen client.TxConfig,
	ak simulation.AccountKeeper,
	bk simulation.BankKeeper,
	k *signal.Keeper,
	sk *stakingkeeper.Keeper,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgSignalVersion{})
		if k.IsUpgradePending(ctx) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "upgrade pending"), nil, nil
		}

		validators, err := sk.GetAllValidators(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to get validators"), nil, err
		}
		if len(validators) == 0 {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "no validators"), nil, nil
		}
		val := validators[r.Intn(len(validators))]
		valAddr, err := sdk.ValAddressFromBech32(val.GetOperator())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid validator address"), nil, err
		}
		simAccount, found := simtypes.FindAccount(accs, sdk.AccAddress(valAddr))
		if !found {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "validator account not found"), nil, nil
		}

		version, err := app.AppVersion(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to get app version"), nil, err
		}
		// Most signals are for the next version so that a MsgTryUpgrade can
		// reach quorum and schedule an upgrade. The next version has no
		// upgrade handler, but the upgrade height delay of the simulation
		// chain ID is far beyond the end of the run.
		if r.Intn(10) != 0 {
			version++
		}

		msg := types.NewMsgSignalVersion(valAddr.String(), version)
		return deliver(r, app, txGen, ak, bk, ctx, chainID, simAccount, msg)
	}
}

// SimulateMsgTryUpgrade sends a MsgTryUpgrade from a random account. It is a
// no-op when the tally would make the message fail: an upgrade is already
// pending, or quorum is reached for a version that is not above the current
// one.
func SimulateMsgTryUpgrade(
	txGen client.TxConfig,
	ak simulation.AccountKeeper,
	bk simulation.BankKeeper,
	k *signal.Keeper,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgTryUpgrade{})
		if k.IsUpgradePending(ctx) {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "upgrade pending"), nil, nil
		}

		// TallyVotingPower can delete signals, so tally on a discarded cache.
		cacheCtx, _ := ctx.CacheContext()
		threshold, err := k.GetVotingPowerThreshold(cacheCtx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to get threshold"), nil, err
		}
		hasQuorum, quorumVersion, err := k.TallyVotingPower(cacheCtx, threshold.Int64())
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to tally"), nil, err
		}
		version, err := app.AppVersion(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to get app version"), nil, err
		}
		if hasQuorum && quorumVersion <= version {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "quorum version is not above the current version"), nil, nil
		}

		simAccount, _ := simtypes.RandomAcc(r, accs)
		msg := types.NewMsgTryUpgrade(simAccount.Address)
		return deliver(r, app, txGen, ak, bk, ctx, chainID, simAccount, msg)
	}
}

// deliver signs msg with simAccount, pays txFee and delivers it. It returns a
// no-op if the account cannot pay txFee.
func deliver(
	r *rand.Rand,
	app *baseapp.BaseApp,
	txGen client.TxConfig,
	ak simulation.AccountKeeper,
	bk simulation.BankKeeper,
	ctx sdk.Context,
	chainID string,
	simAccount simtypes.Account,
	msg sdk.Msg,
) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	if !bk.SpendableCoins(ctx, simAccount.Address).IsAllGTE(txFee) {
		return simtypes.NoOpMsg(types.ModuleName, sdk.MsgTypeURL(msg), "insufficient funds"), nil, nil
	}
	return simulation.GenAndDeliverTx(simulation.OperationInput{
		R:             r,
		App:           app,
		TxGen:         txGen,
		Msg:           msg,
		Context:       ctx.WithChainID(chainID),
		SimAccount:    simAccount,
		AccountKeeper: ak,
		ModuleName:    types.ModuleName,
	}, txFee)
}
