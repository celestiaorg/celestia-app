package minfee

import (
	"encoding/json"
	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	abci "github.com/tendermint/tendermint/abci/types"
    "fmt"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	params "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

var (
	_ sdkmodule.AppModule      = AppModule{}
	_ sdkmodule.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic defines the basic application module used by the minfee module.
type AppModuleBasic struct{}

// RegisterInterfaces registers the module's interfaces with the interface registry.
func (AppModuleBasic) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {}

// Name returns the minfee module's name.
func (AppModuleBasic) Name() string {
	return ModuleName
}

// RegisterLegacyAminoCodec does nothing. MinFee doesn't use Amino.
func (AppModuleBasic) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

// DefaultGenesis returns default genesis state as raw bytes for the minfee module.
func (AppModuleBasic) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the minfee module.
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, config client.TxEncodingConfig, bz json.RawMessage) error {
	var data GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return err
	}
	return ValidateGenesis(&data)
}

// RegisterRESTRoutes registers the capability module's REST service handlers.
func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
}

// GetTxCmd returns the capability module's root tx command.
func (a AppModuleBasic) GetTxCmd() *cobra.Command {
	// Return a dummy command
	return &cobra.Command{}
}

// GetQueryCmd returns the capability module's root query command.
func (AppModuleBasic) GetQueryCmd() *cobra.Command {
	// Return a dummy command
	return &cobra.Command{}
}

// AppModule implements an application module for the minfee module.
type AppModule struct {
	AppModuleBasic
	paramsKeeper params.Keeper
}

// NewAppModule creates a new AppModule object
func NewAppModule(k params.Keeper) AppModule {
	return AppModule{
		AppModuleBasic: AppModuleBasic{},
        paramsKeeper: k,
	}
}

// RegisterInvariants registers the minfee module invariants.
func (am AppModule) RegisterInvariants(ir sdk.InvariantRegistry) {}

// Route returns the message routing key for the minfee module.
func (am AppModule) Route() sdk.Route {
	return sdk.Route{}
}

// QuerierRoute returns the minfee module's querier route name.
func (am AppModule) QuerierRoute() string {
	return ""
}

// LegacyQuerierHandler returns the capability module's Querier.
func (am AppModule) LegacyQuerierHandler(_ *codec.LegacyAmino) sdk.Querier {
	return nil
}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(configurator sdkmodule.Configurator) {}

// InitGenesis performs genesis initialization for the minfee module. It returns no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, gs json.RawMessage) []abci.ValidatorUpdate {
	fmt.Println("HELLOOOOOO FROM MIN FEE")
	var genesisState GenesisState
	cdc.MustUnmarshalJSON(gs, &genesisState)

	// set the global min gas price in the min fee subspace
	subspace, _ := am.paramsKeeper.GetSubspace(ModuleName)
	subspace.Set(ctx, KeyGlobalMinGasPrice, genesisState.GlobalMinGasPrice)

	// fmt.Println("VALUE WAS SET WOOHOO")

	return []abci.ValidatorUpdate{}
}

// ExportGenesis returns the exported genesis state as raw bytes for the minfee module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs := ExportGenesis(ctx, am.paramsKeeper)
	return cdc.MustMarshalJSON(gs)
}

// ExportGenesis returns the capability module's exported genesis.
func ExportGenesis(ctx sdk.Context, k params.Keeper) *GenesisState {
	globalMinGasPrice, bool := k.GetSubspace(ModuleName)

	var minGasPrice float64
	globalMinGasPrice.Get(ctx, KeyGlobalMinGasPrice, &minGasPrice)
	if !bool {
		panic("global min gas price not found")
	}
	return &GenesisState{GlobalMinGasPrice: minGasPrice}
}

// BeginBlock returns the begin blocker for the minfee module.
func (am AppModule) BeginBlock(ctx sdk.Context, req abci.RequestBeginBlock) {}

// EndBlock returns the end blocker for the minfee module. It returns no validator updates.
func (am AppModule) EndBlock(ctx sdk.Context, req abci.RequestEndBlock) []abci.ValidatorUpdate {
	return []abci.ValidatorUpdate{}
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 1 }
