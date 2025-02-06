package gasestimation

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/x/minfee"
	blobtx "github.com/celestiaorg/go-square/v2/tx"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gogogrpc "github.com/gogo/protobuf/grpc"
)

// EstimationZScore is the z-score corresponding to 10% and 90% of the gas prices distribution.
// More information can be found in: https://en.wikipedia.org/wiki/Standard_normal_table#Cumulative_(less_than_Z)
const EstimationZScore = 1.28

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
}

func NewGasEstimatorServer(clientCtx client.Context, simulateFn baseAppSimulateFn) GasEstimatorServer {
	return &gasEstimatorServer{
		clientCtx:  clientCtx,
		simulateFn: simulateFn,
	}
}

// lastFiveBlocksTransactionsQuery transaction search query to get all the transactions in the last five blocks.
// the latestHeight param represents the chain's tip height.
func lastFiveBlocksTransactionsQuery(latestHeight int64) string {
	startHeight := latestHeight - 5
	if startHeight < 0 {
		startHeight = 0
	}
	return fmt.Sprintf("tx.height>%d AND tx.height<=%d", startHeight, latestHeight)
}

// numberOfTransactionsPerPage the number of transactions to return per page in the transaction search
// endpoint.
// Note: the maximum number of transactions per page the endpoint allows is 100.
var numberOfTransactionsPerPage = 100

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

// estimateGasPrice takes a transaction priority and estimates the gas price based
// on the gas prices of the transactions in the last five blocks.
// If no transaction is found in the last five blocks, return the network
// min gas price.
// It's up to the light client to set the gas price in this case
// to the minimum gas price set by that node.
func (s *gasEstimatorServer) estimateGasPrice(ctx context.Context, priority TxPriority) (float64, error) {
	status, err := s.clientCtx.Client.Status(ctx)
	if err != nil {
		return 0, err
	}
	latestHeight := status.SyncInfo.LatestBlockHeight
	page := 1
	txSearchResult, err := s.clientCtx.Client.TxSearch(
		ctx,
		lastFiveBlocksTransactionsQuery(latestHeight),
		false,
		&page,
		&numberOfTransactionsPerPage,
		"asc",
	)
	if err != nil {
		return 0, err
	}

	totalNumberOfTransactions := txSearchResult.TotalCount
	if totalNumberOfTransactions == 0 {
		// return the min gas price if no transaction found in the last 5 blocks
		return minfee.DefaultNetworkMinGasPrice.MustFloat64(), nil
	}

	gasPrices := make([]float64, 0)
	for {
		currentPageGasPrices, err := extractGasPriceFromTransactions(txSearchResult.Txs)
		if err != nil {
			return 0, err
		}
		gasPrices = append(gasPrices, currentPageGasPrices...)
		if len(gasPrices) >= totalNumberOfTransactions {
			break
		}
		page++
		txSearchResult, err = s.clientCtx.Client.TxSearch(
			ctx,
			lastFiveBlocksTransactionsQuery(latestHeight),
			false,
			&page,
			&numberOfTransactionsPerPage,
			"asc",
		)
		if err != nil {
			return 0, err
		}
	}
	return estimateGasPriceForTransactions(gasPrices, priority)
}

// estimateGasPriceForTransactions takes a list of transactions and priority
// and returns a gas price estimation.
// The priority sets the estimation as follows:
// - High Priority: The gas price is the price at the start of the top 10% of transactions’ gas prices from the last five blocks.
// - Medium Priority: The gas price is the mean of all gas prices from the last five blocks.
// - Low Priority: The gas price is the value at the end of the lowest 10% of gas prices from the last five blocks.
// - Unspecified Priority (default): This is equivalent to the Medium priority, using the mean of all gas prices from the last five blocks.
// More information can be found in ADR-023.
func estimateGasPriceForTransactions(gasPrices []float64, priority TxPriority) (float64, error) {
	meanGasPrice := Mean(gasPrices)
	switch priority {
	case TxPriority_TX_PRIORITY_UNSPECIFIED:
		return meanGasPrice, nil
	case TxPriority_TX_PRIORITY_LOW:
		stDev := StandardDeviation(meanGasPrice, gasPrices)
		return meanGasPrice - EstimationZScore*stDev, nil
	case TxPriority_TX_PRIORITY_MEDIUM:
		return meanGasPrice, nil
	case TxPriority_TX_PRIORITY_HIGH:
		stDev := StandardDeviation(meanGasPrice, gasPrices)
		return meanGasPrice + EstimationZScore*stDev, nil
	default:
		return 0, fmt.Errorf("unknown priority: %d", priority)
	}
}

// extractGasPriceFromTransactions takes a list of transaction results
// and returns their corresponding gas prices.
func extractGasPriceFromTransactions(txs []*coretypes.ResultTx) ([]float64, error) {
	gasPrices := make([]float64, 0)
	for _, tx := range txs {
		var feeWithDenom string
		for _, event := range tx.TxResult.Events {
			if event.GetType() == "tx" {
				for _, attr := range event.Attributes {
					if string(attr.Key) == "fee" {
						feeWithDenom = string(attr.Value)
					}
				}
			}
		}
		if feeWithDenom == "" {
			return nil, fmt.Errorf("couldn't find fee for transaction %s", tx.Hash)
		}
		feeWithoutDenom, found := strings.CutSuffix(feeWithDenom, appconsts.BondDenom)
		if !found {
			return nil, fmt.Errorf("couldn't find fee denom for transaction %s: %s", tx.Hash, feeWithDenom)
		}
		fee, err := strconv.ParseFloat(feeWithoutDenom, 64)
		if err != nil {
			return nil, fmt.Errorf("couldn't parse fee for transaction %s: %w", tx.Hash, err)
		}
		if tx.TxResult.GasWanted == 0 {
			return nil, fmt.Errorf("zero gas wanted for transaction %s", tx.Hash)
		}
		gasPrices = append(gasPrices, fee/float64(tx.TxResult.GasWanted))
	}
	return gasPrices, nil
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
		variance += math.Pow(gasPrice-meanGasPrice, 2)
	}
	variance /= float64(len(gasPrices))
	return math.Sqrt(variance)
}
