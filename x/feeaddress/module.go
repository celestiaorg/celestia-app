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

func (AppModule) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	return []byte("{}")
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, _ json.RawMessage) error {
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
	types.RegisterMsgServer(registrar, &am.keeper)
	return nil
}
