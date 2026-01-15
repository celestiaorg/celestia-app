package forwarding

import (
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
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

// AppModule implements the AppModule interface for the forwarding module.
type AppModule struct {
	cdc              codec.Codec
	forwardingKeeper keeper.Keeper
}

// NewAppModule creates a new AppModule object
func NewAppModule(cdc codec.Codec, forwardingKeeper keeper.Keeper) AppModule {
	return AppModule{
		cdc:              cdc,
		forwardingKeeper: forwardingKeeper,
	}
}

func (AppModule) IsAppModule() {}

func (AppModule) IsOnePerModuleType() {}

// Name returns the forwarding module's name.
func (AppModule) Name() string {
	return types.ModuleName
}

// RegisterLegacyAminoCodec registers the forwarding module's types on the LegacyAmino codec.
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers interfaces and implementations of the forwarding module.
func (AppModule) RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(registry)
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the module.
func (am AppModule) RegisterGRPCGatewayRoutes(_ client.Context, _ *runtime.ServeMux) {}

// RegisterServices registers module services.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	types.RegisterMsgServer(cfg.MsgServer(), keeper.NewMsgServerImpl(am.forwardingKeeper))
	types.RegisterQueryServer(cfg.QueryServer(), keeper.NewQueryServerImpl(am.forwardingKeeper))
}

// DefaultGenesis returns default genesis state as raw bytes for the forwarding module.
func (am AppModule) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	return am.cdc.MustMarshalJSON(types.DefaultGenesis())
}

// ValidateGenesis performs genesis state validation for the forwarding module.
func (am AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var data types.GenesisState
	if err := am.cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err)
	}

	return types.ValidateGenesis(&data)
}

// InitGenesis performs genesis initialization for the forwarding module.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, gs json.RawMessage) {
	var genesisState types.GenesisState
	if err := am.cdc.UnmarshalJSON(gs, &genesisState); err != nil {
		panic(fmt.Errorf("failed to unmarshal %s genesis state: %w", types.ModuleName, err))
	}

	if err := am.forwardingKeeper.InitGenesis(ctx, &genesisState); err != nil {
		panic(fmt.Errorf("failed to initialize %s genesis state: %w", types.ModuleName, err))
	}
}

// ExportGenesis returns the exported genesis state as raw bytes for the forwarding module.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	gs, err := am.forwardingKeeper.ExportGenesis(ctx)
	if err != nil {
		panic(fmt.Errorf("failed to export %s genesis state: %w", types.ModuleName, err))
	}
	return am.cdc.MustMarshalJSON(gs)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (AppModule) ConsensusVersion() uint64 { return 1 }
