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

	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	signers := make([]*user.Signer, 0, numberOfAccounts)
	for index, account := range accounts {
		signers = append(signers, newSigner(index))
		seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, account), 1_000_000)
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
				classifiedTxs, err := fibretypes.ClassifyTxs(tc.txs)
				require.NoError(t, err)
				dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
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

// TestProcessProposalWithPayForFibre covers valid PFF blocks and invalid tx shapes.
func TestProcessProposalWithPayForFibre(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	signer := newSigner(0)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), 1_000_000)
	validPFFTx := newSignedPayForFibreTx(t, signer, accounts[0], true)
	invalidValidatorSignatureTx := newSignedPayForFibreTx(t, signer, accounts[0], false)

	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: validPFFTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
	checkResp, err = testApp.CheckTx(&abci.RequestCheckTx{Tx: invalidValidatorSignatureTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.NotEqual(t, abci.CodeTypeOK, checkResp.Code)

	blobSigner := newSigner(1)
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
				classifiedTxs, err := fibretypes.ClassifyTxs(txs)
				require.NoError(t, err)
				dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
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

	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)

	wrappedTx, err := coretypes.MarshalIndexWrapper(newSignedPayForFibreTx(t, signer, accounts[0], true), 0)
	require.NoError(t, err)
	txs := [][]byte{wrappedTx}

	classifiedTxs, err := fibretypes.ClassifyTxs(txs)
	require.NoError(t, err)
	dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
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

// newMsgPayForFibre creates a MsgPayForFibre with a random signer.
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

// newSignedPayForFibreTx creates a signed MsgPayForFibre tx.
func newSignedPayForFibreTx(
	t *testing.T,
	signer *user.Signer,
	account string,
	validValidatorSignatures bool,
) []byte {
	return newSignedPayForFibreTxWithOpts(t, signer, account, validValidatorSignatures,
		user.SetGasLimit(1_000_000), user.SetFee(4_000))
}

// newSignedPayForFibreTxWithOpts creates a signed MsgPayForFibre tx with options.
func newSignedPayForFibreTxWithOpts(
	t *testing.T,
	signer *user.Signer,
	account string,
	validValidatorSignatures bool,
	opts ...user.TxOption,
) []byte {
	t.Helper()
	return newSignedPayForFibreTxAt(t, signer, account, validValidatorSignatures, time.Now(), opts...)
}

// newSignedPayForFibreTxAt creates a signed PayForFibre tx with the given
// promise creation time. Equal times produce duplicate promises.
func newSignedPayForFibreTxAt(
	t *testing.T,
	signer *user.Signer,
	account string,
	validValidatorSignatures bool,
	creationTime time.Time,
	opts ...user.TxOption,
) []byte {
	t.Helper()
	acc := signer.Account(account)
	msg := blobfactory.NewMsgPayForFibre(t, acc.PubKey().(*secp256k1.PubKey), testutil.ChainID)
	// Keep tx size stable for gas parity tests.
	msg.PaymentPromise.CreationTimestamp = creationTime.Truncate(time.Second)

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
		// A 64-byte but cryptographically invalid signature: passes stateless
		// size validation, yet is rejected during signature verification.
		msg.ValidatorSignatures = [][]byte{make([]byte, 64)}
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

// TestProcessProposalPayForFibreStatefulChecks rejects txs that fail stateful checks.
func TestProcessProposalPayForFibreStatefulChecks(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	signers := make([]*user.Signer, len(accounts))
	for i := range accounts {
		signers[i] = newSigner(i)
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

// newPayForFibreTxPair creates two PayForFibre txs with sequential nonces and
// the given promise creation times.
func newPayForFibreTxPair(t *testing.T, signer *user.Signer, account string, first, second time.Time) [][]byte {
	t.Helper()
	firstTx := newSignedPayForFibreTxAt(t, signer, account, true, first,
		user.SetGasLimit(1_000_000), user.SetFee(4_000))
	require.NoError(t, signer.IncrementSequence(account))
	secondTx := newSignedPayForFibreTxAt(t, signer, account, true, second,
		user.SetGasLimit(1_000_000), user.SetFee(4_000))
	return [][]byte{firstTx, secondTx}
}

func TestProcessProposalPayForFibreDoubleSpend(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(3)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)

	// Every factory promise has BlobSize=100 and costs the same amount.
	payment := fibretypes.PaymentAmount(100).Amount.Int64()

	// The first account can afford one payment; the others can afford two.
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), payment)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[1]), 2*payment)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[2]), 2*payment)

	base := time.Now()
	overdrawTxs := newPayForFibreTxPair(t, newSigner(0), accounts[0], base.Add(-2*time.Second), base.Add(-time.Second))
	duplicateTxs := newPayForFibreTxPair(t, newSigner(1), accounts[1], base, base)
	affordableTxs := newPayForFibreTxPair(t, newSigner(2), accounts[2], base.Add(-2*time.Second), base.Add(-time.Second))

	tests := []struct {
		name           string
		txs            [][]byte
		expectedStatus abci.ResponseProcessProposal_ProposalStatus
	}{
		{
			name:           "reject two promises that collectively overdraw one escrow",
			txs:            overdrawTxs,
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "reject duplicate promise within one block",
			txs:            duplicateTxs,
			expectedStatus: abci.ResponseProcessProposal_REJECT,
		},
		{
			name:           "accept two promises the escrow can cover",
			txs:            affordableTxs,
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, tc.txs))
			require.NoError(t, err)
			require.Equal(t, tc.expectedStatus, resp.Status)
		})
	}
}

// TestProcessProposalChargesTxSizeGas checks the ProcessProposal gas boundary.
func TestProcessProposalChargesTxSizeGas(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(3)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	for _, account := range accounts {
		seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, account), 1_000_000)
	}

	// Measure ante-only and full execution gas. Neither call commits state.
	referenceTx := newSignedPayForFibreTx(t, newSigner(0), accounts[0], true)
	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: referenceTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
	anteGas := uint64(checkResp.GasUsed)

	finalizeResp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:   time.Now(),
		Height: testApp.LastBlockHeight() + 1,
		Hash:   testApp.LastCommitID().Hash,
		Txs:    [][]byte{referenceTx},
	})
	require.NoError(t, err)
	require.Len(t, finalizeResp.TxResults, 1)
	require.Equal(t, abci.CodeTypeOK, finalizeResp.TxResults[0].Code, finalizeResp.TxResults[0].Log)
	finalizeGas := uint64(finalizeResp.TxResults[0].GasUsed)
	require.Greater(t, finalizeGas, anteGas, "executing the payment must consume gas beyond ante")

	// The proposal is otherwise valid, so only the gas limit decides the result.
	t.Run("reject gas limit one below the ante cost", func(t *testing.T) {
		underTx := newSignedPayForFibreTxWithOpts(t, newSigner(1), accounts[1], true,
			user.SetGasLimit(anteGas-1), user.SetFee(4_000))
		require.Len(t, underTx, len(referenceTx), "boundary tx must match the reference tx size")

		resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, [][]byte{underTx}))
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	})

	t.Run("reject gas limit one below the FinalizeBlock cost", func(t *testing.T) {
		// Reject a tx without enough gas to execute its payment.
		underTx := newSignedPayForFibreTxWithOpts(t, newSigner(1), accounts[1], true,
			user.SetGasLimit(finalizeGas-1), user.SetFee(4_000))
		require.Len(t, underTx, len(referenceTx), "boundary tx must match the reference tx size")

		resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, [][]byte{underTx}))
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	})

	t.Run("accept gas limit just above the FinalizeBlock cost", func(t *testing.T) {
		// Store value sizes make gas vary slightly by account. This small margin
		// allows that variation while keeping the boundary tight.
		coveredTx := newSignedPayForFibreTxWithOpts(t, newSigner(2), accounts[2], true,
			user.SetGasLimit(finalizeGas+2_000), user.SetFee(4_000))
		require.Len(t, coveredTx, len(referenceTx), "boundary tx must match the reference tx size")

		resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, [][]byte{coveredTx}))
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
	})
}

// processProposalRequest builds a valid ProcessProposal request for the txs.
func processProposalRequest(t testing.TB, testApp *app.App, txs [][]byte) *abci.RequestProcessProposal {
	t.Helper()
	classifiedTxs, err := fibretypes.ClassifyTxs(txs)
	require.NoError(t, err)
	dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
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
