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

// ExtendedDataCommitment adds the `types.DataCommitment` to also contain the commitment
// retrieved from celestia-core.
type ExtendedDataCommitment struct {
	Data       types.DataCommitment
	Commitment bytes.HexBytes
}
