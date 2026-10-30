package gasestimation

import (
	"context"
	"encoding/binary"
	"math"
	"sort"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// mempoolTxSpecSize is the number of fuzz bytes that describe one mempool tx:
// a 16-bit size, a 32-bit fee and a 32-bit gas limit.
const mempoolTxSpecSize = 10

// maxFuzzMempoolTxs bounds the mempool built from one fuzz input.
const maxFuzzMempoolTxs = 512

// FuzzEstimateGasPrice runs the gas price estimator on an attacker-chosen
// request priority over a mempool of txs with attacker-chosen sizes, fees and
// gas limits. Anyone can submit such txs and query the service. Mempool txs
// passed CheckTx, so each has a positive gas limit and a fee below MaxInt64.
// The estimate must be finite, at least the network minimum, and no higher
// than the highest fitting gas price plus the 30% high priority adjustment.
func FuzzEstimateGasPrice(f *testing.F) {
	spec := func(size uint16, fee, gas uint32) []byte {
		b := make([]byte, mempoolTxSpecSize)
		binary.BigEndian.PutUint16(b, size)
		binary.BigEndian.PutUint32(b[2:], fee)
		binary.BigEndian.PutUint32(b[6:], gas)
		return b
	}
	f.Add([]byte{}, int32(TxPriority_TX_PRIORITY_MEDIUM), uint32(1000), uint32(2000))
	f.Add(spec(100, 2000, 1000), int32(TxPriority_TX_PRIORITY_HIGH), uint32(100), uint32(0))
	many := make([]byte, 0, 20*mempoolTxSpecSize)
	for i := range uint32(20) {
		many = append(many, spec(100, 100+i, 1)...)
	}
	for priority := range int32(5) {
		f.Add(many, priority, uint32(1000), uint32(0))
		f.Add(many, priority, uint32(1), uint32(1))
	}
	f.Add(append(spec(4, 1, 1), spec(60000, ^uint32(0), 1)...), int32(-1), uint32(10), uint32(0))

	f.Fuzz(func(t *testing.T, specs []byte, priority int32, govMaxSquareBytes, minGasPriceMicro uint32) {
		decoder := newMockTxDecoder()
		txs := make([]types.Tx, 0, min(len(specs)/mempoolTxSpecSize, maxFuzzMempoolTxs))
		maxPrice := 0.0
		for i := 0; i+mempoolTxSpecSize <= len(specs) && len(txs) < maxFuzzMempoolTxs; i += mempoolTxSpecSize {
			// mockTxDecoder writes a 4-byte id into each tx.
			size := max(int(binary.BigEndian.Uint16(specs[i:])), 4)
			fee := binary.BigEndian.Uint32(specs[i+2:])
			gas := max(binary.BigEndian.Uint32(specs[i+6:]), 1)
			txs = append(txs, decoder.add(size, int64(fee), int64(gas)))
			if size <= int(govMaxSquareBytes) {
				maxPrice = math.Max(maxPrice, float64(fee)/float64(gas))
			}
		}
		minGasPrice := float64(minGasPriceMicro) / 1e6
		server := &gasEstimatorServer{
			mempoolClient:       newMockMempoolClient(txs),
			txDecoder:           decoder.decode,
			minGasPriceFn:       func() (float64, error) { return minGasPrice, nil },
			govMaxSquareBytesFn: func() (uint64, error) { return uint64(govMaxSquareBytes), nil },
		}

		got, err := server.estimateGasPrice(context.Background(), TxPriority(priority))

		gasPrices, sortErr := SortAndExtractGasPrices(decoder.decode, txs, int64(govMaxSquareBytes))
		require.NoError(t, sortErr)
		require.True(t, sort.Float64sAreSorted(gasPrices))
		if err != nil {
			_, known := TxPriority_name[priority]
			require.True(t, !known || len(gasPrices) == 0, "unexpected error: %v", err)
			return
		}
		require.False(t, math.IsNaN(got) || math.IsInf(got, 0), "estimate %v is not finite", got)
		require.GreaterOrEqual(t, got, minGasPrice)
		upper := math.Max(math.Max(maxPrice*highPriorityGasAdjustmentRate, minGasPrice), appconsts.DefaultMinGasPrice)
		require.LessOrEqual(t, got, upper*(1+1e-9))
	})
}
