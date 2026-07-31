package app_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestProcessProposalCappingPayForFibreMessages(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping process proposal capping PayForFibre messages test in short mode.")
	}

	numPFFs := appconsts.MaxPayForFibreMessages + 1
	numberOfAccounts := numPFFs
	accounts := testfactory.GenerateAccounts(numberOfAccounts)
	consensusParams := app.DefaultConsensusParams()
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(consensusParams, 128, accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signers := make([]*user.Signer, 0, numberOfAccounts)
	for index, account := range accounts {
		addr := testfactory.GetAddress(kr, account)
		acc := testutil.DirectQueryAccount(testApp, addr)
		signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
		require.NoError(t, err)
		signers = append(signers, signer)
		seedFibreEscrow(t, testApp, addr, 1_000_000)
		_ = index
	}

	// Generate MaxPayForFibreMessages+1 signed MsgPayForFibre txs.
	pffTxs := make([][]byte, 0, numPFFs)
	for i := range numPFFs {
		pffTxs = append(pffTxs, newSignedPayForFibreTx(t, signers[i], accounts[i], true))
	}

	type testCase struct {
		name           string
		txs            [][]byte
		expectedResult abci.ResponseProcessProposal_ProposalStatus
	}

	testCases := []testCase{
		{
			name:           "reject block exceeding MaxPayForFibreMessages",
			txs:            pffTxs[:appconsts.MaxPayForFibreMessages+1],
			expectedResult: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "accept block at exactly MaxPayForFibreMessages",
			txs:            pffTxs[:appconsts.MaxPayForFibreMessages],
			expectedResult: abci.ResponseProcessProposal_ACCEPT,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var dataRootHash []byte
			var squareSize uint64
			if tc.expectedResult == abci.ResponseProcessProposal_ACCEPT {
				dataSquare, err := square.Construct(tc.txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
				require.NoError(t, err)
				dataRootHash = calculateNewDataHash(t, tc.txs)
				ss, err := dataSquare.Size()
				require.NoError(t, err)
				squareSize = uint64(ss)
			}

			resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Height:       testApp.LastBlockHeight() + 1,
				Time:         time.Now(),
				Txs:          tc.txs,
				DataRootHash: dataRootHash,
				SquareSize:   squareSize,
			})
			require.NoError(t, err)
			require.Equal(t, tc.expectedResult, resp.Status)
		})
	}
}

// TestProcessProposalWithPayForFibre verifies that ProcessProposal correctly
// handles PayForFibre transactions: accept/reject round-trips, rejection of
// multi-PFF txs, mixed PFF+MsgSend txs, and garbage bytes.
func TestProcessProposalWithPayForFibre(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID,
		user.NewAccount(accounts[0], infos[0].AccountNum, infos[0].Sequence))
	require.NoError(t, err)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), 1_000_000)
	validPFFTx := newSignedPayForFibreTx(t, signer, accounts[0], true)
	invalidValidatorSignatureTx := newSignedPayForFibreTx(t, signer, accounts[0], false)

	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: validPFFTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
	checkResp, err = testApp.CheckTx(&abci.RequestCheckTx{Tx: invalidValidatorSignatureTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.NotEqual(t, abci.CodeTypeOK, checkResp.Code)

	blobSigner, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID,
		user.NewAccount(accounts[1], infos[1].AccountNum, infos[1].Sequence))
	require.NoError(t, err)
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{0x02}, share.NamespaceVersionZeroIDSize))
	blob, err := share.NewBlob(ns, bytes.Repeat([]byte{0x01}, 100), share.ShareVersionZero, nil)
	require.NoError(t, err)
	blobTxBytes, _, err := blobSigner.CreatePayForBlobs(accounts[1], []*share.Blob{blob}, user.SetGasLimit(500_000), user.SetFee(1))
	require.NoError(t, err)

	tests := []struct {
		name           string
		txs            func() [][]byte
		expectedStatus abci.ResponseProcessProposal_ProposalStatus
	}{
		{
			name: "accept block with pay-for-fibre via prepare-process round-trip",
			txs: func() [][]byte {
				resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
					Txs:    [][]byte{validPFFTx},
					Height: testApp.LastBlockHeight() + 1,
					Time:   time.Now(),
				})
				require.NoError(t, err)
				require.Len(t, resp.Txs, 1)
				return resp.Txs
			},
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name: "accept block with blob tx and pay-for-fibre",
			txs: func() [][]byte {
				resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
					Txs:    [][]byte{blobTxBytes, validPFFTx},
					Height: testApp.LastBlockHeight() + 1,
					Time:   time.Now(),
				})
				require.NoError(t, err)
				require.Len(t, resp.Txs, 2)
				return resp.Txs
			},
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
		},
		{
			name: "reject pay-for-fibre with invalid validator signatures",
			txs: func() [][]byte {
				return [][]byte{invalidValidatorSignatureTx}
			},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "reject block with garbage bytes",
			txs: func() [][]byte {
				return [][]byte{bytes.Repeat([]byte{0xFF}, 64)}
			},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "reject tx with two MsgPayForFibre",
			txs: func() [][]byte {
				return [][]byte{newUnsignedMultiMsgTx(t, enc.TxConfig,
					newMsgPayForFibre(t), newMsgPayForFibre(t))}
			},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name: "reject tx with MsgPayForFibre mixed with MsgSend",
			txs: func() [][]byte {
				addr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
				sendMsg := banktypes.NewMsgSend(addr, addr,
					sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
				return [][]byte{newUnsignedMultiMsgTx(t, enc.TxConfig,
					newMsgPayForFibre(t), sendMsg)}
			},
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			txs := tc.txs()

			var dataRootHash []byte
			var squareSize uint64
			if tc.expectedStatus == abci.ResponseProcessProposal_ACCEPT {
				dataRootHash = calculateNewDataHash(t, txs)
				dataSquare, err := square.Construct(txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
				require.NoError(t, err)
				ss, err := dataSquare.Size()
				require.NoError(t, err)
				squareSize = uint64(ss)
			}

			resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Height:       testApp.LastBlockHeight() + 1,
				Time:         time.Now(),
				Txs:          txs,
				DataRootHash: dataRootHash,
				SquareSize:   squareSize,
			})
			require.NoError(t, err)
			require.Equal(t, tc.expectedStatus, resp.Status)
		})
	}
}

func TestProcessProposalRejectsIndexWrappedPFF(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID,
		user.NewAccount(accounts[0], infos[0].AccountNum, infos[0].Sequence))
	require.NoError(t, err)

	wrappedTx, err := coretypes.MarshalIndexWrapper(newSignedPayForFibreTx(t, signer, accounts[0], true), 0)
	require.NoError(t, err)
	txs := [][]byte{wrappedTx}

	dataSquare, err := square.Construct(txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	squareSize, err := dataSquare.Size()
	require.NoError(t, err)

	resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
		Height:       testApp.LastBlockHeight() + 1,
		Time:         time.Now(),
		Txs:          txs,
		DataRootHash: calculateNewDataHash(t, txs),
		SquareSize:   uint64(squareSize),
	})
	require.NoError(t, err)
	require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
}

// newMsgPayForFibre creates a MsgPayForFibre with a random signer for testing.
func newMsgPayForFibre(t *testing.T) *fibretypes.MsgPayForFibre {
	t.Helper()
	privKey := secp256k1.GenPrivKey()
	return blobfactory.NewMsgPayForFibre(t, privKey.PubKey().(*secp256k1.PubKey), "test")
}

// newUnsignedMultiMsgTx encodes multiple sdk.Msgs into an unsigned SDK tx.
func newUnsignedMultiMsgTx(t *testing.T, txConfig client.TxConfig, msgs ...sdk.Msg) []byte {
	t.Helper()
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(msgs...))
	txBytes, err := txConfig.TxEncoder()(builder.GetTx())
	require.NoError(t, err)
	return txBytes
}

// newSignedPayForFibreTx creates an SDK-signed MsgPayForFibre transaction with
// a valid owner signature and optionally valid validator signatures.
func newSignedPayForFibreTx(
	t *testing.T,
	signer *user.Signer,
	account string,
	validValidatorSignatures bool,
) []byte {
	return newSignedPayForFibreTxWithOpts(t, signer, account, validValidatorSignatures,
		user.SetGasLimit(1_000_000), user.SetFee(4_000))
}

// newSignedPayForFibreTxWithOpts is newSignedPayForFibreTx with caller-provided
// tx options (gas limit, fee).
func newSignedPayForFibreTxWithOpts(
	t *testing.T,
	signer *user.Signer,
	account string,
	validValidatorSignatures bool,
	opts ...user.TxOption,
) []byte {
	t.Helper()
	acc := signer.Account(account)
	msg := blobfactory.NewMsgPayForFibre(t, acc.PubKey().(*secp256k1.PubKey), testutil.ChainID)
	// Drop the nanoseconds so every tx encodes to the same size and pays the
	// same tx-size gas; the gas parity tests rely on this.
	msg.PaymentPromise.CreationTimestamp = msg.PaymentPromise.CreationTimestamp.Truncate(time.Second)

	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	msg.PaymentPromise.Signature, _, err = signer.Keyring().Sign(account, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(t, err)

	if validValidatorSignatures {
		validatorSignature, err := testutil.GenesisValidatorPrivateKey().Sign(signBytes)
		require.NoError(t, err)
		msg.ValidatorSignatures = [][]byte{validatorSignature}
	} else {
		msg.ValidatorSignatures = [][]byte{{0x01}}
	}

	txBytes, _, err := signer.CreateTx([]sdk.Msg{msg}, opts...)
	require.NoError(t, err)
	return txBytes
}

func seedFibreEscrow(t *testing.T, testApp *app.App, owner sdk.AccAddress, amount int64) {
	t.Helper()
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
		Time:    time.Now(),
	})
	coins := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, amount))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromAccountToModule(ctx, owner, fibretypes.ModuleName, coins))
	testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{
		Signer:           owner.String(),
		Balance:          coins[0],
		AvailableBalance: coins[0],
	})
}

// Reject proposals containing a MsgPayForFibre that fails stateful payment
// promise validation, even when all signatures are valid.
func TestProcessProposalPayForFibreStatefulChecks(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	signers := make([]*user.Signer, len(accounts))
	for i, account := range accounts {
		signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID,
			user.NewAccount(account, infos[i].AccountNum, infos[i].Sequence))
		require.NoError(t, err)
		signers[i] = signer
	}

	// accounts[0] has no escrow account; accounts[1] has an underfunded one.
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[1]), 1)

	tests := []struct {
		name string
		tx   []byte
	}{
		{
			name: "reject pay-for-fibre without an escrow account",
			tx:   newSignedPayForFibreTx(t, signers[0], accounts[0], true),
		},
		{
			name: "reject pay-for-fibre with insufficient escrow balance",
			tx:   newSignedPayForFibreTx(t, signers[1], accounts[1], true),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, [][]byte{tc.tx}))
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
		})
	}
}

// TestProcessProposalChargesTxSizeGas verifies that ProcessProposal charges
// the same ante gas as CheckTx: a gas limit one below the CheckTx-measured
// cost is rejected, exactly at it is accepted.
func TestProcessProposalChargesTxSizeGas(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(3)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	newSigner := func(index int) *user.Signer {
		signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID,
			user.NewAccount(accounts[index], infos[index].AccountNum, infos[index].Sequence))
		require.NoError(t, err)
		return signer
	}
	for _, account := range accounts {
		seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, account), 1_000_000)
	}

	// Measure the full ante gas of a valid PayForFibre tx. The reference tx and
	// the boundary txs below are identically sized, so their ante gas is equal.
	referenceTx := newSignedPayForFibreTx(t, newSigner(0), accounts[0], true)
	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: referenceTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
	anteGas := uint64(checkResp.GasUsed)

	// Both cases build an otherwise-valid proposal (correct square and data
	// root), so the gas limit is the only thing deciding accept vs reject.
	t.Run("reject gas limit one below the ante cost", func(t *testing.T) {
		underTx := newSignedPayForFibreTxWithOpts(t, newSigner(1), accounts[1], true,
			user.SetGasLimit(anteGas-1), user.SetFee(4_000))
		require.Len(t, underTx, len(referenceTx), "boundary tx must match the reference tx size")

		resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, [][]byte{underTx}))
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	})

	t.Run("accept gas limit at exactly the ante cost", func(t *testing.T) {
		exactTx := newSignedPayForFibreTxWithOpts(t, newSigner(2), accounts[2], true,
			user.SetGasLimit(anteGas), user.SetFee(4_000))
		require.Len(t, exactTx, len(referenceTx), "boundary tx must match the reference tx size")

		resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, [][]byte{exactTx}))
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
	})
}

// processProposalRequest builds a ProcessProposal request with a valid square
// and data root, so only tx-level validity can cause a reject.
func processProposalRequest(t *testing.T, testApp *app.App, txs [][]byte) *abci.RequestProcessProposal {
	t.Helper()
	dataSquare, err := square.Construct(txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	squareSize, err := dataSquare.Size()
	require.NoError(t, err)
	return &abci.RequestProcessProposal{
		Height:       testApp.LastBlockHeight() + 1,
		Time:         time.Now(),
		Txs:          txs,
		DataRootHash: calculateNewDataHash(t, txs),
		SquareSize:   uint64(squareSize),
	}
}
