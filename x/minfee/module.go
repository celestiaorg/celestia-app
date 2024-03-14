package minfee

import (
	"encoding/json"
	"fmt"

	"github.com/gorilla/mux"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"

	abci "github.com/tendermint/tendermint/abci/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
	params "github.com/cosmos/cosmos-sdk/x/params/keeper"
)

var (
	_ sdkmodule.AppModule      = AppModule{}
	_ sdkmodule.AppModuleBasic = AppModuleBasic{}
)

// AppModuleBasic defines the basic application module used by the minfee module.
type AppModuleBasic struct{}

// RegisterInterfaces registers the module's interfaces with the interface registry.
func (AppModuleBasic) RegisterInterfaces(_ cdctypes.InterfaceRegistry) {}

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
func (AppModuleBasic) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var data GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", ModuleName, err)
	}
	return ValidateGenesis(&data)
}

// RegisterRESTRoutes registers the REST service handlers for the module.
func (AppModuleBasic) RegisterRESTRoutes(_ client.Context, _ *mux.Router) {}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (AppModuleBasic) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux) {}

// GetTxCmd returns the minfee module's root tx command.
func (a AppModuleBasic) GetTxCmd() *cobra.Command {
	// Return a dummy command
	return &cobra.Command{}
}

// GetQueryCmd returns the minfee module's root query command.
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
		paramsKeeper:   k,
	}
}

// RegisterInvariants registers the minfee module invariants.
func (am AppModule) RegisterInvariants(_ sdk.InvariantRegistry) {}

// Route returns the message routing key for the minfee module.
func (am AppModule) Route() sdk.Route {
	return sdk.Route{}
}

// QuerierRoute returns the minfee module's querier route name.
func (am AppModule) QuerierRoute() string {
	return ""
}

// LegacyQuerierHandler returns the minfee module's Querier.
func (am AppModule) LegacyQuerierHandler(_ *codec.LegacyAmino) sdk.Querier {
	return nil
}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(_ sdkmodule.Configurator) {}

// InitGenesis performs genesis initialization for the minfee module. It returns no validator updates.
func (am AppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, gs json.RawMessage) []abci.ValidatorUpdate {
	var genesisState GenesisState
	cdc.MustUnmarshalJSON(gs, &genesisState)
	
	
	// Set the global min gas price initial value
	subspace, _ := am.paramsKeeper.GetSubspace(ModuleName)
	RegisterMinFeeParamTable(subspace)
	globalMinGasPriceDec, _ := sdk.NewDecFromStr(fmt.Sprintf("%f", genesisState.GlobalMinGasPrice))
	fmt.Println("min fee init genesis", globalMinGasPriceDec)
	subspace.Set(ctx, KeyGlobalMinGasPrice, globalMinGasPriceDec)

	return []abci.ValidatorUpdate{}
}

// ExportGenesis returns the exported genesis state as raw bytes for the minfee module.
func (am AppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs := ExportGenesis(ctx, am.paramsKeeper)
	return cdc.MustMarshalJSON(gs)
}

// ExportGenesis returns the capability module's exported genesis.
func ExportGenesis(ctx sdk.Context, k params.Keeper) *GenesisState {
	globalMinGasPrice, ok := k.GetSubspace(ModuleName)

	var minGasPrice sdk.Dec
	globalMinGasPrice.Get(ctx, KeyGlobalMinGasPrice, &minGasPrice)
	if !ok {
		panic("global min gas price not found")
	}

	return &GenesisState{GlobalMinGasPrice: minGasPrice.MustFloat64()}
}

// BeginBlock returns the begin blocker for the minfee module.
func (am AppModule) BeginBlock(_ sdk.Context, _ abci.RequestBeginBlock) {}

// EndBlock returns the end blocker for the minfee module. It returns no validator updates.
func (am AppModule) EndBlock(_ sdk.Context, _ abci.RequestEndBlock) []abci.ValidatorUpdate {
	return []abci.ValidatorUpdate{}
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 1 }
