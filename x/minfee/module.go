package minfee

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"

	"github.com/celestiaorg/celestia-app/v4/x/minfee/keeper"
	"github.com/celestiaorg/celestia-app/v4/x/minfee/types"
)

var (
	_ module.AppModuleBasic      = AppModule{}
	_ module.AppModule           = AppModule{}
	_ module.HasGenesis          = AppModule{}
	_ module.HasGenesisBasics    = AppModule{}
	_ module.HasServices         = AppModule{}
	_ module.HasConsensusVersion = AppModule{}
	_ appmodule.AppModule        = AppModule{}
)

// AppModule implements the AppModule interface for the minfee module.
type AppModule struct {
	cdc          codec.Codec
	minfeeKeeper *keeper.Keeper
}

// NewAppModule creates a new AppModule object
func NewAppModule(cdc codec.Codec, minFeeKeeper *keeper.Keeper) AppModule {
	// Register the parameter key table in its associated subspace.
	subspace, exists := minFeeKeeper.GetParamsKeeper().GetSubspace(types.ModuleName)
	if !exists {
		panic("minfee subspace not set")
	}
	types.RegisterMinFeeParamTable(subspace)

	return AppModule{
		cdc:          cdc,
		minfeeKeeper: minFeeKeeper,
	}
}

func (AppModule) IsAppModule() {}

func (AppModule) IsOnePerModuleType() {}

// Name returns the minfee module's name.
func (AppModule) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec registers the blob module's types on the LegacyAmino codec.
func (AppModule) RegisterLegacyAminoCodec(_ *codec.LegacyAmino) {}

// RegisterInterfaces registers interfaces and implementations of the minfee module.
func (AppModule) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (am AppModule) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux) {}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), am.minfeeKeeper)
	types.RegisterQueryServer(cfg.QueryServer(), am.minfeeKeeper)

	m := keeper.NewMigrator(am.minfeeKeeper)
	if err := cfg.RegisterMigration(types.ModuleName, 1, m.MigrateParams); err != nil {
		panic(err)
	}
}

// DefaultGenesis returns default genesis state as raw bytes for the minfee module.
func (am AppModule) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	return am.cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the minfee module.
func (am AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var data types.GenesisState
	if err := am.cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}

	return types.ValidateGenesis(&data)
}

// InitGenesis performs genesis initialization for the minfee module.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, gs json.RawMessage) {
	var genesisState types.GenesisState
	if err := am.cdc.UnmarshalJSON(gs, &genesisState); err != nil {
		panic(fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err))
	}

	if err := am.minfeeKeeper.InitGenesis(ctx, genesisState); err != nil {
		panic(fmt.Errorf("failed to initialize %s genesis state: %w", types.ModuleName, err))
	}
}

// ExportGenesis returns the exported genesis state as raw bytes for the minfee module.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	gs := am.minfeeKeeper.ExportGenesis(sdk.UnwrapSDKContext(ctx))
	return am.cdc.MustMarshalJSON(gs)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 2 }
