// Package simulation holds the x/blob operations for the Cosmos SDK
// simulation framework. Only simulation tests import it; a node never runs it.
package simulation

import (
	"errors"
	"math/rand"
	"time"

	"cosmossdk.io/collections"
	"github.com/celestiaorg/celestia-app/v12/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v12/x/blob/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
	simtypes "github.com/cosmos/cosmos-sdk/types/simulation"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govkeeper "github.com/cosmos/cosmos-sdk/x/gov/keeper"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/simulation"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
)

const (
	weightMsgPayForBlobs              = 100
	weightMsgUpdateBlobParamsProposal = 5

	maxBlobsPerPFB = 4
	maxBlobSize    = 100_000
	// maxSimBlockTime is the longest time between two simulated blocks
	// (maxTimePerBlock in x/simulation).
	maxSimBlockTime = 10_000 * time.Second
)

// txFee is the fee every simulated tx pays. GenAndDeliverTx sets the gas
// limit to simtestutil.DefaultGenTxGas (10M), so this is a gas price of
// 0.004 utia (appconsts.DefaultMinGasPrice), above the x/minfee network min
// gas price.
var txFee = sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, simtestutil.DefaultGenTxGas*4/1000))

// WeightedOperations returns the x/blob simulation operations.
func WeightedOperations(
	txGen client.TxConfig,
	ak simulation.AccountKeeper,
	bk simulation.BankKeeper,
	gk *govkeeper.Keeper,
	sk *stakingkeeper.Keeper,
) simulation.WeightedOperations {
	return simulation.WeightedOperations{
		simulation.NewWeightedOperation(weightMsgPayForBlobs, SimulateMsgPayForBlobs(txGen, ak, bk)),
		simulation.NewWeightedOperation(weightMsgUpdateBlobParamsProposal, SimulateMsgUpdateBlobParamsProposal(txGen, ak, bk, gk, sk)),
	}
}

// SimulateMsgPayForBlobs delivers a MsgPayForBlobs for random blobs. It
// delivers the tx the way FinalizeBlock receives it: celestia-core has
// already removed the blobs from the BlobTx, so only the sdk.Tx is executed.
func SimulateMsgPayForBlobs(txGen client.TxConfig, ak simulation.AccountKeeper, bk simulation.BankKeeper) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgPayForBlobs{})
		simAccount, _ := simtypes.RandomAcc(r, accs)

		blobs, err := randomBlobs(r, simAccount.Address)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to generate blobs"), nil, err
		}
		msg, err := types.NewMsgPayForBlobs(simAccount.Address.String(), appconsts.Version, blobs...)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to generate msg"), nil, err
		}
		return deliver(r, app, txGen, ak, bk, ctx, chainID, simAccount, msg, nil)
	}
}

// SimulateMsgUpdateBlobParamsProposal submits a gov proposal that carries a
// MsgUpdateBlobParams with random valid params, and schedules a yes vote from
// every bonded validator. If the proposal passes, the x/gov EndBlocker
// executes the MsgUpdateBlobParams.
func SimulateMsgUpdateBlobParamsProposal(
	txGen client.TxConfig,
	ak simulation.AccountKeeper,
	bk simulation.BankKeeper,
	gk *govkeeper.Keeper,
	sk *stakingkeeper.Keeper,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, accs []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msgType := sdk.MsgTypeURL(&types.MsgUpdateBlobParams{})
		simAccount, _ := simtypes.RandomAcc(r, accs)

		params, err := gk.Params.Get(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to get gov params"), nil, err
		}
		govAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()
		updateMsg := types.NewMsgUpdateBlobParams(govAddr, randomParams(r))
		msg, err := govv1.NewMsgSubmitProposal([]sdk.Msg{updateMsg}, params.MinDeposit, simAccount.Address.String(),
			"", simtypes.RandStringOfLength(r, 10), simtypes.RandStringOfLength(r, 10), false)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to generate proposal"), nil, err
		}

		proposalID, err := gk.ProposalID.Peek(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to get next proposal ID"), nil, err
		}
		opMsg, _, err := deliver(r, app, txGen, ak, bk, ctx, chainID, simAccount, msg, params.MinDeposit)
		if err != nil || !opMsg.OK {
			return simtypes.NoOpMsg(types.ModuleName, msgType, opMsg.Comment), nil, err
		}

		validators, err := sk.GetBondedValidatorsByPower(ctx)
		if err != nil {
			return simtypes.NoOpMsg(types.ModuleName, msgType, "unable to get validators"), nil, err
		}
		// Votes are scheduled by height: SimulateFromSeed drops future
		// operations scheduled by block time. A simulated block lasts at
		// most maxSimBlockTime, so every vote lands in the voting period.
		voteBlocks := max(int64(*params.VotingPeriod/maxSimBlockTime), 1)
		var futureOps []simtypes.FutureOperation
		for _, val := range validators {
			valAddr, err := sdk.ValAddressFromBech32(val.GetOperator())
			if err != nil {
				return simtypes.NoOpMsg(types.ModuleName, msgType, "invalid validator address"), nil, err
			}
			voter, found := simtypes.FindAccount(accs, sdk.AccAddress(valAddr))
			if !found {
				continue
			}
			futureOps = append(futureOps, simtypes.FutureOperation{
				BlockHeight: int(ctx.BlockHeight() + 1 + r.Int63n(voteBlocks)),
				Op:          simulateYesVote(txGen, ak, bk, gk, voter, proposalID),
			})
		}
		return simtypes.NewOperationMsg(updateMsg, true, "proposal submitted"), futureOps, nil
	}
}

// simulateYesVote votes yes on a proposal that is still in its voting period.
func simulateYesVote(
	txGen client.TxConfig,
	ak simulation.AccountKeeper,
	bk simulation.BankKeeper,
	gk *govkeeper.Keeper,
	voter simtypes.Account,
	proposalID uint64,
) simtypes.Operation {
	return func(r *rand.Rand, app *baseapp.BaseApp, ctx sdk.Context, _ []simtypes.Account, chainID string,
	) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
		msg := govv1.NewMsgVote(voter.Address, proposalID, govv1.OptionYes, "")
		proposal, err := gk.Proposals.Get(ctx, proposalID)
		if errors.Is(err, collections.ErrNotFound) || (err == nil && proposal.Status != govv1.StatusVotingPeriod) {
			return simtypes.NoOpMsg(govtypes.ModuleName, sdk.MsgTypeURL(msg), "proposal not in voting period"), nil, nil
		}
		if err != nil {
			return simtypes.NoOpMsg(govtypes.ModuleName, sdk.MsgTypeURL(msg), "unable to get proposal"), nil, err
		}
		return deliver(r, app, txGen, ak, bk, ctx, chainID, voter, msg, nil)
	}
}

// randomParams returns random valid x/blob params.
func randomParams(r *rand.Rand) types.Params {
	// Square sizes 64 to SquareSizeUpperBound, so that every blob that
	// SimulateMsgPayForBlobs generates still fits.
	squareSize := uint64(64) << r.Intn(4)
	return types.NewParams(uint32(simtypes.RandIntBetween(r, 1, 20)), min(squareSize, uint64(appconsts.SquareSizeUpperBound)))
}

// randomBlobs returns 1 to maxBlobsPerPFB blobs of share version 0 or 1 in
// random namespaces.
func randomBlobs(r *rand.Rand, signer sdk.AccAddress) ([]*share.Blob, error) {
	blobs := make([]*share.Blob, simtypes.RandIntBetween(r, 1, maxBlobsPerPFB+1))
	for i := range blobs {
		ns, err := randomNamespace(r)
		if err != nil {
			return nil, err
		}
		data := make([]byte, simtypes.RandIntBetween(r, 1, maxBlobSize+1))
		_, _ = r.Read(data)
		if r.Intn(2) == 0 {
			blobs[i], err = share.NewV0Blob(ns, data)
		} else {
			blobs[i], err = share.NewV1Blob(ns, data, signer)
		}
		if err != nil {
			return nil, err
		}
	}
	return blobs, nil
}

// randomNamespace returns a random version 0 namespace that users may write to.
func randomNamespace(r *rand.Rand) (share.Namespace, error) {
	for {
		id := make([]byte, share.NamespaceVersionZeroIDSize)
		_, _ = r.Read(id)
		ns, err := share.NewV0Namespace(id)
		if err != nil {
			return share.Namespace{}, err
		}
		if types.ValidateBlobNamespace(ns) == nil {
			return ns, nil
		}
	}
}

// deliver signs msg with simAccount, pays txFee and delivers it. It returns a
// no-op if the account cannot pay txFee plus spent.
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
	spent sdk.Coins,
) (simtypes.OperationMsg, []simtypes.FutureOperation, error) {
	spendable := bk.SpendableCoins(ctx, simAccount.Address)
	if !spendable.IsAllGTE(txFee.Add(spent...)) {
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
