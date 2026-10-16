package app

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/module"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/gogoproto/grpc"
	googlegrpc "google.golang.org/grpc"
)

const proposalCancelRatioV11 = "0.050000000000000000"

// RegisterServices retains the SDK services and migrations, wrapping governance
// parameter updates so proposals cannot change the cancellation ratio after v11.
func (am govModule) RegisterServices(cfg module.Configurator) {
	am.AppModule.RegisterServices(govParamsConfigurator{cfg})
}

type govParamsConfigurator struct {
	module.Configurator
}

func (cfg govParamsConfigurator) MsgServer() grpc.Server {
	return govParamsRegistrar{cfg.Configurator.MsgServer()}
}

type govParamsRegistrar struct {
	grpc.Server
}

func (r govParamsRegistrar) RegisterService(desc *googlegrpc.ServiceDesc, server any) {
	if msgServer, ok := server.(govv1.MsgServer); ok {
		server = govParamsMsgServer{msgServer}
	}
	r.Server.RegisterService(desc, server)
}

type govParamsMsgServer struct {
	govv1.MsgServer
}

func cancellationRatioLocked(ctx context.Context) bool {
	version := sdk.UnwrapSDKContext(ctx).ConsensusParams().Version
	return version != nil && version.App >= 11
}

func validateCancellationRatio(ratio string) error {
	value, err := math.LegacyNewDecFromStr(ratio)
	if err != nil {
		return sdkerrors.ErrInvalidRequest.Wrap("invalid proposal cancellation ratio")
	}
	if !value.Equal(math.LegacyNewDecWithPrec(5, 2)) {
		return sdkerrors.ErrUnauthorized.Wrap("proposal cancellation ratio is fixed at 5% from v11")
	}
	return nil
}

func (s govParamsMsgServer) UpdateParams(ctx context.Context, msg *govv1.MsgUpdateParams) (*govv1.MsgUpdateParamsResponse, error) {
	if cancellationRatioLocked(ctx) {
		if err := validateCancellationRatio(msg.Params.ProposalCancelRatio); err != nil {
			return nil, err
		}
	}
	return s.MsgServer.UpdateParams(ctx, msg)
}

func (s govParamsMsgServer) SubmitProposal(ctx context.Context, msg *govv1.MsgSubmitProposal) (*govv1.MsgSubmitProposalResponse, error) {
	if cancellationRatioLocked(ctx) {
		messages, err := msg.GetMsgs()
		if err != nil {
			return nil, err
		}
		for _, message := range messages {
			if update, ok := message.(*govv1.MsgUpdateParams); ok {
				if err := validateCancellationRatio(update.Params.ProposalCancelRatio); err != nil {
					return nil, err
				}
			}
		}
	}
	return s.MsgServer.SubmitProposal(ctx, msg)
}
