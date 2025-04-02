package gasestimation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"

	tmclient "github.com/tendermint/tendermint/rpc/client"
	"github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	blobtx "github.com/celestiaorg/go-square/v2/tx"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogogrpc "github.com/gogo/protobuf/grpc"
)

// gasMultiplier is the multiplier for the gas limit. It's used to account for the fact that
// when the gas is simulated it will occasionally underestimate the real gas used by the transaction.
const gasMultiplier = 1.1

// baseAppSimulateFn is the signature of the Baseapp#Simulate function.
type baseAppSimulateFn func(txBytes []byte) (sdk.GasInfo, *sdk.Result, error)

// govMaxSquareBytesFn is the signature of a function that returns the
// current max square size in bytes.
type govMaxSquareBytesFn func() (uint64, error)

// RegisterGasEstimationService registers the gas estimation service on the gRPC router.
func RegisterGasEstimationService(qrt gogogrpc.Server, clientCtx client.Context, txDecoder sdk.TxDecoder, govMaxSquareBytesFn govMaxSquareBytesFn, simulateFn baseAppSimulateFn) {
	RegisterGasEstimatorServer(
		qrt,
		NewGasEstimatorServer(clientCtx.Client, txDecoder, govMaxSquareBytesFn, simulateFn),
	)
}

var _ GasEstimatorServer = &gasEstimatorServer{}

type gasEstimatorServer struct {
	mempoolClient       tmclient.MempoolClient
	simulateFn          baseAppSimulateFn
	txDecoder           sdk.TxDecoder
	govMaxSquareBytesFn govMaxSquareBytesFn
}

func NewGasEstimatorServer(mempoolClient tmclient.MempoolClient, txDecoder sdk.TxDecoder, govMaxSquareBytesFn govMaxSquareBytesFn, simulateFn baseAppSimulateFn) GasEstimatorServer {
	return &gasEstimatorServer{
		mempoolClient:       mempoolClient,
		simulateFn:          simulateFn,
		txDecoder:           txDecoder,
		govMaxSquareBytesFn: govMaxSquareBytesFn,
	}
}

func (s *gasEstimatorServer) EstimateGasPrice(ctx context.Context, request *EstimateGasPriceRequest) (*EstimateGasPriceResponse, error) {
	gasPrice, err := s.estimateGasPrice(ctx, request.TxPriority)
	if err != nil {
		return nil, err
	}
	return &EstimateGasPriceResponse{EstimatedGasPrice: gasPrice}, nil
}

// EstimateGasPriceAndUsage takes a transaction priority and a transaction bytes
// and estimates the gas price based on the gas prices of the transactions in the last five blocks.
// If no transaction is found in the last five blocks, return the network
// min gas price.
// It's up to the light client to set the gas price in this case
// to the minimum gas price set by that node.
// The gas used is estimated using the state machine simulation.
func (s *gasEstimatorServer) EstimateGasPriceAndUsage(ctx context.Context, request *EstimateGasPriceAndUsageRequest) (*EstimateGasPriceAndUsageResponse, error) {
	// estimate the gas price
	gasPrice, err := s.estimateGasPrice(ctx, request.TxPriority)
	if err != nil {
		return nil, err
	}

	// estimate the gas used
	btx, isBlob, err := blobtx.UnmarshalBlobTx(request.TxBytes)
	if isBlob && err != nil {
		return nil, err
	}

	var txBytes []byte
	if isBlob {
		txBytes = btx.Tx
	} else {
		txBytes = request.TxBytes
	}

	gasUsedInfo, _, err := s.simulateFn(txBytes)
	if err != nil {
		return nil, err
	}
	estimatedGasUsed := uint64(math.Round(float64(gasUsedInfo.GasUsed) * gasMultiplier))

	return &EstimateGasPriceAndUsageResponse{
		EstimatedGasPrice: gasPrice,
		EstimatedGasUsed:  estimatedGasUsed,
	}, nil
}

// gasPriceEstimationThreshold the threshold of mempool transactions to
// estimate the gas price.
// If the returned transactions from the mempool can't fill more than 70% of
// the max block, the node min gas price is returned.
// Otherwise, the gas is estimated following the provided priority.
var gasPriceEstimationThreshold = 0.70

// estimateGasPrice takes a transaction priority and estimates the gas price based
// on the gas prices of the transactions in the mempool.
// If the mempool transactions can't fill more than 70% of the block, the min gas
// price is returned.
func (s *gasEstimatorServer) estimateGasPrice(ctx context.Context, priority TxPriority) (float64, error) {
	// Use -1 to query all the unconfirmed transactions.
	limit := -1
	txsResp, err := s.mempoolClient.UnconfirmedTxs(ctx, &limit)
	if err != nil {
		return 0, err
	}
	govMaxSquareBytes, err := s.govMaxSquareBytesFn()
	if err != nil {
		return 0, err
	}
	if float64(txsResp.TotalBytes) < float64(govMaxSquareBytes)*gasPriceEstimationThreshold {
		return appconsts.DefaultMinGasPrice, nil
	}
	gasPrices, err := SortAndExtractGasPrices(s.txDecoder, txsResp.Txs, int64(appconsts.DefaultUpperBoundMaxBytes))
	if err != nil {
		return 0, err
	}
	return estimateGasPriceForTransactions(gasPrices, priority)
}

const (
	// highPriorityGasAdjustmentRate is the percentage increase applied to the
	// estimated gas price when the block is more than 70% full, i.e., gasPriceEstimationThreshold,
	// but the gas prices are tightly clustered. This ensures that high-priority
	// transactions still have a competitive fee to improve inclusion probability.
	highPriorityGasAdjustmentRate = 1.3
	// mediumPriorityGasAdjustmentRate similar to highPriorityGasAdjustmentRate but for
	// the medium priority.
	mediumPriorityGasAdjustmentRate = 1.1
	// gasPriceAdjustmentThreshold the standard deviation threshold under which we
	// apply the gas price adjustment.
	gasPriceAdjustmentThreshold = 0.001
)

// estimateGasPriceForTransactions takes a list of transactions and priority
// and returns a gas price estimation.
// The priority sets the estimation as follows:
// - High Priority: The gas price is the median price of the top 10% of transactions’ gas prices in the mempool.
// - Medium Priority: The gas price is the median price of the all gas prices in the mempool.
// - Low Priority: The gas price is the median price of the bottom 10% of gas prices in the mempool.
// - Unspecified Priority (default): This is equivalent to the Medium priority, using the median price of all gas prices in the mempool.
// If the list of gas prices has a standard deviation < gasPriceAdjustmentThreshold, meaning the gas price values are tightly clustered,
// an increase of 30% and 10% will be added for high and medium priority respectively.
// More information can be found in ADR-023.
func estimateGasPriceForTransactions(gasPrices []float64, priority TxPriority) (float64, error) {
	if len(gasPrices) == 0 {
		return 0, errors.New("empty gas prices list")
	}
	stDev := StandardDeviation(Mean(gasPrices), gasPrices)
	switch priority {
	case TxPriority_TX_PRIORITY_MEDIUM, TxPriority_TX_PRIORITY_UNSPECIFIED:
		estimation, err := Median(gasPrices)
		if err != nil {
			return 0, err
		}
		if stDev < gasPriceAdjustmentThreshold {
			return estimation * mediumPriorityGasAdjustmentRate, nil
		}
		return estimation, nil
	case TxPriority_TX_PRIORITY_LOW:
		bottom10PercentIndex := len(gasPrices) * 10 / 100
		if bottom10PercentIndex == 0 {
			// the case of a slice containing less than 10 elements
			bottom10PercentIndex = 1
		}
		return Median(gasPrices[:bottom10PercentIndex])
	case TxPriority_TX_PRIORITY_HIGH:
		estimation, err := Median(gasPrices[len(gasPrices)*90/100:])
		if err != nil {
			return 0, err
		}
		if stDev < gasPriceAdjustmentThreshold {
			return estimation * highPriorityGasAdjustmentRate, nil
		}
		return estimation, nil
	default:
		return 0, fmt.Errorf("unknown priority: %d", priority)
	}
}

// SortAndExtractGasPrices takes a list of transaction results
// and returns their corresponding gas prices.
// The total size of the returned transactions won't exceed the maxBytes parameter.
func SortAndExtractGasPrices(txDecoder sdk.TxDecoder, txs []types.Tx, maxBytes int64) ([]float64, error) {
	type gasPriceAndSize struct {
		gasPrice float64
		size     int64
	}
	gasPriceAndSizes := make([]gasPriceAndSize, len(txs))
	for index, rawTx := range txs {
		txBytes := rawTx
		bTx, isBlob, err := blobtx.UnmarshalBlobTx(rawTx)
		if isBlob {
			if err != nil {
				panic(err)
			}
			txBytes = bTx.Tx
		}
		sdkTx, err := txDecoder(txBytes)
		if err != nil {
			return nil, err
		}
		feeTx := sdkTx.(sdk.FeeTx)
		gasPrice := float64(feeTx.GetFee().AmountOf(appconsts.BondDenom).Uint64()) / float64(feeTx.GetGas())
		gasPriceAndSizes[index] = gasPriceAndSize{
			size:     int64(len(rawTx)),
			gasPrice: gasPrice,
		}
	}

	// sort the gas prices in descending order
	sort.Slice(gasPriceAndSizes, func(i, j int) bool {
		return gasPriceAndSizes[i].gasPrice > gasPriceAndSizes[j].gasPrice
	})

	gasPrices := make([]float64, 0)
	totalSize := int64(0)
	for _, tx := range gasPriceAndSizes {
		if tx.size+totalSize > maxBytes {
			// to also add small transactions in case they can be included in the block.
			continue
		}
		gasPrices = append(gasPrices, tx.gasPrice)
		totalSize += tx.size
	}
	sort.Float64s(gasPrices)
	return gasPrices, nil
}

// Median calculates the median value of the provided gas prices.
// Expects a sorted slice.
func Median(gasPrices []float64) (float64, error) {
	n := len(gasPrices)
	if n == 0 {
		return 0, errors.New("cannot compute median of an empty slice")
	}

	if n%2 == 1 {
		return gasPrices[n/2], nil
	}
	mid1 := gasPrices[n/2-1]
	mid2 := gasPrices[n/2]
	return (mid1 + mid2) / 2.0, nil
}

// Mean calculates the mean value of the provided gas prices.
func Mean(gasPrices []float64) float64 {
	if len(gasPrices) == 0 {
		return 0
	}
	sum := 0.0
	for _, gasPrice := range gasPrices {
		sum += gasPrice
	}
	return sum / float64(len(gasPrices))
}

// StandardDeviation calculates the standard deviation of the provided gas prices.
func StandardDeviation(meanGasPrice float64, gasPrices []float64) float64 {
	if len(gasPrices) < 2 {
		return 0
	}
	var variance float64
	for _, gasPrice := range gasPrices {
		diff := gasPrice - meanGasPrice
		variance += diff * diff
	}
	variance /= float64(len(gasPrices))
	return math.Sqrt(variance)
}
