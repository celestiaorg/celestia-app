package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalDepositReplayMatchesFinalizeBlock covers CELESTIA-310.
// ProcessProposal replays MsgDepositToEscrow before settling later PFFs, but it
// does not replay the bank send that precedes the deposit. The deposit thus
// succeeds on the proposal branch and fails in FinalizeBlock, so a PFF that the
// proposal settled against the phantom deposit fails in FinalizeBlock while its
// system blob is already in the square.
//
// Invariant: a proposal that ProcessProposal accepts must settle every PFF in
// FinalizeBlock.
func TestProcessProposalDepositReplayMatchesFinalizeBlock(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	owner := testfactory.GetAddress(kr, accounts[0])
	recipient := testfactory.GetAddress(kr, accounts[1])

	// The escrow covers exactly one PFF.
	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	seedFibreEscrow(t, testApp, owner, payment)

	blockTime := time.Now()
	ctx := testApp.NewProposalContext(cmtproto.Header{ChainID: testutil.ChainID, Height: testApp.LastBlockHeight() + 1, Time: blockTime})
	balance := testApp.BankKeeper.GetBalance(ctx, owner, appconsts.BondDenom).Amount.Int64()

	const fee = 4_000
	opts := []user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(fee)}
	// The deposit is far larger than what the owner keeps after the send, so it
	// fails in FinalizeBlock. On the proposal branch it lifts the escrow to cover
	// both PFFs.
	deposit := int64(1_000_000)
	require.Greater(t, balance, deposit+4*fee)

	// tx0: drain the owner, keeping only the fees of the three later txs.
	send := banktypes.NewMsgSend(owner, recipient, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, balance-4*fee)))
	sendTx, _, err := signer.CreateTx([]sdk.Msg{send}, opts...)
	require.NoError(t, err)
	require.NoError(t, signer.IncrementSequence(accounts[0]))

	// tx1: deposit that only succeeds when tx0 is not executed.
	depositMsg := &fibretypes.MsgDepositToEscrow{Signer: owner.String(), Amount: sdk.NewInt64Coin(appconsts.BondDenom, deposit)}
	depositTx, _, err := signer.CreateTx([]sdk.Msg{depositMsg}, opts...)
	require.NoError(t, err)
	require.NoError(t, signer.IncrementSequence(accounts[0]))

	// tx2, tx3: two distinct PFFs against an escrow that covers one.
	pffTxs := newPayForFibreTxPair(t, signer, accounts[0], blockTime.Add(-2*time.Second), blockTime.Add(-time.Second))

	txs := append([][]byte{sendTx, depositTx}, pffTxs...)
	resp, err := testApp.ProcessProposal(processProposalRequestAt(t, testApp, txs, blockTime))
	require.NoError(t, err)

	finalize, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:   blockTime,
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    txs,
	})
	require.NoError(t, err)
	require.Len(t, finalize.TxResults, 4)
	codes := make([]uint32, len(finalize.TxResults))
	for i, r := range finalize.TxResults {
		codes[i] = r.Code
	}
	t.Logf("ProcessProposal=%s FinalizeBlock codes=%v", resp.Status, codes)
	require.Equal(t, abci.CodeTypeOK, codes[0], "the send succeeds")
	require.NotEqual(t, abci.CodeTypeOK, codes[1], "the deposit fails after the send drains the owner")

	if resp.Status == abci.ResponseProcessProposal_ACCEPT {
		for i := 2; i < 4; i++ {
			require.Equal(t, abci.CodeTypeOK, codes[i],
				"ProcessProposal accepted PFF tx %d but it failed in FinalizeBlock: %s", i, finalize.TxResults[i].Log)
		}
	}
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status,
		"PFF tx 3 cannot settle in FinalizeBlock, so ProcessProposal must reject")
}
