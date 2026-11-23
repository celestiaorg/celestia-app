package keeper

import (
	"context"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var _ types.QueryServer = Keeper{}

// Params queries the parameters of the fibre module.
func (k Keeper) Params(c context.Context, req *types.QueryParamsRequest) (*types.QueryParamsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	ctx := sdk.UnwrapSDKContext(c)
	params := k.GetParams(ctx)

	return &types.QueryParamsResponse{Params: params}, nil
}

// EscrowAccount queries an escrow account by signer address.
func (k Keeper) EscrowAccount(c context.Context, req *types.QueryEscrowAccountRequest) (*types.QueryEscrowAccountResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Signer == "" {
		return nil, status.Error(codes.InvalidArgument, "signer address cannot be empty")
	}

	ctx := sdk.UnwrapSDKContext(c)
	account, found := k.GetEscrowAccount(ctx, req.Signer)

	return &types.QueryEscrowAccountResponse{
		EscrowAccount: &account,
		Found:         found,
	}, nil
}

// Withdrawals queries all withdrawals for an escrow account by signer address.
func (k Keeper) Withdrawals(c context.Context, req *types.QueryWithdrawalsRequest) (*types.QueryWithdrawalsResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if req.Signer == "" {
		return nil, status.Error(codes.InvalidArgument, "signer address cannot be empty")
	}

	ctx := sdk.UnwrapSDKContext(c)
	withdrawals := k.GetWithdrawalsBySigner(ctx, req.Signer)

	return &types.QueryWithdrawalsResponse{Withdrawals: withdrawals}, nil
}

// IsPaymentProcessed queries whether a payment promise has been processed.
func (k Keeper) IsPaymentProcessed(c context.Context, req *types.QueryIsPaymentProcessedRequest) (*types.QueryIsPaymentProcessedResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}

	if len(req.PromiseHash) == 0 {
		return nil, status.Error(codes.InvalidArgument, "payment promise hash cannot be empty")
	}
	if req.CreationTimestamp == nil {
		return nil, status.Error(codes.InvalidArgument, "payment promise creation timestamp is required")
	}

	ctx := sdk.UnwrapSDKContext(c)
	processedPayment, found := k.GetProcessedPayment(ctx, *req.CreationTimestamp, req.PromiseHash)
	var processedAt *time.Time
	if found {
		processedAt = &processedPayment.ProcessedAt
	}

	return &types.QueryIsPaymentProcessedResponse{
		ProcessedAt: processedAt,
		Found:       found,
	}, nil
}

// ValidatePaymentPromise validates a payment promise for server use.
// This is called by validators before signing a payment promise to verify
// that the escrow account has sufficient balance and hasn't been processed.
func (k Keeper) ValidatePaymentPromise(c context.Context, req *types.QueryValidatePaymentPromiseRequest) (*types.QueryValidatePaymentPromiseResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid request")
	}
	// The promise cache must only see the latest committed state. ABCI queries
	// with a height are rejected by App.Query.
	if md, ok := metadata.FromIncomingContext(c); ok && len(md.Get(grpctypes.GRPCBlockHeightHeader)) > 0 {
		return nil, status.Errorf(codes.InvalidArgument, "%s header is not supported for this query", grpctypes.GRPCBlockHeightHeader)
	}

	// The gRPC endpoint is adversarial: check the format of the promise before
	// the stateful checks, which assume a well-formed promise. For example,
	// they derive the signer address and that panics on a key of the wrong
	// length.
	if err := req.Promise.ValidateBasic(); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "invalid payment promise: %v", err)
	}

	ctx := sdk.UnwrapSDKContext(c)

	// Note: The signature is verified below, and only when the promise is
	// reserved against the validator-local budget.
	expirationTime, err := k.ValidatePaymentPromiseStateful(ctx, &req.Promise)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	// Reserve the promise against the validator-local budget to close the
	// double-spend window between this validation and on-chain settlement.
	if k.promiseCache != nil {
		// The gRPC endpoint is adversarial: verify the signature before mutating the
		// cache so a forged promise cannot poison a signer's budget.
		pp := fibre.PaymentPromise{}
		if err := pp.FromProto(&req.Promise); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid payment promise format: %v", err)
		}
		if err := k.ValidatePromiseStateless(&pp); err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		hash, err := pp.Hash()
		if err != nil {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		signer := sdk.AccAddress(req.Promise.SignerPublicKey.Address()).String()
		if err := k.promiseCache.Reserve(ctx, signer, hash, req.Promise.BlobSize, req.Promise.CreationTimestamp); err != nil {
			return nil, status.Error(codes.ResourceExhausted, err.Error())
		}
	}

	return &types.QueryValidatePaymentPromiseResponse{
		IsValid:        true,
		ExpirationTime: &expirationTime,
		ShardRetention: k.GetParams(ctx).ShardRetention,
	}, nil
}
