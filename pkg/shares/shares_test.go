package shares

import (
	"context"
	"math"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

func TestMerge(t *testing.T) {
	type test struct {
		name     string
		txCount  int
		evdCount int
		msgCount int
		maxSize  int // max size of each tx or msg
	}

	tests := []test{
		{"one of each random small size", 1, 1, 1, 40},
		{"one of each random large size", 1, 1, 1, 400},
		{"many of each random large size", 10, 10, 10, 40},
		{"many of each random large size", 10, 10, 10, 400},
		{"only transactions", 10, 0, 0, 400},
		{"only evidence", 0, 10, 0, 400},
		{"only messages", 0, 0, 10, 400},
	}

	for _, tc := range tests {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			// generate random data
			data := generateRandomBlockData(
				tc.txCount,
				tc.evdCount,
				tc.msgCount,
				tc.maxSize,
			)

			shares, err := Split(data, false)
			require.NoError(t, err)
			rawShares := ToBytes(shares)

			eds, err := rsmt2d.ComputeExtendedDataSquare(rawShares, appconsts.DefaultCodec(), rsmt2d.NewDefaultTree)
			if err != nil {
				t.Error(err)
			}

			res, err := merge(eds)
			if err != nil {
				t.Fatal(err)
			}

			// we have to compare the evidence by string because the the
			// timestamps differ not by actual time represented, but by
			// internals see https://github.com/stretchr/testify/issues/666
			for i := 0; i < len(data.Evidence.Evidence); i++ {
				inputEvidence := data.Evidence.Evidence[i].(*coretypes.DuplicateVoteEvidence)
				resultEvidence := res.Evidence.Evidence[i].(*coretypes.DuplicateVoteEvidence)
				assert.Equal(t, inputEvidence.String(), resultEvidence.String())
			}

			// compare the original to the result w/o the evidence
			data.Evidence = coretypes.EvidenceData{}
			res.Evidence = coretypes.EvidenceData{}

			res.SquareSize = data.SquareSize

			assert.Equal(t, data, res)
		})
	}
}

func TestFuzz_Merge(t *testing.T) {
	t.Skip()
	// run random shares through processCompactShares for a minute
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			TestMerge(t)
		}
	}
}

// generateRandomBlockData returns randomly generated block data for testing purposes
func generateRandomBlockData(txCount, evdCount, msgCount, maxSize int) (data coretypes.Data) {
	data.Txs = generateRandomlySizedTransactions(txCount, maxSize)
	data.Evidence = generateIdenticalEvidence(evdCount)
	data.Blobs = generateRandomlySizedBlobs(msgCount, maxSize)
	data.SquareSize = appconsts.MaxSquareSize
	return data
}

func generateRandomlySizedTransactions(count, maxSize int) coretypes.Txs {
	txs := make(coretypes.Txs, count)
	for i := 0; i < count; i++ {
		size := rand.Intn(maxSize)
		if size == 0 {
			size = 1
		}
		txs[i] = generateRandomTransaction(1, size)[0]
	}
	return txs
}

func generateRandomTransaction(count, size int) coretypes.Txs {
	txs := make(coretypes.Txs, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		_, err := rand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}

func generateIdenticalEvidence(count int) coretypes.EvidenceData {
	evidence := make([]coretypes.Evidence, count)
	for i := 0; i < count; i++ {
		ev := coretypes.NewMockDuplicateVoteEvidence(math.MaxInt64, time.Now(), "chainID")
		evidence[i] = ev
	}
	return coretypes.EvidenceData{Evidence: evidence}
}

func generateRandomlySizedBlobs(count, maxBlobSize int) []coretypes.Blob {
	blobs := make([]coretypes.Blob, count)
	for i := 0; i < count; i++ {
		blobs[i] = generateRandomBlob(rand.Intn(maxBlobSize))
		if len(blobs[i].Data) == 0 {
			i--
		}
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		blobs = nil
	}

	sort.Sort(types.BlobsByNamespace(blobs))
	return blobs
}

// generateRandomBlob returns a random blob of the given size (in bytes)
func generateRandomBlob(size int) coretypes.Blob {
	blob := coretypes.Blob{
		NamespaceID:  tmrand.Bytes(appconsts.NamespaceSize),
		Data:         tmrand.Bytes(size),
		ShareVersion: appconsts.ShareVersionZero,
	}
	return blob
}

// generateRandomMessageOfShareCount returns a message that spans the given
// number of shares
func generateRandomMessageOfShareCount(count int) coretypes.Blob {
	size := rawMessageSize(appconsts.SparseShareContentSize * count)
	return generateRandomBlob(size)
}

// rawMessageSize returns the raw message size that can be used to construct a
// message of totalSize bytes. This function is useful in tests to account for
// the delimiter length that is prefixed to a message's data.
func rawMessageSize(totalSize int) int {
	return totalSize - DelimLen(uint64(totalSize))
}
