package warp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"cosmossdk.io/core/appmodule"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/warp/client/cli"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	corekeeper "github.com/bcp-innovations/hyperlane-cosmos/x/core/keeper"
	celestiawarptypes "github.com/celestiaorg/celestia-app/v6/x/warp/types"
)

var (
	_ module.AppModuleBasic = PermissionlessAppModule{}
	_ module.HasGenesis     = PermissionlessAppModule{}
	_ appmodule.AppModule   = PermissionlessAppModule{}
)

// ConsensusVersion defines the current module consensus version.
const ConsensusVersion = 1

type PermissionlessAppModule struct {
	cdc             codec.Codec
	keeper          warpkeeper.Keeper
	hyperlaneKeeper *corekeeper.Keeper
}

// NewPermissionlessAppModule creates a new AppModule object with permissionless enrollment
func NewPermissionlessAppModule(cdc codec.Codec, keeper warpkeeper.Keeper, hyperlaneKeeper *corekeeper.Keeper) PermissionlessAppModule {
	return PermissionlessAppModule{
		cdc:             cdc,
		keeper:          keeper,
		hyperlaneKeeper: hyperlaneKeeper,
	}
}

// Name returns the warp module's name.
func (PermissionlessAppModule) Name() string { return warptypes.ModuleName }

// RegisterLegacyAminoCodec registers ONLY the Celestia-specific warp module types.
// The base hyperlane warp types are registered by warp.AppModule{} in ModuleEncodingRegisters.
func (PermissionlessAppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	// Only register Celestia-specific permissionless messages
	// Do NOT re-register hyperlane warp messages as they're already registered by warp.AppModule{}
	cdc.RegisterConcrete(&celestiawarptypes.MsgSetupPermissionlessInfrastructure{}, "celestia/warp/MsgSetupPermissionlessInfrastructure", nil)
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the warp module.
func (PermissionlessAppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *gwruntime.ServeMux) {
	if err := warptypes.RegisterQueryHandlerClient(context.Background(), mux, warptypes.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// RegisterInterfaces registers ONLY Celestia-specific interfaces and implementations.
// The base hyperlane warp interfaces are registered by warp.AppModule{} in ModuleEncodingRegisters.
func (PermissionlessAppModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	// Only register Celestia-specific interfaces
	// Do NOT re-register hyperlane warp interfaces as they're already registered by warp.AppModule{}
	celestiawarptypes.RegisterInterfaces(registry)
}

// ConsensusVersion implements AppModule/ConsensusVersion.
func (PermissionlessAppModule) ConsensusVersion() uint64 { return ConsensusVersion }

// RegisterServices registers a gRPC query service with PERMISSIONLESS msg server
func (am PermissionlessAppModule) RegisterServices(cfg module.Configurator) {
	// Register Celestia-specific setup message server FIRST
	// This must be done before the permissionless wrapper to ensure type URLs are registered
	celestiawarptypes.RegisterMsgServer(cfg.MsgServer(), NewSetupMsgServer(&am.keeper, am.hyperlaneKeeper))

	// THIS IS THE KEY CHANGE: Use our permissionless msg server instead of the default one
	warptypes.RegisterMsgServer(cfg.MsgServer(), NewPermissionlessMsgServer(&am.keeper))
	warptypes.RegisterQueryServer(cfg.QueryServer(), warpkeeper.NewQueryServerImpl(am.keeper))
}

// DefaultGenesis returns default genesis state as raw bytes for the module.
func (PermissionlessAppModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(warptypes.NewGenesisState())
}

// ValidateGenesis performs genesis state validation for the warp module.
func (PermissionlessAppModule) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var data warptypes.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("failed to unmarshal %s genesis state: %w", warptypes.ModuleName, err)
	}

	return data.Validate()
}

// InitGenesis performs genesis initialization for the warp module.
func (am PermissionlessAppModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, data json.RawMessage) {
	var genesisState warptypes.GenesisState
	cdc.MustUnmarshalJSON(data, &genesisState)

	if err := am.keeper.InitGenesis(ctx, &genesisState); err != nil {
		panic(err)
	}
}

// ExportGenesis returns the exported genesis state as raw bytes for the warp module.
func (am PermissionlessAppModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	gs, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	return cdc.MustMarshalJSON(gs)
}

// GetTxCmd returns the root tx command for the warp module.
func (PermissionlessAppModule) GetTxCmd() *cobra.Command {
	return cli.GetTxCmd()
}

// IsOnePerModuleType implements the depinject.OnePerModuleType interface.
func (am PermissionlessAppModule) IsOnePerModuleType() {}

// IsAppModule implements the appmodule.AppModule interface.
func (am PermissionlessAppModule) IsAppModule() {}
