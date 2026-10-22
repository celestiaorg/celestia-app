package app_test

import (
	"bytes"
	"math/rand"
	"slices"
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
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
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

			if tc.expectedResult != abci.ResponseProcessProposal_REJECT {
				return
			}
			// The count is reached in the decode loop, before the
			// pre-verification pass. Nothing ran CheckTx on these txs, so a
			// cache entry could only have come from that pass.
			for i, rawTx := range tc.txs {
				d := fibrekeeper.DecodePayForFibre(rawTx)
				require.NotNil(t, d)
				require.False(t, testApp.HasVerifiedSignature(d.CertKey),
					"tx %d: certificate verified for a proposal rejected on its message count", i)
				require.False(t, testApp.HasVerifiedSignature(d.PromiseKey),
					"tx %d: promise verified for a proposal rejected on its message count", i)
			}
		})
	}
}

// TestProcessProposalParallelSquare runs every block shape through the square built
// alongside the walk: it must accept only the right root and size, and leave walk
// rejections to the walk.
func TestProcessProposalParallelSquare(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping parallel square test in short mode.")
	}

	type mix struct{ pff, pfb, send int }
	for _, tc := range []struct {
		name string
		mix  mix
	}{
		{"PayForFibre", mix{pff: 8}},
		{"PayForBlobs", mix{pfb: 8}},
		{"PayForFibre and PayForBlobs", mix{pff: 4, pfb: 4}},
		{"PayForFibre, PayForBlobs and MsgSend", mix{pff: 3, pfb: 3, send: 3}},
		{"MsgSend", mix{send: 8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testApp, txs := newMixedBlock(t, tc.mix.pff, tc.mix.pfb, tc.mix.send)
			process := func(t *testing.T, req *abci.RequestProcessProposal) abci.ResponseProcessProposal_ProposalStatus {
				t.Helper()
				resp, err := testApp.ProcessProposal(req)
				require.NoError(t, err)
				return resp.Status
			}

			t.Run("valid block", func(t *testing.T) {
				require.Equal(t, abci.ResponseProcessProposal_ACCEPT, process(t, processProposalRequest(t, testApp, txs)))
			})
			t.Run("bad tx signature at tx 0", func(t *testing.T) {
				block := slices.Clone(txs)
				block[0] = withCorruptTxSignature(t, block[0])
				require.Equal(t, abci.ResponseProcessProposal_REJECT, process(t, processProposalRequest(t, testApp, block)))
			})
			t.Run("wrong data root", func(t *testing.T) {
				req := processProposalRequest(t, testApp, txs)
				req.DataRootHash = bytes.Clone(req.DataRootHash)
				req.DataRootHash[0] ^= 0xFF
				require.Equal(t, abci.ResponseProcessProposal_REJECT, process(t, req))
			})
			t.Run("wrong square size", func(t *testing.T) {
				req := processProposalRequest(t, testApp, txs)
				req.SquareSize *= 2
				require.Equal(t, abci.ResponseProcessProposal_REJECT, process(t, req))
			})
			// A mismatched size yields no root, so an empty claimed root leaves the size check alone to reject.
			t.Run("wrong square size with empty data root", func(t *testing.T) {
				req := processProposalRequest(t, testApp, txs)
				req.SquareSize *= 2
				req.DataRootHash = nil
				require.Equal(t, abci.ResponseProcessProposal_REJECT, process(t, req))
			})
			if tc.mix.pfb > 0 {
				t.Run("bad share commitment", func(t *testing.T) {
					block := slices.Clone(txs)
					block[tc.mix.send] = withSwappedBlobData(t, block[tc.mix.send])
					require.Equal(t, abci.ResponseProcessProposal_REJECT, process(t, processProposalRequest(t, testApp, block)))
				})
			}
			// Last: the proposer's own proposal hits the proposal cache, which skips the square.
			t.Run("proposal cache hit", func(t *testing.T) {
				height := testApp.LastBlockHeight() + 1
				prep, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{Txs: txs, Height: height, Time: time.Now()})
				require.NoError(t, err)
				require.Equal(t, txs, prep.Txs)
				require.Equal(t, abci.ResponseProcessProposal_ACCEPT, process(t, &abci.RequestProcessProposal{
					Height:       height,
					Time:         time.Now(),
					Txs:          prep.Txs,
					DataRootHash: prep.DataRootHash,
					SquareSize:   prep.SquareSize,
				}))
			})
		})
	}
}

// newMixedBlock returns a valid block in PrepareProposal order (sends, blob txs,
// PayForFibre txs), one tx per funded account.
func newMixedBlock(t *testing.T, pff, pfb, send int) (*app.App, [][]byte) {
	t.Helper()
	n := pff + pfb + send
	accounts := testfactory.GenerateAccounts(n)
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, queryAccountInfo(testApp, accounts, kr))
	r := rand.New(rand.NewSource(1))

	txs := make([][]byte, 0, n)
	for i, account := range accounts {
		signer := newSigner(i)
		addr := testfactory.GetAddress(kr, account)
		switch {
		case i < send:
			to := testfactory.GetAddress(kr, accounts[(i+1)%n])
			msg := banktypes.NewMsgSend(addr, to, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)))
			tx, _, err := signer.CreateTx([]sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
			require.NoError(t, err)
			txs = append(txs, tx)
		case i < send+pfb:
			tx, _, err := signer.CreatePayForBlobs(account, blobfactory.ManyRandBlobs(r, 1024), blobfactory.DefaultTxOpts()...)
			require.NoError(t, err)
			txs = append(txs, tx)
		default:
			seedFibreEscrow(t, testApp, addr, 1_000_000)
			txs = append(txs, newSignedPayForFibreTx(t, signer, account, true))
		}
	}
	return testApp, txs
}

// withCorruptTxSignature flips a bit of the tx signature, unwrapping a blob tx if needed.
func withCorruptTxSignature(t *testing.T, rawTx []byte) []byte {
	t.Helper()
	blobTx, isBlobTx, err := blobtx.UnmarshalBlobTx(rawTx)
	if isBlobTx {
		require.NoError(t, err)
		rawTx = blobTx.Tx
	}
	var txRaw txtypes.TxRaw
	require.NoError(t, txRaw.Unmarshal(rawTx))
	txRaw.Signatures[0] = bytes.Clone(txRaw.Signatures[0])
	txRaw.Signatures[0][0] ^= 0x01
	corrupted, err := txRaw.Marshal()
	require.NoError(t, err)
	if !isBlobTx {
		return corrupted
	}
	corrupted, err = blobtx.MarshalBlobTx(corrupted, blobTx.Blobs...)
	require.NoError(t, err)
	return corrupted
}

// withSwappedBlobData replaces the first blob's data with same-sized data, so the
// PFB's share commitment no longer matches.
func withSwappedBlobData(t *testing.T, rawBlobTx []byte) []byte {
	t.Helper()
	blobTx, isBlobTx, err := blobtx.UnmarshalBlobTx(rawBlobTx)
	require.True(t, isBlobTx)
	require.NoError(t, err)
	original := blobTx.Blobs[0]
	swapped, err := share.NewBlob(original.Namespace(), bytes.Repeat([]byte{0xAB}, len(original.Data())), original.ShareVersion(), nil)
	require.NoError(t, err)
	blobTx.Blobs[0] = swapped
	out, err := blobtx.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
	require.NoError(t, err)
	return out
}

// TestProcessProposalWithPayForFibre covers valid PFF blocks and invalid tx shapes.
func TestProcessProposalWithPayForFibre(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(2)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	commitBlock(t, testApp)
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
	commitBlock(t, testApp)
	infos := queryAccountInfo(testApp, accounts, kr)

	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)
	for _, account := range accounts {
		seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, account), 1_000_000)
	}

	// Settlement stores the block time, whose protobuf encoding length varies
	// with its nanoseconds, so every measurement and proposal uses one time.
	blockTime := time.Now()

	// Measure ante-only and full execution gas. Neither call commits state.
	referenceTx := newSignedPayForFibreTxAt(t, newSigner(0), accounts[0], true, blockTime,
		user.SetGasLimit(1_000_000), user.SetFee(4_000))
	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: referenceTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
	anteGas := uint64(checkResp.GasUsed)

	finalizeResp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time:   blockTime,
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
		underTx := newSignedPayForFibreTxAt(t, newSigner(1), accounts[1], true, blockTime,
			user.SetGasLimit(anteGas-1), user.SetFee(4_000))
		require.Len(t, underTx, len(referenceTx), "boundary tx must match the reference tx size")

		resp, err := testApp.ProcessProposal(processProposalRequestAt(t, testApp, [][]byte{underTx}, blockTime))
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	})

	t.Run("reject gas limit one below the FinalizeBlock cost", func(t *testing.T) {
		// Reject a tx without enough gas to execute its payment.
		underTx := newSignedPayForFibreTxAt(t, newSigner(1), accounts[1], true, blockTime,
			user.SetGasLimit(finalizeGas-1), user.SetFee(4_000))
		require.Len(t, underTx, len(referenceTx), "boundary tx must match the reference tx size")

		resp, err := testApp.ProcessProposal(processProposalRequestAt(t, testApp, [][]byte{underTx}, blockTime))
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_REJECT, resp.Status)
	})

	t.Run("accept gas limit just above the FinalizeBlock cost", func(t *testing.T) {
		// Store value sizes make gas vary slightly by account. This small margin
		// allows that variation while keeping the boundary tight.
		coveredTx := newSignedPayForFibreTxAt(t, newSigner(2), accounts[2], true, blockTime,
			user.SetGasLimit(finalizeGas+2_000), user.SetFee(4_000))
		require.Len(t, coveredTx, len(referenceTx), "boundary tx must match the reference tx size")

		resp, err := testApp.ProcessProposal(processProposalRequestAt(t, testApp, [][]byte{coveredTx}, blockTime))
		require.NoError(t, err)
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, resp.Status)
	})
}

// processProposalRequest builds a valid ProcessProposal request for the txs.
func processProposalRequest(t testing.TB, testApp *app.App, txs [][]byte) *abci.RequestProcessProposal {
	t.Helper()
	return processProposalRequestAt(t, testApp, txs, time.Now())
}

// processProposalRequestAt builds a valid ProcessProposal request for the txs
// at the given block time.
func processProposalRequestAt(t testing.TB, testApp *app.App, txs [][]byte, blockTime time.Time) *abci.RequestProcessProposal {
	t.Helper()
	classifiedTxs, err := fibretypes.ClassifyTxs(txs)
	require.NoError(t, err)
	dataSquare, err := square.Construct(classifiedTxs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	squareSize, err := dataSquare.Size()
	require.NoError(t, err)
	return &abci.RequestProcessProposal{
		Height:       testApp.LastBlockHeight() + 1,
		Time:         blockTime,
		Txs:          txs,
		DataRootHash: calculateNewDataHash(t, txs),
		SquareSize:   uint64(squareSize),
	}
}

// TestProcessProposalPayForFibreTxSignature checks that PFF tx-signature
// pre-verification only warms the cache: every ante rejection still rejects.
func TestProcessProposalPayForFibreTxSignature(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), 1_000_000)

	base := time.Now()
	valid := newSignedPayForFibreTxAt(t, signer, accounts[0], true, base.Add(-3*time.Second),
		user.SetGasLimit(1_000_000), user.SetFee(4_000))

	var raw txtypes.TxRaw
	require.NoError(t, raw.Unmarshal(valid))
	require.Len(t, raw.Signatures, 1)
	raw.Signatures[0] = bytes.Clone(raw.Signatures[0])
	raw.Signatures[0][len(raw.Signatures[0])-1] ^= 0x01
	tampered, err := raw.Marshal()
	require.NoError(t, err)

	require.NoError(t, signer.IncrementSequence(accounts[0]))
	aheadOfSequence := newSignedPayForFibreTxAt(t, signer, accounts[0], true, base.Add(-2*time.Second),
		user.SetGasLimit(1_000_000), user.SetFee(4_000))
	require.NoError(t, signer.SetSequence(accounts[0], infos[0].Sequence))
	underpaid := newSignedPayForFibreTxAt(t, signer, accounts[0], true, base.Add(-time.Second),
		user.SetGasLimit(1_000_000), user.SetFee(0))
	pair := newPayForFibreTxPair(t, signer, accounts[0], base.Add(-5*time.Second), base.Add(-4*time.Second))

	tests := []struct {
		name           string
		txs            [][]byte
		expectedStatus abci.ResponseProcessProposal_ProposalStatus
	}{
		// Runs first, so its signature is cached for the cases below.
		{"accept a valid pay-for-fibre", [][]byte{valid}, abci.ResponseProcessProposal_ACCEPT},
		{"reject a tampered tx signature over a verified body", [][]byte{tampered}, abci.ResponseProcessProposal_REJECT},
		{"reject a validly signed tx ahead of the account sequence", [][]byte{aheadOfSequence}, abci.ResponseProcessProposal_REJECT},
		{"reject a validly signed tx paying below the minimum gas price", [][]byte{underpaid}, abci.ResponseProcessProposal_REJECT},
		{"accept one signer's txs in sequence order", pair, abci.ResponseProcessProposal_ACCEPT},
		{"reject one signer's txs out of sequence order", [][]byte{pair[1], pair[0]}, abci.ResponseProcessProposal_REJECT},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := testApp.ProcessProposal(processProposalRequest(t, testApp, tc.txs))
			require.NoError(t, err)
			require.Equal(t, tc.expectedStatus, resp.Status)
		})
	}
}

// TestProcessProposalPayForFibreVotesTheSameAfterHistoryIsPruned pins the
// invariant behind the signature cache: a node that admitted the tx must vote
// like one that never saw it once the promise height's history is pruned.
func TestProcessProposalPayForFibreVotesTheSameAfterHistoryIsPruned(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), 1_000_000)

	// Keep two historical entries so the promise height (1) is pruned while
	// still inside PaymentPromiseHeightWindow. The chain clock runs from genesis.
	setHistoricalEntries(t, testApp, 2)
	pffTx := newSignedPayForFibreTxAt(t, signer, accounts[0], true,
		testutil.GenesisTime.Add(time.Second),
		user.SetGasLimit(1_000_000), user.SetFee(4_000))

	// Admitting it warms the cache, the only difference between the two nodes.
	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: pffTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
	require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processProposalStatus(t, testApp, pffTx))

	for testApp.LastBlockHeight() < 5 {
		execBlock(t, testApp, func(sdk.Context) {})
	}
	_, err = testApp.StakingKeeper.GetHistoricalInfo(uncachedContext(testApp), 1)
	require.Error(t, err, "the promise height must be pruned for this test to mean anything")

	// A node that never ran CheckTx finds no historical info, so this one must not either.
	require.Equal(t, abci.ResponseProcessProposal_REJECT, processProposalStatus(t, testApp, pffTx))
}

// TestProcessProposalPayForFibreWarmValidatorSetVotesTheSameAfterPruning pins the
// miss path: a node that converted the promise height for another certificate
// must vote like a cold one once that height is pruned.
func TestProcessProposalPayForFibreWarmValidatorSetVotesTheSameAfterPruning(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)
	signer := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)(0)
	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), 1_000_000)
	setHistoricalEntries(t, testApp, 2)
	opts := []user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(4_000)}
	warmTx := newSignedPayForFibreTxAt(t, signer, accounts[0], true, testutil.GenesisTime.Add(time.Second), opts...)
	// Same promise height, different certificate: a sig-cache miss over a warm validator set.
	otherTx := newSignedPayForFibreTxAt(t, signer, accounts[0], true, testutil.GenesisTime.Add(2*time.Second), opts...)

	checkResp, err := testApp.CheckTx(&abci.RequestCheckTx{Tx: warmTx, Type: abci.CheckTxType_New})
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, checkResp.Code, checkResp.Log)
	for testApp.LastBlockHeight() < 5 {
		execBlock(t, testApp, func(sdk.Context) {})
	}
	_, err = testApp.StakingKeeper.GetHistoricalInfo(uncachedContext(testApp), 1)
	require.Error(t, err, "the promise height must be pruned for this test to mean anything")

	require.Equal(t, abci.ResponseProcessProposal_REJECT, processProposalStatus(t, testApp, otherTx))
}

// processProposalStatus proposes txs at the chain's clock and returns the vote.
func processProposalStatus(t *testing.T, testApp *app.App, txs ...[]byte) abci.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	req := processProposalRequest(t, testApp, txs)
	req.Time = testutil.GenesisTime.Add(time.Duration(req.Height) * time.Second)
	resp, err := testApp.ProcessProposal(req)
	require.NoError(t, err)
	return resp.Status
}
