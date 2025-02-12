package proof_test

import (
	"bytes"
	"strings"
	"testing"

	tmrand "cosmossdk.io/math/unsafe"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"

	"github.com/celestiaorg/celestia-app/v4/pkg/da"
	"github.com/celestiaorg/celestia-app/v4/pkg/proof"
	square "github.com/celestiaorg/go-square/v2"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTxInclusionProof(t *testing.T) {
	blockTxs := testfactory.GenerateRandomTxs(50, 500).ToSliceOfBytes()

	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)

	blockTxs = append(blockTxs, blobfactory.RandBlobTxs(signer, tmrand.NewRand(), 50, 1, 500).ToSliceOfBytes()...)
	require.Len(t, blockTxs, 100)

	type test struct {
		name      string
		txs       [][]byte
		txIndex   uint64
		expectErr bool
	}
	tests := []test{
		{
			name:      "empty txs returns error",
			txs:       nil,
			txIndex:   0,
			expectErr: true,
		},
		{
			name:      "txIndex 0 of block data",
			txs:       blockTxs,
			txIndex:   0,
			expectErr: false,
		},
		{
			name:      "last regular transaction of block data",
			txs:       blockTxs,
			txIndex:   49,
			expectErr: false,
		},
		{
			name:      "first blobTx of block data",
			txs:       blockTxs,
			txIndex:   50,
			expectErr: false,
		},
		{
			name:      "last blobTx of block data",
			txs:       blockTxs,
			txIndex:   99,
			expectErr: false,
		},
		{
			name:      "txIndex 100 of block data returns error because only 100 txs",
			txs:       blockTxs,
			txIndex:   100,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proof, err := proof.NewTxInclusionProof(
				tt.txs,
				tt.txIndex,
				appconsts.LatestVersion,
			)
			if tt.expectErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.True(t, proof.VerifyProof())
		})
	}
}

func TestNewShareInclusionProof(t *testing.T) {
	ns1 := share.MustNewV0Namespace(bytes.Repeat([]byte{1}, share.NamespaceVersionZeroIDSize))
	ns2 := share.MustNewV0Namespace(bytes.Repeat([]byte{2}, share.NamespaceVersionZeroIDSize))
	ns3 := share.MustNewV0Namespace(bytes.Repeat([]byte{3}, share.NamespaceVersionZeroIDSize))

	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	blobTxs := blobfactory.RandBlobTxsWithNamespacesAndSigner(signer, []share.Namespace{ns1, ns2, ns3}, []int{500, 500, 500})
	txs := testfactory.GenerateRandomTxs(50, 500)
	txs = append(txs, blobTxs...)

	dataSquare, err := square.Construct(txs.ToSliceOfBytes(), appconsts.SquareSizeUpperBound(appconsts.LatestVersion), appconsts.SubtreeRootThreshold(appconsts.LatestVersion))
	if err != nil {
		panic(err)
	}

	// erasure the data square which we use to create the data root.
	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	require.NoError(t, err)

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	dataRoot := dah.Hash()

	type test struct {
		name          string
		startingShare int
		endingShare   int
		namespaceID   share.Namespace
		expectErr     bool
	}
	tests := []test{
		{
			name:          "negative starting share",
			startingShare: -1,
			endingShare:   99,
			namespaceID:   share.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "negative ending share",
			startingShare: 0,
			endingShare:   -99,
			namespaceID:   share.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "ending share lower than starting share",
			startingShare: 1,
			endingShare:   0,
			namespaceID:   share.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "ending share is equal to the starting share",
			startingShare: 1,
			endingShare:   1,
			namespaceID:   share.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "ending share higher than number of shares available in square size of 32",
			startingShare: 0,
			endingShare:   4097,
			namespaceID:   share.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "1 transaction share",
			startingShare: 0,
			endingShare:   1,
			namespaceID:   share.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "10 transaction shares",
			startingShare: 0,
			endingShare:   10,
			namespaceID:   share.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "53 transaction shares",
			startingShare: 0,
			endingShare:   53,
			namespaceID:   share.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "shares from different namespaces",
			startingShare: 48,
			endingShare:   55,
			namespaceID:   share.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "shares from PFB namespace",
			startingShare: 53,
			endingShare:   55,
			namespaceID:   share.PayForBlobNamespace,
			expectErr:     false,
		},
		{
			name:          "blob shares for first namespace",
			startingShare: 56,
			endingShare:   58,
			namespaceID:   ns1,
			expectErr:     false,
		},
		{
			name:          "blob shares for third namespace",
			startingShare: 60,
			endingShare:   62,
			namespaceID:   ns3,
			expectErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNID, err := proof.ParseNamespace(dataSquare, tt.startingShare, tt.endingShare)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.namespaceID, actualNID)
			proof, err := proof.NewShareInclusionProof(
				dataSquare,
				tt.namespaceID,
				share.NewRange(tt.startingShare, tt.endingShare),
			)
			require.NoError(t, err)
			assert.NoError(t, proof.Validate(dataRoot))
		})
	}
}

// TestAllSharesInclusionProof creates a proof for all shares in the data
// square. Since we can't prove multiple namespaces at the moment, all the
// shares use the same namespace.
func TestAllSharesInclusionProof(t *testing.T) {
	txs := testfactory.GenerateRandomTxs(243, 500)

	dataSquare, err := square.Construct(txs.ToSliceOfBytes(), appconsts.SquareSizeUpperBound(appconsts.LatestVersion), appconsts.SubtreeRootThreshold(appconsts.LatestVersion))
	require.NoError(t, err)
	assert.Equal(t, 256, len(dataSquare))

	// erasure the data square which we use to create the data root.
	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	require.NoError(t, err)

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	dataRoot := dah.Hash()

	actualNamespace, err := proof.ParseNamespace(dataSquare, 0, 256)
	require.NoError(t, err)
	require.Equal(t, share.TxNamespace, actualNamespace)
	proof, err := proof.NewShareInclusionProof(
		dataSquare,
		share.TxNamespace,
		share.NewRange(0, 256),
	)
	require.NoError(t, err)
	assert.NoError(t, proof.Validate(dataRoot))
}

// Ensure that we reject negative index values and avoid overflows.
// https://github.com/celestiaorg/celestia-app/issues/3140
func TestQueryTxInclusionProofRejectsNegativeValues(t *testing.T) {
	path := []string{"-2"}
	ctx := sdk.Context{}
	rawProof, err := proof.QueryTxInclusionProof(ctx, path, &abci.RequestQuery{Data: []byte{}})
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	if !strings.Contains(err.Error(), "negative") {
		t.Fatalf("The error should reject negative values and report such, but did not\n\tGot: %v", err)
	}
	if len(rawProof) != 0 {
		t.Fatal("no rawProof expected")
	}
}
