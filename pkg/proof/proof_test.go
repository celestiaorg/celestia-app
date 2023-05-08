package proof_test

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/proof"
	"github.com/celestiaorg/celestia-app/pkg/square"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTxInclusionProof(t *testing.T) {
	blockTxs := testfactory.GenerateRandomTxs(50, 500).ToSliceOfBytes()

	type test struct {
		name      string
		txs       [][]byte
		txIndex   uint64
		expectErr bool
	}
	tests := []test{
		{
			name:      "empty data returns error",
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
			name:      "txIndex 49 of block data",
			txs:       blockTxs,
			txIndex:   49,
			expectErr: false,
		},
		{
			name:      "txIndex 50 of block data returns error because only 50 txs",
			txs:       blockTxs,
			txIndex:   50,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proof, err := proof.NewTxInclusionProof(
				tt.txs,
				tt.txIndex,
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
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	ns2 := appns.MustNewV0(bytes.Repeat([]byte{2}, appns.NamespaceVersionZeroIDSize))
	ns3 := appns.MustNewV0(bytes.Repeat([]byte{3}, appns.NamespaceVersionZeroIDSize))

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	blobTxs := blobfactory.RandBlobTxsWithNamespaces(encCfg.TxConfig.TxEncoder(), []appns.Namespace{ns1, ns2, ns3}, []int{500, 500, 500})
	txs := testfactory.GenerateRandomTxs(50, 500)
	txs = append(txs, blobTxs...)

	// not setting useShareIndexes because the transactions indexes do not refer
	// to the messages because the square and transactions were created manually.
	dataSquare, err := square.Construct(txs.ToSliceOfBytes(), appconsts.DefaultMaxSquareSize)
	if err != nil {
		panic(err)
	}

	// erasure the data square which we use to create the data root.
	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	require.NoError(t, err)

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah := da.NewDataAvailabilityHeader(eds)
	dataRoot := dah.Hash()

	type test struct {
		name          string
		startingShare int64
		endingShare   int64
		namespaceID   appns.Namespace
		expectErr     bool
	}
	tests := []test{
		{
			name:          "negative starting share",
			startingShare: -1,
			endingShare:   99,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "negative ending share",
			startingShare: 0,
			endingShare:   -99,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "ending share lower than starting share",
			startingShare: 1,
			endingShare:   0,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "ending share higher than number of shares available in square size of 32",
			startingShare: 0,
			endingShare:   4097,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "1 transaction share",
			startingShare: 0,
			endingShare:   0,
			namespaceID:   appns.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "10 transaction shares",
			startingShare: 0,
			endingShare:   9,
			namespaceID:   appns.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "50 transaction shares",
			startingShare: 0,
			endingShare:   49,
			namespaceID:   appns.TxNamespace,
			expectErr:     false,
		},
		{
			name:          "shares from different namespaces",
			startingShare: 48,
			endingShare:   54,
			namespaceID:   appns.TxNamespace,
			expectErr:     true,
		},
		{
			name:          "20 custom namespace shares",
			startingShare: 106,
			endingShare:   125,
			namespaceID:   ns1,
			expectErr:     false,
		},
		{
			name:          "40 custom namespace shares",
			startingShare: 355,
			endingShare:   394,
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
				dataSquare.Size(),
				tt.namespaceID,
				uint64(tt.startingShare),
				uint64(tt.endingShare),
			)
			require.NoError(t, err)
			assert.NoError(t, proof.Validate(dataRoot))
		})
	}
}
