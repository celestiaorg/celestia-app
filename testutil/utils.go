package testutil

import (
	"context"
	"encoding/hex"

	"github.com/cosmos/cosmos-sdk/client"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
)

func QueryWithoutProof(clientCtx client.Context, hashHexStr string) (*rpctypes.ResultTx, error) {
	hash, err := hex.DecodeString(hashHexStr)
	if err != nil {
		return nil, err
	}

	node, err := clientCtx.GetNode()
	if err != nil {
		return nil, err
	}

	return node.Tx(context.Background(), hash, false)
}
