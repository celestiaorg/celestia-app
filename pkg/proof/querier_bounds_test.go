package proof_test

import (
	"bytes"
	"runtime"
	"strconv"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/proof"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/random"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

// maxRejectAllocs bounds the allocations of a rejected query. It is far below
// the tx count of the blocks below, so a handler that allocates per tx fails.
const maxRejectAllocs = 16

// encodeBlock returns the serialized block holding txs.
func encodeBlock(t *testing.T, txs [][]byte) []byte {
	t.Helper()
	raw, err := (&tmproto.Block{Data: tmproto.Data{Txs: txs}}).Marshal()
	require.NoError(t, err)
	return raw
}

// manyTxsBlock returns a serialized block holding n txs of txSize bytes each.
func manyTxsBlock(t *testing.T, n, txSize int) []byte {
	t.Helper()
	txs := make([][]byte, n)
	for i := range txs {
		txs[i] = bytes.Repeat([]byte{0x01}, txSize)
	}
	return encodeBlock(t, txs)
}

func TestQueryTxInclusionProofRejectsOutOfRangeIndexBeforeDecoding(t *testing.T) {
	const n = 100_000
	req := &abci.RequestQuery{Data: manyTxsBlock(t, n, 1)}
	for _, index := range []string{strconv.Itoa(n), strconv.Itoa(n + 1), "9223372036854775807"} {
		var err error
		allocs := testing.AllocsPerRun(3, func() {
			_, err = proof.QueryTxInclusionProof(sdk.Context{}, []string{index}, req)
		})
		require.ErrorContains(t, err, "out of bounds")
		require.LessOrEqual(t, allocs, float64(maxRejectAllocs), "index %s", index)
	}
}

func TestQueryProofsRejectEmptyTxsBeforeDecoding(t *testing.T) {
	req := &abci.RequestQuery{Data: manyTxsBlock(t, 100_000, 0)}
	var err error
	allocs := testing.AllocsPerRun(3, func() {
		_, err = proof.QueryTxInclusionProof(sdk.Context{}, []string{"0"}, req)
	})
	require.ErrorContains(t, err, "empty tx")
	require.LessOrEqual(t, allocs, float64(maxRejectAllocs))

	allocs = testing.AllocsPerRun(3, func() {
		_, err = proof.QueryShareInclusionProof(sdk.Context{}, []string{"0", "1"}, req)
	})
	require.ErrorContains(t, err, "empty tx")
	require.LessOrEqual(t, allocs, float64(maxRejectAllocs))

	// One empty tx among non-empty ones is rejected too.
	txs := [][]byte{{0x01}, {0x02}, {}}
	_, err = proof.QueryTxInclusionProof(sdk.Context{}, []string{"0"}, &abci.RequestQuery{Data: encodeBlock(t, txs)})
	require.ErrorContains(t, err, "empty tx at index 2")
}

func TestQueryShareInclusionProofRejectsInvalidRangeBeforeDecoding(t *testing.T) {
	req := &abci.RequestQuery{Data: manyTxsBlock(t, 100_000, 1)}
	maxShares := appconsts.SquareSizeUpperBound * appconsts.SquareSizeUpperBound
	for _, path := range [][]string{
		{"-1", "1"},
		{"1", "1"},
		{"2", "1"},
		{"0", strconv.Itoa(maxShares + 1)},
	} {
		var err error
		allocs := testing.AllocsPerRun(3, func() {
			_, err = proof.QueryShareInclusionProof(sdk.Context{}, path, req)
		})
		require.ErrorContains(t, err, "invalid share range", "path %v", path)
		require.LessOrEqual(t, allocs, float64(maxRejectAllocs), "path %v", path)
	}
}

// allocatedBytes returns the bytes the heap allocated while running f.
func allocatedBytes(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

// TestQueryProofsRejectMalformedBlockBeforeAllocatingTxs checks that a block
// the decoder rejects before reaching its txs costs no allocation per tx, even
// when it holds many txs.
func TestQueryProofsRejectMalformedBlockBeforeAllocatingTxs(t *testing.T) {
	const n = 1_000_000
	txField := func(b []byte) []byte {
		for range n {
			b = protowire.AppendTag(b, 1, protowire.BytesType)
			b = protowire.AppendBytes(b, []byte{0x01})
		}
		return b
	}
	dataField := func(data []byte) []byte {
		b := protowire.AppendTag(nil, 2, protowire.BytesType)
		return protowire.AppendBytes(b, data)
	}
	// A header holding its version field (a message) as a varint.
	badHeader := protowire.AppendTag(nil, 1, protowire.BytesType)
	badHeader = protowire.AppendBytes(badHeader, protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 0))
	// A header encoded as a varint.
	headerAsVarint := protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 0)
	// A square size encoded as bytes.
	squareSizeAsBytes := protowire.AppendBytes(protowire.AppendTag(nil, 5, protowire.BytesType), []byte{0x01})

	for name, data := range map[string][]byte{
		"block field with wrong wire type": append(headerAsVarint, dataField(txField(nil))...),
		"data field with wrong wire type":  dataField(txField(squareSizeAsBytes)),
		"malformed header before txs":      append(badHeader, dataField(txField(nil))...),
	} {
		req := &abci.RequestQuery{Data: data}
		var err error
		allocated := allocatedBytes(func() {
			_, err = proof.QueryTxInclusionProof(sdk.Context{}, []string{"0"}, req)
		})
		require.Error(t, err, name)
		require.Less(t, allocated, uint64(1<<20), "%s: tx query allocated %d bytes", name, allocated)

		allocated = allocatedBytes(func() {
			_, err = proof.QueryShareInclusionProof(sdk.Context{}, []string{"0", "1"}, req)
		})
		require.Error(t, err, name)
		require.Less(t, allocated, uint64(1<<20), "%s: share query allocated %d bytes", name, allocated)
	}
}

// legacyQueryTxInclusionProof and legacyQueryShareInclusionProof compute
// proofs the way the query handlers did before they validated requests ahead
// of decoding the block.
func legacyQueryTxInclusionProof(t *testing.T, index int, data []byte) []byte {
	t.Helper()
	pbb := new(tmproto.Block)
	require.NoError(t, pbb.Unmarshal(data))
	blockData, err := types.DataFromProto(&pbb.Data)
	require.NoError(t, err)
	sp, err := proof.NewTxInclusionProof(blockData.Txs.ToSliceOfBytes(), uint64(index), pbb.Header.Version.App)
	require.NoError(t, err)
	raw, err := sp.Marshal()
	require.NoError(t, err)
	return raw
}

func legacyQueryShareInclusionProof(t *testing.T, begin, end int, data []byte) []byte {
	t.Helper()
	pbb := new(tmproto.Block)
	require.NoError(t, pbb.Unmarshal(data))
	classified, err := fibretypes.ClassifyTxs(pbb.Data.Txs)
	require.NoError(t, err)
	sq, err := square.Construct(classified, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	ns, err := proof.ParseNamespace(sq, begin, end)
	require.NoError(t, err)
	sp, err := proof.NewShareInclusionProof(sq, ns, share.NewRange(begin, end))
	require.NoError(t, err)
	raw, err := sp.Marshal()
	require.NoError(t, err)
	return raw
}

// TestQueryProofsMatchLegacyHandlers checks that the handlers return proofs
// byte-identical to the previous implementation for valid requests.
func TestQueryProofsMatchLegacyHandlers(t *testing.T) {
	// Normal, blob and pay-for-fibre txs, in square order.
	txs := testfactory.GenerateRandomTxs(10, 500).ToSliceOfBytes()
	signer, err := testnode.NewOfflineSigner()
	require.NoError(t, err)
	txs = append(txs, blobfactory.RandBlobTxs(signer, random.New(), 5, 2, 700).ToSliceOfBytes()...)
	txConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...).TxConfig
	txs = append(txs, blobfactory.UnsignedPayForFibreTx(t, txConfig))
	data := encodeBlock(t, txs)

	for i := range txs {
		got, err := proof.QueryTxInclusionProof(sdk.Context{}, []string{strconv.Itoa(i)}, &abci.RequestQuery{Data: data})
		require.NoError(t, err)
		require.Equal(t, legacyQueryTxInclusionProof(t, i, data), got, "tx %d", i)
	}

	classified, err := fibretypes.ClassifyTxs(txs)
	require.NoError(t, err)
	sq, err := square.Construct(classified, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	for begin := range len(sq) {
		// end is the end of the namespace run that begin starts.
		end := begin + 1
		for end < len(sq) && sq[end].Namespace().Equals(sq[begin].Namespace()) {
			end++
		}
		for _, e := range []int{begin + 1, end} {
			path := []string{strconv.Itoa(begin), strconv.Itoa(e)}
			got, err := proof.QueryShareInclusionProof(sdk.Context{}, path, &abci.RequestQuery{Data: data})
			require.NoError(t, err)
			require.Equal(t, legacyQueryShareInclusionProof(t, begin, e, data), got, "shares [%d, %d)", begin, e)
		}
	}
}
