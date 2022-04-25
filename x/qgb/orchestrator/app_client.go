package orchestrator

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/tendermint/tendermint/libs/bytes"
)

type AppClient interface {
	SubscribeValset(ctx context.Context) (<-chan types.Valset, error)
	SubscribeDataCommitment(ctx context.Context) (<-chan ExtendedDataCommitment, error)
}

// TODO replace with data commitment request when we have it
type ExtendedDataCommitment struct {
	Commitment bytes.HexBytes
	Start, End int64
	Nonce      uint64
}
