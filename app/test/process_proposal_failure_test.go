package app_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/celestia-app/v10/pkg/wrapper"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/nmt"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestProcessProposalBranchFailures(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), "account")
	req := processProposalRequest(t, testApp, nil)
	req.Time = testutil.GenesisTime.Add(time.Hour)
	header := cmtproto.Header{ChainID: testutil.ChainID, Height: req.Height, Time: req.Time}
	newContext := func() sdk.Context {
		return testApp.NewProposalContext(header).WithExecMode(sdk.ExecModeProcessProposal)
	}
	floorBefore, err := testApp.FibreKeeper.GetPromiseFreshnessFloor(newContext())
	require.NoError(t, err)

	t.Run("begin blocker error rejects the proposal", func(t *testing.T) {
		ctx := newContext()
		ctx.KVStore(testApp.GetKey(fibretypes.StoreKey)).Set(fibretypes.PromiseFreshnessFloorKey, []byte("invalid time"))
		_, err := testApp.FibreKeeper.GetPromiseFreshnessFloor(ctx)
		require.ErrorContains(t, err, "failed to parse promise freshness floor")

		resp, err := testApp.ProcessProposalHandler(ctx, req)
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	})

	t.Run("out of gas panic rejects without escaping the handler", func(t *testing.T) {
		meter := storetypes.NewGasMeter(1)
		ctx := newContext().WithGasMeter(meter)
		require.NotPanics(t, func() {
			resp, err := testApp.ProcessProposalHandler(ctx, req)
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
		})
		require.True(t, meter.IsPastLimit(), "the injected gas limit must trigger the recovery path")
	})

	// Each failure was confined to a proposal branch; the same empty proposal
	// still succeeds from the original state.
	floorAfter, err := testApp.FibreKeeper.GetPromiseFreshnessFloor(newContext())
	require.NoError(t, err)
	require.Equal(t, floorBefore, floorAfter)
	resp, err := testApp.ProcessProposalHandler(newContext(), req)
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
}

func TestProcessProposalKeepsFailedEscrowDeposit(t *testing.T) {
	accounts := []string{"owner"}
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	owner := testfactory.GetAddress(kr, accounts[0])
	seedFibreEscrow(t, testApp, owner, 100)
	header := cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight() + 1,
		Time:    testutil.GenesisTime.Add(time.Hour),
	}
	ctx := testApp.NewProposalContext(header)
	escrowBefore, found := testApp.FibreKeeper.GetEscrowAccount(ctx, owner.String())
	require.True(t, found)
	balanceBefore := testApp.BankKeeper.GetBalance(ctx, owner, appconsts.BondDenom)
	msg := &fibretypes.MsgDepositToEscrow{
		Signer: owner.String(),
		Amount: balanceBefore.Add(sdk.NewInt64Coin(appconsts.BondDenom, 1)),
	}
	tx, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1_000_000), user.SetFee(4_000))
	require.NoError(t, err)
	req := processProposalRequest(t, testApp, [][]byte{tx})
	req.Time = header.Time

	// Prove that message execution fails for the intended reason. Ordinary
	// message failure must not invalidate an otherwise valid proposal.
	_, err = testApp.MsgServiceRouter().Handler(msg)(ctx, msg)
	require.ErrorContains(t, err, "failed to transfer funds to escrow")
	resp, err := testApp.ProcessProposal(req)
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)

	ctx = testApp.NewProposalContext(header)
	escrowAfter, found := testApp.FibreKeeper.GetEscrowAccount(ctx, owner.String())
	require.True(t, found)
	require.Equal(t, escrowBefore, escrowAfter)
	require.Equal(t, infos[0].Sequence, testApp.AccountKeeper.GetAccount(ctx, owner).GetSequence())
	require.Equal(t, balanceBefore, testApp.BankKeeper.GetBalance(ctx, owner, appconsts.BondDenom))
}

func TestProcessProposalRejectsDataRootComputationFailure(t *testing.T) {
	accounts := []string{"sender", "recipient"}
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txs := testutil.SendTxsWithAccounts(t, testApp, enc.TxConfig, kr, 1, accounts[0], accounts[1:], testutil.ChainID)
	req := processProposalRequest(t, testApp, coretypes.Txs(txs).ToSliceOfBytes())
	req.Time = testutil.GenesisTime.Add(time.Hour)
	header := cmtproto.Header{ChainID: testutil.ChainID, Height: req.Height, Time: req.Time}
	sender := testfactory.GetAddress(kr, accounts[0])
	ctx := testApp.NewProposalContext(header)
	sequenceBefore := testApp.AccountKeeper.GetAccount(ctx, sender).GetSequence()
	balanceBefore := testApp.BankKeeper.GetBalance(ctx, sender, appconsts.BondDenom)

	// Inject an NMT failure through the pool's existing custom-hasher option.
	// No transaction validation or square-construction checks are bypassed.
	hasher := &failingProposalHasher{Hasher: nmt.NewNmtHasher(appconsts.NewBaseHashFunc(), share.NamespaceSize, true)}
	pool, err := wrapper.NewTreePool(1, 1, nmt.CustomHasher(hasher))
	require.NoError(t, err)
	originalPool := *testApp.TreePool()
	*testApp.TreePool() = *pool
	t.Cleanup(func() { *testApp.TreePool() = originalPool })

	resp, err := testApp.ProcessProposal(req)
	require.NoError(t, err)
	require.Positive(t, hasher.calls.Load(), "the proposal must reach data-root computation")
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	ctx = testApp.NewProposalContext(header)
	require.Equal(t, sequenceBefore, testApp.AccountKeeper.GetAccount(ctx, sender).GetSequence())
	require.Equal(t, balanceBefore, testApp.BankKeeper.GetBalance(ctx, sender, appconsts.BondDenom))

	*testApp.TreePool() = originalPool
	resp, err = testApp.ProcessProposal(req)
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status,
		"the identical proposal must succeed after restoring the tree pool")
}

type failingProposalHasher struct {
	nmt.Hasher
	calls atomic.Int64
}

func (h *failingProposalHasher) HashLeaf([]byte) ([]byte, error) {
	h.calls.Add(1)
	return nil, errors.New("injected leaf hashing failure")
}
