package app_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestPrepareProposalDifferential logs a digest of which inputs PrepareProposal keeps, in
// what order, for fixed workloads. It must not change when only proposal performance does.
func TestPrepareProposalDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prepare proposal differential test in short mode.")
	}

	var (
		numSends      = 2*appconsts.MaxSDKMessages + 100
		numMultiSends = 200
		numBlobs      = 2*appconsts.MaxPFBMessages + 50
		numPFFs       = 2*appconsts.MaxPayForFibreMessages + 50
		numExpiring   = 2 * appconsts.MaxSDKMessages
	)
	// One account per tx, so a dropped tx never breaks a later tx's sequence.
	accounts := testfactory.GenerateAccounts(numSends + numMultiSends + numBlobs + numPFFs + numExpiring)
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(app.DefaultConsensusParams(), 128, accounts...)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	signerAccounts := make([]*user.Account, len(accounts))
	for i, info := range queryAccountInfo(testApp, accounts, kr) {
		signerAccounts[i] = user.NewAccount(accounts[i], info.AccountNum, info.Sequence)
	}
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, signerAccounts...)
	require.NoError(t, err)

	next := 0
	nextAccount := func() (string, sdk.AccAddress) {
		name := accounts[next]
		next++
		return name, testfactory.GetAddress(kr, name)
	}
	sendTx := func(msgs int, opts ...user.TxOption) []byte {
		_, addr := nextAccount()
		sends := make([]sdk.Msg, msgs)
		for i := range sends {
			sends[i] = banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
		}
		opts = append([]user.TxOption{user.SetGasLimit(uint64(msgs) * 100_000), user.SetFee(uint64(msgs) * 10_000)}, opts...)
		rawTx, _, err := signer.CreateTx(sends, opts...)
		require.NoError(t, err)
		return rawTx
	}

	sends := make([][]byte, numSends)
	for i := range sends {
		sends[i] = sendTx(1)
	}
	multiSends := make([][]byte, numMultiSends)
	for i := range multiSends {
		multiSends[i] = sendTx(3)
	}
	ns := share.MustNewV0Namespace([]byte("diff-test"))
	blobs := make([][]byte, numBlobs)
	for i := range blobs {
		name, _ := nextAccount()
		blob, err := share.NewV0Blob(ns, []byte(fmt.Sprintf("blob %d", i)))
		require.NoError(t, err)
		blobs[i], _, err = signer.CreatePayForBlobs(name, []*share.Blob{blob}, user.SetGasLimit(300_000), user.SetFee(30_000))
		require.NoError(t, err)
	}
	pffs := make([][]byte, numPFFs)
	for i := range pffs {
		name, addr := nextAccount()
		seedFibreEscrow(t, testApp, addr, 1_000_000)
		pffs[i] = newSignedPayForFibreTx(t, signer, name, true)
	}
	// expiring txs pass CheckTx at the committed height but fail ante in the proposal, so
	// they are never included and must not push included txs out.
	expiring := make([][]byte, numExpiring)
	for i := range expiring {
		expiring[i] = sendTx(1, user.SetTimeoutHeight(uint64(testApp.LastBlockHeight())))
	}

	// interleave takes one tx from each list in turn until all are used.
	interleave := func(lists ...[][]byte) [][]byte {
		var out [][]byte
		for i := 0; ; i++ {
			added := false
			for _, l := range lists {
				if i < len(l) {
					out = append(out, l[i])
					added = true
				}
			}
			if !added {
				return out
			}
		}
	}
	// pattern repeats two 3-message txs and one 1-message tx, so the SDK count overshoots
	// the cap and a later 1-message tx still fits.
	pattern := make([][]byte, 0, numMultiSends*3/2)
	for i := 0; i+1 < numMultiSends; i += 2 {
		pattern = append(pattern, multiSends[i], multiSends[i+1], sends[i/2])
	}

	workloads := []struct {
		name     string
		txs      [][]byte
		wantKept int
	}{
		{"under-cap-mix", interleave(sends[:100], multiSends[:20], blobs[:50], pffs[:50]), 220},
		{"sdk-over-cap", sends, appconsts.MaxSDKMessages},
		// 85 triples reach 595; one more 3-message tx fits, then two 1-message txs.
		{"sdk-multi-msg-over-cap", pattern, 85*3 + 3},
		{"pfb-over-cap", blobs, appconsts.MaxPFBMessages},
		{"pff-over-cap", pffs, appconsts.MaxPayForFibreMessages},
		{"all-over-cap-interleaved", interleave(sends, blobs, pffs), 1000},
		{"all-over-cap-concatenated", append(append(append([][]byte{}, blobs...), pffs...), sends...), 1000},
		{"expiring-flood", append(append(append(append([][]byte{}, expiring...), sends[:10]...), blobs[:5]...), pffs[:5]...), 20},
		{"expiring-interleaved-over-cap", interleave(expiring, sends), appconsts.MaxSDKMessages},
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
