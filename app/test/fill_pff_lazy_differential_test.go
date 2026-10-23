package app_test

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// TestPrepareProposalPFFDifferential is TestPrepareProposalDifferential for PayForFibre txs that
// pass CheckTx and fail in the proposal. Only pff-ante-fail-flood may differ from before the fix.
func TestPrepareProposalPFFDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prepare proposal differential test in short mode.")
	}

	var (
		numValid    = 300
		numExpiring = appconsts.MaxPayForFibreCandidates / 2
		numUnfunded = appconsts.MaxPayForFibreCandidates / 2
		numBadCert  = 100
	)
	// One account per tx, so a dropped tx never breaks a later tx's sequence.
	accounts := testfactory.GenerateAccounts(numValid + numExpiring + numUnfunded + numBadCert)
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signerAccounts := make([]*user.Account, len(accounts))
	for i, info := range queryAccountInfo(testApp, accounts, kr) {
		signerAccounts[i] = user.NewAccount(accounts[i], info.AccountNum, info.Sequence)
	}
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, signerAccounts...)
	require.NoError(t, err)

	next := 0
	pffs := func(n int, funded, validCert bool, opts ...user.TxOption) [][]byte {
		txs := make([][]byte, n)
		for i := range txs {
			name := accounts[next]
			next++
			if funded {
				seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, name), 1_000_000)
			}
			txOpts := append([]user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(4_000)}, opts...)
			txs[i] = newSignedPayForFibreTxWithOpts(t, signer, name, validCert, txOpts...)
		}
		return txs
	}
	valid := pffs(numValid, true, true)
	// Expiring txs pass CheckTx at the committed height and fail ante in the proposal; unfunded
	// ones stand for an escrow drained since CheckTx; bad-cert ones fail signature verification.
	expiring := pffs(numExpiring, true, true, user.SetTimeoutHeight(uint64(testApp.LastBlockHeight())))
	unfunded := pffs(numUnfunded, false, true)
	badCert := pffs(numBadCert, true, false)

	interleave := func(a, b [][]byte) [][]byte {
		var out [][]byte
		for i := 0; i < len(a) || i < len(b); i++ {
			if i < len(a) {
				out = append(out, a[i])
			}
			if i < len(b) {
				out = append(out, b[i])
			}
		}
		return out
	}
	failing := interleave(expiring, unfunded)

	workloads := []struct {
		name     string
		txs      [][]byte
		wantKept int
	}{
		// The candidate bound used to fill with failing txs and keep no PayForFibre tx.
		{"pff-ante-fail-flood", slices.Concat(failing, valid[:50]), 50},
		// At least MaxPayForFibreMessages valid txs within the first MaxPayForFibreCandidates.
		{"pff-ante-fail-under-bound", slices.Concat(expiring[:150], valid), appconsts.MaxPayForFibreMessages},
		{"pff-ante-fail-interleaved-over-cap", interleave(failing, valid), appconsts.MaxPayForFibreMessages},
		{"pff-bad-cert-under-bound", slices.Concat(badCert, valid), appconsts.MaxPayForFibreMessages},
		{"pff-mixed-fail-under-cap", interleave(slices.Concat(badCert[:50], expiring[:50]), valid[:150]), 150},
	}
	for _, w := range workloads {
		t.Run(w.name, func(t *testing.T) {
			index := make(map[string]int, len(w.txs))
			for i, tx := range w.txs {
				index[string(tx)] = i
			}

			height := testApp.LastBlockHeight() + 1
			blockTime := time.Now()
			prepared, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{Height: height, Time: blockTime, Txs: w.txs})
			require.NoError(t, err)

			kept := make([]string, len(prepared.Txs))
			for i, tx := range prepared.Txs {
				idx, ok := index[string(tx)]
				require.True(t, ok, "output tx %d is not an input", i)
				kept[i] = fmt.Sprint(idx)
			}
			digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", prepared.SquareSize, strings.Join(kept, ","))))
			t.Logf("DIGEST %s in=%d kept=%d square=%d sha256=%x", w.name, len(w.txs), len(prepared.Txs), prepared.SquareSize, digest)

			processed, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Height: height, Time: blockTime, Txs: prepared.Txs,
				SquareSize: prepared.SquareSize, DataRootHash: prepared.DataRootHash,
			})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)
			require.Len(t, prepared.Txs, w.wantKept)
		})
	}
}
