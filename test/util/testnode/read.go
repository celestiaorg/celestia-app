package testnode

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/go-square/blob"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/types"
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

// ReadBlockchainInfo retrieves the blockchain information from height 0 up to the latest height from the node at
// rpcAddress and returns it.
func ReadBlockchainInfo(ctx context.Context, rpcAddress string) ([]*types.BlockMeta, error) {
	client, err := http.New(rpcAddress, "/websocket")
	if err != nil {
		return nil, err
	}

	// fetch the latest height
	resp, err := client.Status(ctx)
	if err != nil {
		return nil, err
	}

	// fetch the blocks meta data
	blocksMeta := make([]*types.BlockMeta, 0)
	maxHeight := resp.SyncInfo.LatestBlockHeight
	lastFetchedHeight := int64(0)
	println("max height: ", maxHeight)
	for {
		// BlockchainInfo may not return the requested number of blocks (a limit of 20 may be applied),
		// so we need to request them iteratively
		println("fetching blocks from ", lastFetchedHeight+1, " to ", maxHeight)
		res, err := client.BlockchainInfo(ctx, lastFetchedHeight+1, maxHeight)
		if err != nil {
			return nil, err
		}

		blocksMeta = append(blocksMeta, res.BlockMetas...)
		println("fetched ", len(res.BlockMetas), " blocks")

		lastFetchedHeight = res.BlockMetas[len(res.BlockMetas)-1].Header.Height
		println("last seen height: ", lastFetchedHeight)

		if lastFetchedHeight >= maxHeight {
			break
		}

	}

	println("Read ", len(blocksMeta), " blocks")

	return blocksMeta, nil
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
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoder := encCfg.TxConfig.TxDecoder()
	txs := make([]sdk.Tx, 0)
	for _, txBytes := range data.Txs {
		blobTx, isBlobTx := blob.UnmarshalBlobTx(txBytes)
		if isBlobTx {
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
		encCfg   = encoding.MakeConfig(app.ModuleEncodingRegisters...)
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
