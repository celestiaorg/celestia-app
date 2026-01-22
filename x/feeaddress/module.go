package feeaddress

import (
	"context"
	"encoding/json"

	"cosmossdk.io/core/appmodule"
	"github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/grpc"
)

var (
	_ module.AppModuleBasic   = AppModule{}
	_ module.HasGenesisBasics = AppModule{}
	_ appmodule.AppModule     = AppModule{}
	_ appmodule.HasServices   = AppModule{}
)

type AppModule struct {
	keeper Keeper
}

func NewAppModule(k Keeper) AppModule {
	return AppModule{keeper: k}
}

func (AppModule) Name() string             { return types.ModuleName }
func (AppModule) IsAppModule()             {}
func (AppModule) IsOnePerModuleType()      {}
func (AppModule) ConsensusVersion() uint64 { return 1 }

func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

func (AppModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(&types.GenesisState{})
}

func (AppModule) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var gs types.GenesisState
	if err := cdc.UnmarshalJSON(bz, &gs); err != nil {
		return err
	}
	// GenesisState is empty, no further validation needed
	return nil
}

func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

func (AppModule) RegisterInterfaces(reg cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(reg)
}

func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) error {
	types.RegisterQueryServer(registrar, &am.keeper)
	// MsgServer must be registered for SDK message routing even though
	// MsgForwardFees is protocol-injected. User submissions are rejected
	// by FeeForwardTerminatorDecorator in the ante chain (CheckTx, ReCheckTx, simulate).
	types.RegisterMsgServer(registrar, &am.keeper)
	return nil
}
