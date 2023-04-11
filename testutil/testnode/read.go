package testnode

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/rpc/client/http"
	"github.com/tendermint/tendermint/types"
)

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

func DecodeBlockData(data types.Data) ([]sdk.Msg, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decoder := encoding.IndexWrapperDecoder(encCfg.TxConfig.TxDecoder())
	msgs := make([]sdk.Msg, 0)
	for _, txBytes := range data.Txs {
		tx, err := decoder(txBytes)
		if err != nil {
			return nil, fmt.Errorf("decoding tx: %s: %w", string(txBytes), err)
		}
		msgs = append(msgs, tx.GetMsgs()...)
	}
	return msgs, nil
}
