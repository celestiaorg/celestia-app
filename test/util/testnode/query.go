package testnode

import (
	"context"
	"encoding/hex"

	"github.com/cosmos/cosmos-sdk/client"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
)

func QueryTx(clientCtx client.Context, hashHexStr string, prove bool) (*rpctypes.ResultTx, error) {
	hash, err := hex.DecodeString(hashHexStr)
	if err != nil {
		return nil, err
	}

	node, err := clientCtx.GetNode()
	if err != nil {
		return nil, err
	}

	return node.Tx(context.Background(), hash, prove)
}
