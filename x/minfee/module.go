package minfee

import (
	"encoding/json"
	"fmt"

	grpc "google.golang.org/grpc"

	"cosmossdk.io/core/appmodule"
	"cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	params "github.com/cosmos/cosmos-sdk/x/params/keeper"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
)

var (
	_ module.AppModuleBasic = AppModule{}
	_ module.AppModule      = AppModule{}
	_ module.HasGenesis     = AppModule{}

	_ appmodule.AppModule = AppModule{}
)

// AppModule implements the AppModule interface for the minfee module.
type AppModule struct {
	cdc          codec.Codec
	paramsKeeper params.Keeper
}

// NewAppModule creates a new AppModule object
func NewAppModule(cdc codec.Codec, k params.Keeper) AppModule {
	// Register the parameter key table in its associated subspace.
	subspace, exists := k.GetSubspace(ModuleName)
	if !exists {
		panic("minfee subspace not set")
	}
	RegisterMinFeeParamTable(subspace)

	return AppModule{
		cdc:          cdc,
		paramsKeeper: k,
	}
}

func (AppModule) IsAppModule() {}

func (AppModule) IsOnePerModuleType() {}

// Name returns the minfee module's name.
func (AppModule) Name() string {
	return ModuleName
}

// RegisterLegacyAminoCodec registers the blob module's types on the LegacyAmino codec.
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {}

// RegisterInterfaces registers interfaces and implementations of the blob module.
func (AppModule) RegisterInterfaces(reg cdctypes.InterfaceRegistry) {}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (am AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) {
	RegisterQueryServer(registrar, NewQueryServerImpl(am.paramsKeeper))
}

// DefaultGenesis returns default genesis state as raw bytes for the minfee module.
func (am AppModule) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	return am.cdc.MustMarshalJSON(DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the minfee module.
func (am AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var data GenesisState
	if err := am.cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", ModuleName, err)
	}

	return ValidateGenesis(&data)
}

// InitGenesis performs genesis initialization for the minfee module.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, gs json.RawMessage) {
	var genesisState GenesisState
	if err := am.cdc.UnmarshalJSON(gs, &genesisState); err != nil {
		panic(fmt.Errorf("failed to unmarshal %s genesis state: %w", ModuleName, err))
	}

	subspace, exists := am.paramsKeeper.GetSubspace(ModuleName)
	if !exists {
		panic(fmt.Errorf("minfee subspace not set"))
	}

	subspace = RegisterMinFeeParamTable(subspace)

	// Set the network min gas price initial value
	networkMinGasPriceDec, err := math.LegacyNewDecFromStr(fmt.Sprintf("%f", genesisState.NetworkMinGasPrice))
	if err != nil {
		panic(fmt.Errorf("failed to convert NetworkMinGasPrice to "))
	}
	subspace.SetParamSet(sdk.UnwrapSDKContext(ctx), &Params{NetworkMinGasPrice: networkMinGasPriceDec})
}

// ExportGenesis returns the exported genesis state as raw bytes for the minfee module.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	gs := ExportGenesis(sdk.UnwrapSDKContext(ctx), am.paramsKeeper)
	return am.cdc.MustMarshalJSON(gs)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 1 }
