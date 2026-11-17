package app

import (
	"context"

	"github.com/celestiaorg/celestia-app/v11/pkg/proof"
	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type abciQueryFn = func(context.Context, *abci.RequestQuery) (*abci.ResponseQuery, error)

// rejectProofQueries wraps an ABCI query function that serves callers outside
// the node. It refuses the inclusion proof query paths, which build a data
// square from caller-supplied bytes. celestia-core calls those paths with
// stored blocks over its own ABCI connection, which this wrapper does not
// touch.
func rejectProofQueries(next abciQueryFn) abciQueryFn {
	return func(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
		if req != nil && proof.IsProofQueryPath(req.Path) {
			return nil, status.Errorf(codes.PermissionDenied, "query path %q is not served over this endpoint; use the CometBFT RPC routes tx, tx_search or prove_shares_v2", req.Path)
		}
		return next(ctx, req)
	}
}
