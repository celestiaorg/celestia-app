package gasestimation

import (
	"context"
	"errors"
	"fmt"
	"github.com/tendermint/tendermint/types"
	"sort"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	blobtx "github.com/celestiaorg/go-square/v2/tx"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogogrpc "github.com/gogo/protobuf/grpc"
)

// baseAppSimulateFn is the signature of the Baseapp#Simulate function.
type baseAppSimulateFn func(txBytes []byte) (sdk.GasInfo, *sdk.Result, error)

// RegisterGasEstimationService registers the gas estimation service on the gRPC router.
func RegisterGasEstimationService(qrt gogogrpc.Server, clientCtx client.Context, simulateFn baseAppSimulateFn) {
	RegisterGasEstimatorServer(
		qrt,
		NewGasEstimatorServer(clientCtx, simulateFn),
	)
}

var _ GasEstimatorServer = &gasEstimatorServer{}

type gasEstimatorServer struct {
	clientCtx  client.Context
	simulateFn baseAppSimulateFn
	txDecoder  sdk.TxDecoder
}

func NewGasEstimatorServer(clientCtx client.Context, simulateFn baseAppSimulateFn) GasEstimatorServer {
	return &gasEstimatorServer{
		clientCtx:  clientCtx,
		simulateFn: simulateFn,
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
	return &EstimateGasPriceAndUsageResponse{
		EstimatedGasPrice: gasPrice,
		EstimatedGasUsed:  gasUsedInfo.GasUsed,
	}, nil
}

// gasPriceEstimationThreshold the threshold of mempool transactions to
// estimate the gas price.
var gasPriceEstimationThreshold = 0.70

// estimateGasPrice takes a transaction priority and estimates the gas price based
// on the gas prices of the transactions in the mempool.
// If the mempool transactions can't fill more than 70% of the block, the min gas
// price is returned.
func (s *gasEstimatorServer) estimateGasPrice(ctx context.Context, priority TxPriority) (float64, error) {
	// using -1 to return all the transactions.
	limit := -1
	txsResp, err := s.clientCtx.Client.UnconfirmedTxs(ctx, &limit)
	if err != nil {
		return 0, err
	}
	if txsResp.TotalBytes < int64(appconsts.DefaultMaxBytes*gasPriceEstimationThreshold) {
		return appconsts.DefaultMinGasPrice, nil
	}
	gasPrices, err := sortAndExtractGasPrices(s.txDecoder, txsResp.Txs, int64(appconsts.DefaultUpperBoundMaxBytes))
	if err != nil {
		return 0, err
	}
	return estimateGasPriceForTransactions(gasPrices, priority)
}

// estimateGasPriceForTransactions takes a list of transactions and priority
// and returns a gas price estimation.
// The priority sets the estimation as follows:
// TODO update
// - High Priority: The gas price is the price at the start of the top 10% of transactions’ gas prices from the last five blocks.
// - Medium Priority: The gas price is the mean of all gas prices from the last five blocks.
// - Low Priority: The gas price is the value at the end of the lowest 10% of gas prices from the last five blocks.
// - Unspecified Priority (default): This is equivalent to the Medium priority, using the mean of all gas prices from the last five blocks.
// More information can be found in ADR-023.
func estimateGasPriceForTransactions(gasPrices []float64, priority TxPriority) (float64, error) {
	switch priority {
	case TxPriority_TX_PRIORITY_UNSPECIFIED:
		return Median(gasPrices)
	case TxPriority_TX_PRIORITY_LOW:
		return Median(gasPrices[:len(gasPrices)*10/100])
	case TxPriority_TX_PRIORITY_MEDIUM:
		return Median(gasPrices)
	case TxPriority_TX_PRIORITY_HIGH:
		return Median(gasPrices[len(gasPrices)*90/100:])
	default:
		return 0, fmt.Errorf("unknown priority: %d", priority)
	}
}

// sortAndExtractGasPrices takes a list of transaction results
// and returns their corresponding gas prices.
// The total size of the returned transactions won't exceed the max byte parameter.
func sortAndExtractGasPrices(txDecoder sdk.TxDecoder, txs []types.Tx, maxBytes int64) ([]float64, error) {
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
			break
		}
		gasPrices = append(gasPrices, tx.gasPrice)
	}
	return gasPrices, nil
}

// Median calculates the median value of the provided gas prices.
func Median(gasPrices []float64) (float64, error) {
	n := len(gasPrices)
	if n == 0 {
		return 0, errors.New("cannot compute median of an empty slice")
	}
	sort.Float64s(gasPrices)

	if n%2 == 1 {
		return gasPrices[n/2], nil
	}
	mid1 := gasPrices[n/2-1]
	mid2 := gasPrices[n/2]
	return (mid1 + mid2) / 2.0, nil
}
