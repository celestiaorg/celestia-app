package app

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v11/pkg/proof"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type abciQueryFn = func(context.Context, *abci.RequestQuery) (*abci.ResponseQuery, error)

// validatePaymentPromisePath is the ABCI query path of fibre's ValidatePaymentPromise.
var validatePaymentPromisePath = "/" + fibretypes.Query_serviceDesc.ServiceName + "/ValidatePaymentPromise"

// Query refuses ValidatePaymentPromise at a set height, because it reserves
// against a validator-local cache that must only see the latest committed state.
func (app *App) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	if req.Path == validatePaymentPromisePath && req.Height != 0 {
		return sdkerrors.QueryResult(errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "query height is not supported for ValidatePaymentPromise"), false), nil
	}
	return app.BaseApp.Query(ctx, req)
}

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
