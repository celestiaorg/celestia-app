package app_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"
)

// TestPrepareProposalDifferentialPFB logs the same digest as TestPrepareProposalDifferential
// for blob-only workloads around the PFB cap.
func TestPrepareProposalDifferentialPFB(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prepare proposal differential test in short mode.")
	}

	var (
		numExpiring = appconsts.MaxPFBMessages + 50
		numSmall    = 2*appconsts.MaxPFBMessages + 50
		numHeavy    = appconsts.MaxPFBMessages + 100
		numBig      = 150
	)
	accounts := testfactory.GenerateAccounts(numExpiring + numSmall + numHeavy + numBig)
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signerAccounts := make([]*user.Account, len(accounts))
	for i, info := range queryAccountInfo(testApp, accounts, kr) {
		signerAccounts[i] = user.NewAccount(accounts[i], info.AccountNum, info.Sequence)
	}
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, signerAccounts...)
	require.NoError(t, err)

	ns := share.MustNewV0Namespace([]byte("diff-pfb"))
	next := 0
	// blobTxs signs n PFBs, one account each, with one blob of size bytes.
	blobTxs := func(n, size int, gas uint64, opts ...user.TxOption) [][]byte {
		out := make([][]byte, n)
		for i := range out {
			name := accounts[next]
			next++
			data := bytes.Repeat([]byte{byte(next)}, size)
			blob, err := share.NewV0Blob(ns, data)
			require.NoError(t, err)
			opts := append([]user.TxOption{user.SetGasLimit(gas), user.SetFee(gas / 10)}, opts...)
			out[i], _, err = signer.CreatePayForBlobs(name, []*share.Blob{blob}, opts...)
			require.NoError(t, err)
		}
		return out
	}
	// expiring blob txs pass CheckTx at the committed height but fail ante in the proposal.
	expiring := blobTxs(numExpiring, 100, 300_000, user.SetTimeoutHeight(uint64(testApp.LastBlockHeight())))
	isExpiring := make(map[string]bool, len(expiring))
	for _, tx := range expiring {
		isExpiring[string(tx)] = true
	}
	small := blobTxs(numSmall, 100, 300_000)
	heavy := blobTxs(numHeavy, 8_500, 300_000)
	// big blobs fill the square before the PFB cap.
	big := blobTxs(numBig, 64_000, 1_000_000)

	interleave := func(a, b [][]byte) [][]byte {
		out := make([][]byte, 0, len(a)+len(b))
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

	workloads := []struct {
		name     string
		txs      [][]byte
		wantKept int
	}{
		{"pfb-expiring-flood", append(append([][]byte{}, expiring...), small...), appconsts.MaxPFBMessages},
		{"pfb-expiring-interleaved-over-cap", interleave(expiring, small), appconsts.MaxPFBMessages},
		{"blob-heavy-over-cap", heavy, appconsts.MaxPFBMessages},
		// The square holds 119 of the 64 KB blobs, so the PFB cap is never reached.
		{"blob-square-full", big, 119},
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
				require.False(t, isExpiring[string(tx)], "expiring tx %d kept", idx)
				kept[i] = fmt.Sprint(idx)
			}
			digest := sha256.Sum256([]byte(fmt.Sprintf("%d|%s", prepared.SquareSize, strings.Join(kept, ","))))
			require.Len(t, prepared.Txs, w.wantKept)
			t.Logf("DIGEST %s in=%d kept=%d square=%d sha256=%x", w.name, len(w.txs), len(prepared.Txs), prepared.SquareSize, digest)

			processed, err := testApp.ProcessProposal(&abci.RequestProcessProposal{
				Height: height, Time: blockTime, Txs: prepared.Txs,
				SquareSize: prepared.SquareSize, DataRootHash: prepared.DataRootHash,
			})
			require.NoError(t, err)
			require.Equal(t, abci.ResponseProcessProposal_ACCEPT, processed.Status)
		})
	}
}
