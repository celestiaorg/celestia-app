//go:build fibre

package app_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v8/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
		_ = index
	}

	// Generate MaxPayForFibreMessages+1 signed MsgPayForFibre txs.
	pffTxs := make([][]byte, 0, numPFFs)
	for i := range numPFFs {
		pffTxs = append(pffTxs, newSignedPayForFibreTx(t, signers[i], accounts[i]))
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
	validPFFTx := newSignedPayForFibreTx(t, signer, accounts[0])

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
				return resp.Txs
			},
			expectedStatus: abci.ResponseProcessProposal_ACCEPT,
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

// newSignedPayForFibreTx creates a signed MsgPayForFibre transaction.
func newSignedPayForFibreTx(t *testing.T, signer *user.Signer, account string) []byte {
	t.Helper()
	acc := signer.Account(account)
	msg := blobfactory.NewMsgPayForFibre(t, acc.PubKey().(*secp256k1.PubKey), testutil.ChainID)
	txBytes, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(1_000_000), user.SetFee(1))
	require.NoError(t, err)
	return txBytes
}
