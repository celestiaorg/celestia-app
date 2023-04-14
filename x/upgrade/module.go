package upgrade

import (
	"context"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/x/upgrade"
	cli "github.com/cosmos/cosmos-sdk/x/upgrade/client/cli"
	upgradekeeper "github.com/cosmos/cosmos-sdk/x/upgrade/keeper"
	upgradetypes "github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	abci "github.com/tendermint/tendermint/abci/types"
)

var (
	_ module.AppModule = AppModule{}
)

const (
	consensusVersion uint64 = 1
)

var (
	_ module.AppModule      = AppModule{}
	_ module.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic implements the sdk.AppModuleBasic interface
type AppModuleBasic struct {
	upgrade.AppModuleBasic
}

// Name returns the ModuleName
func (AppModuleBasic) Name() string {
	return upgradetypes.ModuleName
}

// RegisterLegacyAminoCodec registers the upgrade types on the LegacyAmino codec
func (AppModuleBasic) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	upgradetypes.RegisterLegacyAminoCodec(cdc)
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the upgrade module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := upgradetypes.RegisterQueryHandlerClient(context.Background(), mux, upgradetypes.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetQueryCmd returns the cli query commands for this module
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	return GetQueryCmd()
}

// GetTxCmd returns the transaction commands for this module
func (AppModuleBasic) GetTxCmd() *cobra.Command {
	return cli.GetTxCmd()
}

func (b AppModuleBasic) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	upgradetypes.RegisterInterfaces(registry)
}

// AppModule implements the sdk.AppModule interface by wrapping the standard
// upgrade module and overwriting methods specifc to the msg server.
type AppModule struct {
	upgrade.AppModule
}

// NewAppModule returns the AppModule for the application's upgrade module.
func NewAppModule(keeper upgradekeeper.Keeper) AppModule {
	return AppModule{
		upgrade.NewAppModule(keeper),
	}
}

// RegisterInterfaces overwrites the default upgrade module's method with a
// noop.
func (AppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
}

// RegisterServices overwrites the default upgrade module's method with a
// noop.
func (am AppModule) RegisterServices(cfg module.Configurator) {
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return consensusVersion }

// BeginBlock overwrites the default upgrade module's BeginBlock method with a
// noop.
func (am AppModule) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) {
}
