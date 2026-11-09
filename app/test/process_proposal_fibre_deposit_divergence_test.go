package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalRejectsPayForFibreAfterEscrowChange covers CELESTIA-310.
// Proposal cannot predict whether a deposit or timeout succeeds in
// FinalizeBlock, because the outcome can depend on msgs that proposal does not
// execute. A PFF settled against such a guess could fail in FinalizeBlock while
// its system blob is already in the square. So a block must not hold a PFF
// whose escrow a deposit or timeout changed earlier in the block.
func TestProcessProposalRejectsPayForFibreAfterEscrowChange(t *testing.T) {
	const fee = 4_000
	opts := []user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(fee)}
	payment := fibretypes.PaymentAmount(100).Amount.Int64()

	type fixture struct {
		testApp   *app.App
		signers   []*user.Signer
		accounts  []string
		addrs     []sdk.AccAddress
		blockTime time.Time
	}
	setup := func(t *testing.T) fixture {
		enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
		accounts := testfactory.GenerateAccounts(2)
		testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
		commitBlock(t, testApp)
		infos := queryAccountInfo(testApp, accounts, kr)
		newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
		f := fixture{testApp: testApp, accounts: accounts, blockTime: time.Now()}
		for i, account := range accounts {
			f.signers = append(f.signers, newSigner(i))
			f.addrs = append(f.addrs, testfactory.GetAddress(kr, account))
		}
		return f
	}
	// tx creates a tx from account i and bumps its sequence.
	tx := func(t *testing.T, f fixture, i int, msgs ...sdk.Msg) []byte {
		raw, _, err := f.signers[i].CreateTx(msgs, opts...)
		require.NoError(t, err)
		require.NoError(t, f.signers[i].IncrementSequence(f.accounts[i]))
		return raw
	}
	// timeout returns a valid timeout for an expired promise of account i.
	timeout := func(t *testing.T, f fixture, i int) *fibretypes.MsgPaymentPromiseTimeout {
		params := f.testApp.FibreKeeper.GetParams(f.testApp.NewContext(true))
		creation := f.blockTime.Add(-params.PaymentPromiseTimeout).Add(-time.Hour)
		return &fibretypes.MsgPaymentPromiseTimeout{
			Signer:         f.addrs[i].String(),
			PaymentPromise: signedPromise(t, f.signers[i], f.accounts[i], creation),
		}
	}
	// run returns the ProcessProposal status and the FinalizeBlock tx codes.
	run := func(t *testing.T, f fixture, txs [][]byte) (abci.ResponseProcessProposal_ProposalStatus, []uint32) {
		resp, err := f.testApp.ProcessProposal(processProposalRequestAt(t, f.testApp, txs, f.blockTime))
		require.NoError(t, err)
		finalize, err := f.testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time:   f.blockTime,
			Height: f.testApp.LastBlockHeight() + 1,
			Hash:   f.testApp.LastCommitID().Hash,
			Txs:    txs,
		})
		require.NoError(t, err)
		codes := make([]uint32, len(finalize.TxResults))
		for i, r := range finalize.TxResults {
			codes[i] = r.Code
		}
		t.Logf("ProcessProposal=%s FinalizeBlock codes=%v", resp.Status, codes)
		return resp.Status, codes
	}

	t.Run("drained deposit funds a second PFF", func(t *testing.T) {
		f := setup(t)
		seedFibreEscrow(t, f.testApp, f.addrs[0], payment)
		ctx := f.testApp.NewProposalContext(cmtproto.Header{ChainID: testutil.ChainID, Height: f.testApp.LastBlockHeight() + 1, Time: f.blockTime})
		balance := f.testApp.BankKeeper.GetBalance(ctx, f.addrs[0], appconsts.BondDenom).Amount.Int64()
		deposit := int64(1_000_000)
		require.Greater(t, balance, deposit+4*fee)

		// Drain the owner down to the fees of the three later txs, so the
		// deposit fails in FinalizeBlock.
		send := banktypes.NewMsgSend(f.addrs[0], f.addrs[1], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, balance-4*fee)))
		sendTx := tx(t, f, 0, send)
		depositTx := tx(t, f, 0, &fibretypes.MsgDepositToEscrow{Signer: f.addrs[0].String(), Amount: sdk.NewInt64Coin(appconsts.BondDenom, deposit)})
		pffs := newPayForFibreTxPair(t, f.signers[0], f.accounts[0], f.blockTime.Add(-2*time.Second), f.blockTime.Add(-time.Second))
		txs := [][]byte{sendTx, depositTx, pffs[0], pffs[1]}

		status, codes := run(t, f, txs)
		require.Equal(t, []uint32{abci.CodeTypeOK, 5, abci.CodeTypeOK, 18}, codes, "the second PFF cannot settle in FinalizeBlock")
		require.Equal(t, abci.ResponseProcessProposal_REJECT, status)
	})

	t.Run("deposit bundled with a timeout", func(t *testing.T) {
		f := setup(t)
		seedFibreEscrow(t, f.testApp, f.addrs[0], 2*payment)
		txs := [][]byte{
			tx(t, f, 0, timeout(t, f, 0), &fibretypes.MsgDepositToEscrow{Signer: f.addrs[0].String(), Amount: sdk.NewInt64Coin(appconsts.BondDenom, payment)}),
			newSignedPayForFibreTxAt(t, f.signers[0], f.accounts[0], true, f.blockTime.Add(-time.Second), opts...),
		}
		status, _ := run(t, f, txs)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, status)
	})

	t.Run("withdrawal request before a PFF", func(t *testing.T) {
		// The request changes the pending withdrawals the PFF's settlement
		// charges gas for, and proposal does not execute it.
		f := setup(t)
		seedFibreEscrow(t, f.testApp, f.addrs[0], 10*payment)
		withdraw := tx(t, f, 0, &fibretypes.MsgRequestWithdrawal{Signer: f.addrs[0].String(), Amount: sdk.NewInt64Coin(appconsts.BondDenom, 10*payment)})
		pff := newSignedPayForFibreTxAt(t, f.signers[0], f.accounts[0], true, f.blockTime.Add(-time.Second), opts...)
		prep, err := f.testApp.PrepareProposal(&abci.RequestPrepareProposal{Txs: [][]byte{withdraw, pff}, Height: f.testApp.LastBlockHeight() + 1, Time: f.blockTime})
		require.NoError(t, err)
		require.Equal(t, [][]byte{withdraw}, prep.Txs)
		status, _ := run(t, f, [][]byte{withdraw, pff})
		require.Equal(t, abci.ResponseProcessProposal_REJECT, status)
	})

	t.Run("forged timeout does not block the owner's PFF", func(t *testing.T) {
		f := setup(t)
		seedFibreEscrow(t, f.testApp, f.addrs[0], 2*payment)
		// Account 1 submits a timeout for an expired promise that names the
		// owner's key but carries a forged owner signature.
		forged := timeout(t, f, 0)
		forged.Signer = f.addrs[1].String()
		forged.PaymentPromise.Signature = make([]byte, 64)
		txs := [][]byte{
			tx(t, f, 1, forged),
			newSignedPayForFibreTxAt(t, f.signers[0], f.accounts[0], true, f.blockTime.Add(-time.Second), opts...),
		}
		status, codes := run(t, f, txs)
		require.NotEqual(t, abci.CodeTypeOK, codes[0], "the forged timeout cannot settle")
		require.Equal(t, abci.CodeTypeOK, codes[1])
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, status)
	})

	t.Run("timeout before a PFF on another escrow", func(t *testing.T) {
		f := setup(t)
		seedFibreEscrow(t, f.testApp, f.addrs[0], payment)
		seedFibreEscrow(t, f.testApp, f.addrs[1], payment)
		txs := [][]byte{
			tx(t, f, 0, timeout(t, f, 0)),
			newSignedPayForFibreTxAt(t, f.signers[1], f.accounts[1], true, f.blockTime.Add(-time.Second), opts...),
		}
		status, codes := run(t, f, txs)
		require.Equal(t, []uint32{abci.CodeTypeOK, abci.CodeTypeOK}, codes)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, status)
	})
}

// TestPrepareProposalSkipsPayForFibreAfterEscrowChange checks that an honest
// proposer leaves out a PFF whose escrow a deposit changed earlier in the
// block, so its proposal passes ProcessProposal.
func TestPrepareProposalSkipsPayForFibreAfterEscrowChange(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	owner := testfactory.GetAddress(kr, accounts[0])
	payment := fibretypes.PaymentAmount(100).Amount.Int64()
	seedFibreEscrow(t, testApp, owner, payment)

	deposit, _, err := signer.CreateTx([]sdk.Msg{&fibretypes.MsgDepositToEscrow{Signer: owner.String(), Amount: sdk.NewInt64Coin(appconsts.BondDenom, payment)}},
		user.SetGasLimit(1_000_000), user.SetFee(4_000))
	require.NoError(t, err)
	require.NoError(t, signer.IncrementSequence(accounts[0]))
	pff := newSignedPayForFibreTx(t, signer, accounts[0], true)

	resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:    [][]byte{deposit, pff},
		Height: testApp.LastBlockHeight() + 1,
		Time:   time.Now(),
	})
	require.NoError(t, err)
	require.Equal(t, [][]byte{deposit}, resp.Txs)
}

// signedPromise returns a payment promise of account, signed by its owner.
func signedPromise(t *testing.T, signer *user.Signer, account string, creation time.Time) fibretypes.PaymentPromise {
	t.Helper()
	msg := blobfactory.NewMsgPayForFibre(t, signer.Account(account).PubKey().(*secp256k1.PubKey), testutil.ChainID)
	msg.PaymentPromise.CreationTimestamp = creation.Truncate(time.Second)
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	msg.PaymentPromise.Signature, _, err = signer.Keyring().Sign(account, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(t, err)
	return msg.PaymentPromise
}
