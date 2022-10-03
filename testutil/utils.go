package testutil

import (
	"bytes"
	"context"
	"encoding/hex"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/cosmos/cosmos-sdk/client"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
)

func RandomValidNamespace() namespace.ID {
	for {
		ns := tmrand.Bytes(8)
		isReservedNS := bytes.Compare(ns, appconsts.MaxReservedNamespace) <= 0
		isParityNS := bytes.Equal(ns, appconsts.ParitySharesNamespaceID)
		isTailPaddingNS := bytes.Equal(ns, appconsts.TailPaddingNamespaceID)
		if isReservedNS || isParityNS || isTailPaddingNS {
			continue
		}
		return ns
	}
}

func QueryWithOutProof(clientCtx client.Context, hashHexStr string) (*rpctypes.ResultTx, error) {
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
