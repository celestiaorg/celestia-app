package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
)

// TestProcessProposalRebuildsSquareAfterReverts pins the equivalence between the
// square FilteredSquareBuilder builds incrementally in PrepareProposal and the
// one constructSquare rebuilds in a single pass in ProcessProposal, across the
// builder's three revert paths.
//
// The proposal cache makes this worth pinning explicitly. The proposer's own
// ProcessProposal used to rebuild the square and compare the result against the
// data root it had stated, which cross-checked the two builders on every block
// it proposed. On a cache hit it no longer rebuilds, so the round trips in this
// package that take a hit have stopped covering that equivalence, and
// RevertLastTx, RevertLastBlobTx and RevertLastPayForFibreTx are not
// round-tripped through a rebuild anywhere else.
//
// Each case builds a proposal whose second transaction is appended to the square
// and then reverted, and asserts the rebuild accepts it. The second
// ProcessProposal call is the one that matters: Take empties the cache on every
// call, so the first reuses PrepareProposal's artifacts and the second
// recomputes them from the tx bytes.
func TestProcessProposalRebuildsSquareAfterReverts(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// Every transaction below is small enough to fit the square and stays well
	// inside the message caps, so the only path that can drop the second one is
	// an append followed by a revert.
	tests := []struct {
		name   string
		revert string
		txs    func(t *testing.T, testApp *app.App, kr keyring.Keyring, accounts []string, newSigner func(int) *user.Signer) [][]byte
	}{
		{
			name:   "normal tx reverted after the ante handler rejects it",
			revert: "RevertLastTx",
			txs: func(t *testing.T, testApp *app.App, kr keyring.Keyring, accounts []string, _ func(int) *user.Signer) [][]byte {
				seq, num := accountSequence(testApp, kr, accounts[0])
				// The second tx skips a sequence number, so the ante handler
				// rejects it after AppendTx has already placed it.
				send := func(sequence uint64) []byte {
					const gasLimit = 200_000
					fee := uint64(float64(gasLimit)*appconsts.DefaultMinGasPrice) + 1
					return testutil.SendTxWithManualSequence(t, enc.TxConfig, kr, accounts[0], accounts[1], 1,
						testutil.ChainID, sequence, num, user.SetGasLimit(gasLimit), user.SetFee(fee))
				}
				return [][]byte{send(seq), send(seq + 2)}
			},
		},
		{
			name:   "blob tx reverted after the ante handler rejects it",
			revert: "RevertLastBlobTx",
			txs: func(t *testing.T, testApp *app.App, kr keyring.Keyring, accounts []string, _ func(int) *user.Signer) [][]byte {
				seq, num := accountSequence(testApp, kr, accounts[0])
				return [][]byte{
					testutil.BlobTxWithManualSequence(t, enc.TxConfig, kr, 100, 1, testutil.ChainID, accounts[0], seq, num),
					testutil.BlobTxWithManualSequence(t, enc.TxConfig, kr, 100, 1, testutil.ChainID, accounts[0], seq+2, num),
				}
			},
		},
		{
			name:   "pay-for-fibre tx reverted after settlement fails",
			revert: "RevertLastPayForFibreTx",
			txs: func(t *testing.T, testApp *app.App, kr keyring.Keyring, accounts []string, newSigner func(int) *user.Signer) [][]byte {
				// Fund exactly one payment, so the second promise overdraws the
				// escrow and settlement fails after AppendFibreTx succeeded.
				payment := fibretypes.PaymentAmount(100).Amount.Int64()
				seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[0]), payment)
				base := time.Now()
				return newPayForFibreTxPair(t, newSigner(0), accounts[0], base.Add(-2*time.Second), base.Add(-time.Second))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			accounts := testfactory.GenerateAccounts(2)
			testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
			infos := queryAccountInfo(testApp, accounts, kr)
			newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)

			txs := tc.txs(t, testApp, kr, accounts, newSigner)
			require.Len(t, txs, 2)

			// Prepare and process must see the same block time: pay-for-fibre
			// promise freshness is judged against it.
			now := time.Now()
			height := testApp.LastBlockHeight() + 1

			prepared, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
				Txs:    txs,
				Height: height,
				Time:   now,
			})
			require.NoError(t, err)

			// The revert fired: the first tx was kept and the second was
			// appended and taken back out. If this ever reports 2, the case has
			// stopped exercising tc.revert and the rebuild below proves nothing
			// about it.
			require.Len(t, prepared.Txs, 1, "second tx must be dropped via %s", tc.revert)
			require.Equal(t, txs[0], prepared.Txs[0])

			process := func() abci.ResponseProcessProposal_ProposalStatus {
				resp, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
					Height:       height,
					Time:         now,
					Txs:          prepared.Txs,
					DataRootHash: prepared.DataRootHash,
					SquareSize:   prepared.SquareSize,
				})
				require.NoError(t, err)
				return resp.Status
			}

			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, process(),
				"reuse path must accept the block the proposer built")
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, process(),
				"rebuild path must derive the same square and data root after %s", tc.revert)
		})
	}
}

// accountSequence returns the on-chain sequence and account number for account.
func accountSequence(testApp *app.App, kr keyring.Keyring, account string) (sequence, accountNumber uint64) {
	info := testutil.DirectQueryAccount(testApp, testfactory.GetAddress(kr, account))
	return info.GetSequence(), info.GetAccountNumber()
}
