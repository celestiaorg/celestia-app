package prove

import (
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/testutil/testfactory"

	"github.com/celestiaorg/celestia-app/pkg/da"
	nmtnamespace "github.com/celestiaorg/nmt/namespace"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
)

func TestNewShareInclusionProof(t *testing.T) {
	blobs := append(
		testfactory.GenerateBlobsWithNamespace(
			100,
			500,
			[]byte{0, 0, 0, 0, 0, 1, 0, 0},
		),
		append(
			testfactory.GenerateBlobsWithNamespace(
				50,
				500,
				[]byte{0, 0, 0, 1, 0, 0, 0, 0},
			),
			testfactory.GenerateBlobsWithNamespace(
				50,
				500,
				[]byte{0, 0, 1, 0, 0, 0, 0, 0},
			)...,
		)...,
	)
	sort.Sort(blobs)
	blockData := types.Data{
		Txs:        testfactory.GenerateRandomTxs(50, 500),
		Blobs:      blobs,
		SquareSize: 32,
	}

	// not setting useShareIndexes because the transactions indexes do not refer
	// to the messages because the square and transactions were created manually.
	rawShares, err := shares.Split(blockData, false)
	if err != nil {
		panic(err)
	}

	// erasure the data square which we use to create the data root.
	eds, err := da.ExtendShares(blockData.SquareSize, shares.ToBytes(rawShares))
	require.NoError(t, err)

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah := da.NewDataAvailabilityHeader(eds)
	dataRoot := dah.Hash()

	type test struct {
		name          string
		startingShare int64
		endingShare   int64
		namespaceID   nmtnamespace.ID
		expectErr     bool
	}
	tests := []test{
		{
			name:          "negative starting share",
			startingShare: -1,
			endingShare:   99,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "negative ending share",
			startingShare: 0,
			endingShare:   -99,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "ending share lower than starting share",
			startingShare: 1,
			endingShare:   0,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "ending share higher than number of shares available in square size of 32",
			startingShare: 0,
			endingShare:   4097,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "1 transaction share",
			startingShare: 0,
			endingShare:   0,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     false,
		},
		{
			name:          "10 transaction shares",
			startingShare: 0,
			endingShare:   9,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     false,
		},
		{
			name:          "50 transaction shares",
			startingShare: 0,
			endingShare:   49,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     false,
		},
		{
			name:          "shares from different namespaces",
			startingShare: 48,
			endingShare:   54,
			namespaceID:   appconsts.TxNamespaceID,
			expectErr:     true,
		},
		{
			name:          "20 custom namespace shares",
			startingShare: 106,
			endingShare:   125,
			namespaceID:   []byte{0, 0, 0, 0, 0, 1, 0, 0},
			expectErr:     false,
		},
		{
			name:          "40 custom namespace shares",
			startingShare: 355,
			endingShare:   394,
			namespaceID:   []byte{0, 0, 1, 0, 0, 0, 0, 0},
			expectErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actualNID, err := ParseNamespaceID(rawShares, tt.startingShare, tt.endingShare)
			if tt.expectErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.namespaceID, actualNID)
			proof, err := NewShareInclusionProof(
				rawShares,
				blockData.SquareSize,
				tt.namespaceID,
				uint64(tt.startingShare),
				uint64(tt.endingShare),
			)
			require.NoError(t, err)
			assert.NoError(t, proof.Validate(dataRoot))
		})
	}
}

// stripPrefix strips the universal prefix (namespace, info byte, sequence length) and
// reserved bytes from a list of compact shares and joins them into a single byte
// slice.
func stripPrefix(compactShares []shares.Share) (result []byte, err error) {
	for _, compactShare := range compactShares {
		rawData, err := compactShare.RawData()
		if err != nil {
			return []byte{}, err
		}
		result = append(result, rawData...)
	}
	return result, nil
}
