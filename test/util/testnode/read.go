package testnode

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/cometbft/cometbft/rpc/client/http"
	"github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func ReadRecentBlocks(ctx context.Context, rpcAddress string, blocks int64) ([]*types.Block, error) {
	client, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		return nil, err
	}
	status, err := client.Status(ctx)
	if err != nil {
		return nil, err
	}
	if status.SyncInfo.LatestBlockHeight < blocks {
		return nil, fmt.Errorf("latest block height %d is less than requested blocks %d", status.SyncInfo.LatestBlockHeight, blocks)
	}
	return ReadBlockHeights(ctx, rpcAddress, status.SyncInfo.LatestBlockHeight-blocks+1, status.SyncInfo.LatestBlockHeight)
}

func ReadBlockchain(ctx context.Context, rpcAddress string) ([]*types.Block, error) {
	client, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		return nil, err
	}
	status, err := client.Status(ctx)
	if err != nil {
		return nil, err
	}
	return ReadBlockHeights(ctx, rpcAddress, 1, status.SyncInfo.LatestBlockHeight)
}

// ReadBlockchainHeaders retrieves the blockchain headers from height 1 up to
// latest available height from the node at rpcAddress and returns it.
// The headers are returned in ascending order (lowest first).
func ReadBlockchainHeaders(ctx context.Context, rpcAddress string) ([]*types.BlockMeta, error) {
	client, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		return nil, err
	}

	// fetch the latest height
	resp, err := client.Status(ctx)
	if err != nil {
		return nil, err
	}
	maxHeight := resp.SyncInfo.LatestBlockHeight

	blockHeaders := make([]*types.BlockMeta, 0)
	// fetch headers up to maxHeight
	lastFetchedHeight := int64(0)
	for {
		// BlockchainInfo may apply a limit on the range of blocks to fetch,
		// so we need to request them iteratively.
		// note that block headers returned by BlockchainInfo are in descending
		// order (highest first).

		res, err := client.BlockchainInfo(ctx, 1, maxHeight)
		if err != nil {
			return nil, err
		}

		blockHeaders = append(blockHeaders, res.BlockMetas...)

		lastFetchedHeight = res.BlockMetas[len(res.BlockMetas)-1].Header.Height

		// fetch until the first block
		if lastFetchedHeight <= 1 {
			break
		}

		// set the new maxHeight to fetch the next batch of headers
		maxHeight = lastFetchedHeight - 1

	}

	// reverse the order of headers to be ascending (lowest first).
	reverseSlice(blockHeaders)

	return blockHeaders, nil
}

// reverseSlice reverses the order of elements in a slice in place.
func reverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func ReadBlockHeights(ctx context.Context, rpcAddress string, fromHeight, toHeight int64) ([]*types.Block, error) {
	client, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		return nil, err
	}
	blocks := make([]*types.Block, toHeight-fromHeight+1)
	for i := fromHeight; i <= toHeight; i++ {
		resp, err := client.Block(ctx, &i)
		if err != nil {
			return nil, err
		}
		blocks[i-fromHeight] = resp.Block
	}
	return blocks, nil
}

func DecodeBlockData(data types.Data) ([]sdk.Tx, error) {
	encCfg := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	decoder := encCfg.TxConfig.TxDecoder()
	txs := make([]sdk.Tx, 0)
	for _, txBytes := range data.Txs {
		blobTx, isBlobTx, err := tx.UnmarshalBlobTx(txBytes)
		if isBlobTx {
			if err != nil {
				return nil, fmt.Errorf("decoding blob tx: %w", err)
			}
			txBytes = blobTx.Tx
		}
		tx, err := decoder(txBytes)
		if err != nil {
			return nil, fmt.Errorf("decoding tx: %s: %w", string(txBytes), err)
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func CalculateMeanGasFromRecentBlocks(ctx context.Context, rpcAddress, msgType string, blocks int64) (float64, int64, error) {
	client, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		return 0.0, 0, err
	}
	status, err := client.Status(ctx)
	if err != nil {
		return 0.0, 0, err
	}
	if status.SyncInfo.LatestBlockHeight <= blocks {
		return 0.0, 0, fmt.Errorf("latest block height %d is less than %d", status.SyncInfo.LatestBlockHeight, blocks)
	}
	return CalculateMeanGas(ctx, rpcAddress, msgType, status.SyncInfo.LatestBlockHeight-blocks+1, status.SyncInfo.LatestBlockHeight)
}

func CalculateMeanGas(ctx context.Context, rpcAddress, msgType string, fromHeight int64, toHeight int64) (float64, int64, error) {
	var (
		encCfg   = encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
		decoder  = encCfg.TxConfig.TxDecoder()
		totalGas int64
		count    int64
		average  = func() float64 {
			if count == 0 {
				return 0
			}
			return float64(totalGas) / float64(count)
		}
	)
	client, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		return 0.0, 0, err
	}

	for height := fromHeight; height <= toHeight; height++ {
		resp, err := client.Block(ctx, &height)
		if err != nil {
			return average(), count, err
		}
		indices := make([]int, 0, len(resp.Block.Data.Txs))
		for i, rawTx := range resp.Block.Data.Txs {
			tx, err := decoder(rawTx)
			if err != nil {
				return average(), count, fmt.Errorf("decoding tx (height: %d): %w", height, err)
			}
			msgs := tx.GetMsgs()
			// multi message transactions are not included
			if len(msgs) == 1 && sdk.MsgTypeURL(msgs[0]) == msgType {
				indices = append(indices, i)
			}
		}
		if len(indices) > 0 {
			results, err := client.BlockResults(ctx, &height)
			if err != nil {
				return average(), count, fmt.Errorf("getting block results (height %d): %w", height, err)
			}
			for _, i := range indices {
				totalGas += results.TxsResults[i].GasUsed
				count++
			}
		}
	}
	return average(), count, nil
}
