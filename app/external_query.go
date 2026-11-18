package app

import (
	"context"

	"github.com/celestiaorg/celestia-app/v10/pkg/proof"
	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type abciQueryFn = func(context.Context, *abci.RequestQuery) (*abci.ResponseQuery, error)

// rejectProofQueries wraps a query function for callers outside the node and
// refuses the inclusion proof paths, which build a square from request bytes.
func rejectProofQueries(next abciQueryFn) abciQueryFn {
	return func(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
		if proof.IsProofQueryPath(req.Path) {
			return nil, status.Errorf(codes.PermissionDenied, "query path %q is not served over this endpoint; use the CometBFT RPC routes tx, tx_search or prove_shares_v2", req.Path)
		}
		return next(ctx, req)
	}
}
