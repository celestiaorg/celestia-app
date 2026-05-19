package ethidentity

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"github.com/celestiaorg/celestia-app/v9/x/ethidentity/keeper"
	"github.com/celestiaorg/celestia-app/v9/x/ethidentity/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
)

const consensusVersion uint64 = 1

var (
	_ appmodule.AppModule        = AppModule{}
	_ module.AppModule           = AppModule{}
	_ module.AppModuleBasic      = AppModule{}
	_ module.HasConsensusVersion = AppModule{}
	_ module.HasGenesis          = AppModule{}
	_ module.HasGenesisBasics    = AppModule{}
)

// AppModule implements the ethidentity module.
type AppModule struct {
	keeper keeper.Keeper
}

// NewAppModule creates an ethidentity app module.
func NewAppModule(keeper keeper.Keeper) AppModule {
	return AppModule{keeper: keeper}
}

// IsAppModule implements the appmodule.AppModule interface.
func (AppModule) IsAppModule() {}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (AppModule) IsOnePerModuleType() {}

// Name returns the module name.
func (AppModule) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec registers the module's concrete types.
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers module interfaces.
func (AppModule) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterGRPCGatewayRoutes registers module gRPC Gateway routes.
func (AppModule) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux) {}

// DefaultGenesis returns the module's default genesis state.
func (AppModule) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	data, err := json.Marshal(types.DefaultGenesis())
	if err != nil {
		panic(fmt.Errorf("failed to marshal %s default genesis: %w", types.ModuleName, err))
	}
	return data
}

// ValidateGenesis validates the module genesis state.
func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, data json.RawMessage) error {
	var genesis types.GenesisState
	if err := json.Unmarshal(data, &genesis); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}
	return types.ValidateGenesis(genesis)
}

// InitGenesis initializes the module state from genesis.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, data json.RawMessage) {
	var genesis types.GenesisState
	if err := json.Unmarshal(data, &genesis); err != nil {
		panic(fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err))
	}
	if err := types.ValidateGenesis(genesis); err != nil {
		panic(fmt.Errorf("invalid %s genesis state: %w", types.ModuleName, err))
	}
	if err := am.keeper.InitGenesis(ctx, genesis); err != nil {
		panic(fmt.Errorf("failed to initialize %s genesis state: %w", types.ModuleName, err))
	}
}

// ExportGenesis exports the module genesis state.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	genesis := am.keeper.ExportGenesis(ctx)
	data, err := json.Marshal(genesis)
	if err != nil {
		panic(fmt.Errorf("failed to marshal %s genesis state: %w", types.ModuleName, err))
	}
	return data
}

// ConsensusVersion returns the module consensus version.
func (AppModule) ConsensusVersion() uint64 {
	return consensusVersion
}
