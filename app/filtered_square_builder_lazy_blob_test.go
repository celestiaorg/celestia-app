package app

import (
	"math"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// errorSpyLogger records the messages of Error lines.
type errorSpyLogger struct {
	log.Logger
	errors *[]string
}

func (l errorSpyLogger) Error(msg string, _ ...any) { *l.errors = append(*l.errors, msg) }

func (l errorSpyLogger) With(...any) log.Logger { return l }

// TestFillDoesNotDecodeBlobTxsPastCap pins that blob txs are decoded only by the blob loop, so
// malformed ones past the PFB cap are never decoded, logged or counted.
func TestFillDoesNotDecodeBlobTxsPastCap(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	valid := make([][]byte, appconsts.MaxPFBMessages)
	for i := range valid {
		valid[i] = newBlobTx(t, txConfig)
	}
	malformed := make([][]byte, 50)
	for i := range malformed {
		malformed[i] = appendUnknownProtoField(newBlobTx(t, txConfig), 16)
	}

	fill := func(txs [][]byte) (kept [][]byte, malformedLogs int) {
		alwaysPass := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil }
		fsb, err := NewFilteredSquareBuilder(alwaysPass, nil, txConfig, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
		require.NoError(t, err)
		fsb.validateBlobTx = func(bTx *blobtx.BlobTx) (bool, error) {
			return false, blobtypes.ValidateBlobTx(txConfig, bTx, appconsts.SubtreeRootThreshold, appconsts.Version)
		}
		var errors []string
		ms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())
		ctx := sdk.NewContext(ms, cmtproto.Header{}, false, errorSpyLogger{log.NewNopLogger(), &errors})
		kept = fsb.Fill(ctx, txs, math.MaxInt64)
		for _, msg := range errors {
			if msg == "dropping malformed blob tx" {
				malformedLogs++
			}
		}
		require.Len(t, fsb.KeptBlobTxs(), len(kept))
		for i, bTx := range fsb.KeptBlobTxs() {
			want, isBlob, err := blobtx.UnmarshalBlobTx(kept[i])
			require.True(t, isBlob)
			require.NoError(t, err)
			require.Equal(t, want, bTx, "kept blob tx %d", i)
		}
		return kept, malformedLogs
	}

	kept, logs := fill(append(append([][]byte{}, valid...), malformed...))
	require.Equal(t, valid, kept)
	require.Zero(t, logs, "a malformed blob tx past the cap was decoded")

	kept, logs = fill(append(append([][]byte{}, malformed...), valid...))
	require.Equal(t, valid, kept)
	require.Equal(t, len(malformed), logs)
}
