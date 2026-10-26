package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/da"
	"github.com/celestiaorg/celestia-app/v11/pkg/wrapper"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/nmt"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// TestPrepareProposalSquareAndRoot checks the kept txs, square size and data
// root against values computed independently from the expected txs.
func TestPrepareProposalSquareAndRoot(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(5)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	infos := queryAccountInfo(testApp, accounts, kr)

	sendTxs := coretypes.Txs(testutil.SendTxsWithAccounts(
		t, testApp, enc.TxConfig, kr, 1000, accounts[0], accounts[1:4], testutil.ChainID,
	)).ToSliceOfBytes()
	blob, err := share.NewBlob(share.RandomBlobNamespace(), []byte{1, 2, 3}, appconsts.DefaultShareVersion, nil)
	require.NoError(t, err)
	blobTx := blobfactory.ManyMultiBlobTx(t, enc.TxConfig, kr, testutil.ChainID,
		accounts[4:], infos[4:], [][]*share.Blob{{blob}})[0]

	framedSize := func(txs [][]byte) int64 {
		return coretypes.ComputeProtoSizeForTxs(coretypes.ToTxs(txs))
	}
	// Equal sizes make the byte-budget boundaries independent of tx order.
	require.Equal(t, framedSize(sendTxs[:1]), framedSize(sendTxs[1:2]))
	require.Equal(t, framedSize(sendTxs[:1]), framedSize(sendTxs[2:3]))

	tests := []struct {
		name       string
		txs        [][]byte
		maxTxBytes int64
		want       [][]byte
	}{
		{
			name: "empty proposal",
			want: [][]byte{},
		},
		{
			name: "only undecodable txs",
			txs:  [][]byte{{}, {1, 2, 3}},
			want: [][]byte{},
		},
		{
			name: "mixed valid, duplicate and undecodable txs",
			txs:  [][]byte{blobTx, sendTxs[0], {1, 2, 3}, sendTxs[0], sendTxs[1]},
			want: [][]byte{sendTxs[0], sendTxs[1], blobTx},
		},
		{
			name:       "byte budget exactly fits two txs",
			txs:        sendTxs,
			maxTxBytes: framedSize(sendTxs[:2]),
			want:       sendTxs[:2],
		},
		{
			name:       "byte budget one below two txs",
			txs:        sendTxs,
			maxTxBytes: framedSize(sendTxs[:2]) - 1,
			want:       sendTxs[:1],
		},
		{
			name:       "byte budget below a single tx",
			txs:        sendTxs,
			maxTxBytes: framedSize(sendTxs[:1]) - 1,
			want:       [][]byte{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
				Txs:        tc.txs,
				MaxTxBytes: tc.maxTxBytes,
				Height:     testApp.LastBlockHeight() + 1,
				Time:       time.Now(),
			})
			require.NoError(t, err)
			require.Equal(t, tc.want, resp.Txs)

			want := processProposalRequest(t, testApp, tc.want)
			require.Equal(t, want.SquareSize, resp.SquareSize)
			require.Equal(t, want.DataRootHash, resp.DataRootHash)
			if len(tc.want) == 0 {
				minDAH := da.MinDataAvailabilityHeader()
				require.Equal(t, uint64(1), resp.SquareSize)
				require.Equal(t, minDAH.Hash(), resp.DataRootHash)
			}
		})
	}
}

func TestPrepareProposalHandlerFailures(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), "account")
	req := &abci.RequestPrepareProposal{
		Height: testApp.LastBlockHeight() + 1,
		Time:   testutil.GenesisTime.Add(time.Hour),
	}
	header := cmtproto.Header{ChainID: testutil.ChainID, Height: req.Height, Time: req.Time}
	newContext := func() sdk.Context {
		return testApp.NewProposalContext(header).WithExecMode(sdk.ExecModePrepareProposal)
	}

	t.Run("invalid max square size", func(t *testing.T) {
		ctx := newContext()
		params := testApp.BlobKeeper.GetParams(ctx)
		params.GovMaxSquareSize = 3
		testApp.BlobKeeper.SetParams(ctx, params)
		require.Equal(t, 3, testApp.MaxEffectiveSquareSize(ctx))

		resp, err := testApp.PrepareProposalHandler(ctx, req)
		require.ErrorContains(t, err, "failed to create FilteredSquareBuilder: max square size must be a power of two")
		require.Nil(t, resp)
	})

	t.Run("begin blocker error", func(t *testing.T) {
		ctx := newContext()
		ctx.KVStore(testApp.GetKey(fibretypes.StoreKey)).Set(fibretypes.PromiseFreshnessFloorKey, []byte("invalid time"))

		resp, err := testApp.PrepareProposalHandler(ctx, req)
		require.ErrorContains(t, err, "failed to run fibre begin blocker on proposal branch: failed to parse promise freshness floor")
		require.Nil(t, resp)
	})

	t.Run("data root computation error", func(t *testing.T) {
		hasher := &failingProposalHasher{Hasher: nmt.NewNmtHasher(appconsts.NewBaseHashFunc(), share.NamespaceSize, true)}
		pool, err := wrapper.NewTreePool(1, 1, nmt.CustomHasher(hasher))
		require.NoError(t, err)
		originalPool := *testApp.TreePool()
		*testApp.TreePool() = *pool
		t.Cleanup(func() { *testApp.TreePool() = originalPool })

		resp, err := testApp.PrepareProposalHandler(newContext(), req)
		require.ErrorContains(t, err, "failure to create new data availability header: ")
		require.ErrorContains(t, err, "injected leaf hashing failure")
		require.Nil(t, resp)
		require.Positive(t, hasher.calls.Load())
		*testApp.TreePool() = originalPool
	})

	// Each failure was confined to its own branch or restored.
	resp, err := testApp.PrepareProposalHandler(newContext(), req)
	require.NoError(t, err)
	minDAH := da.MinDataAvailabilityHeader()
	require.Equal(t, &abci.ResponsePrepareProposal{
		Txs:          [][]byte{},
		SquareSize:   1,
		DataRootHash: minDAH.Hash(),
	}, resp)
}
