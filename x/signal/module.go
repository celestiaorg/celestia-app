package signal

import (
	"context"
	"encoding/json"

	"cosmossdk.io/core/appmodule"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"

	"github.com/celestiaorg/celestia-app/v4/x/signal/cli"
	"github.com/celestiaorg/celestia-app/v4/x/signal/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/types/module"
)

const (
	// consensusVersion defines the current x/signal module consensus version.
	consensusVersion uint64 = 3
)

var (
	_ module.AppModuleBasic   = AppModule{}
	_ module.HasGenesisBasics = AppModule{}

	_ appmodule.AppModule = AppModule{}
)

// AppModule implements the AppModule interface for the blobstream module.
type AppModule struct {
	keeper Keeper
}

func NewAppModule(k Keeper) AppModule {
	return AppModule{k}
}

// Name returns the ModuleName
func (AppModule) Name() string {
	return types.ModuleName
}

func (AppModule) IsAppModule() {}

func (AppModule) IsOnePerModuleType() {}

// RegisterLegacyAminoCodec registers the blob module's types on the LegacyAmino codec.
func (AppModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	types.RegisterLegacyAminoCodec(cdc)
}

// RegisterInterfaces registers interfaces and implementations of the blob module.
func (AppModule) RegisterInterfaces(reg cdctypes.InterfaceRegistry) {
	types.RegisterInterfaces(reg)
}

// RegisterGRPCGatewayRoutes registers the gRPC Gateway routes for the upgrade module.
func (AppModule) RegisterGRPCGatewayRoutes(clientCtx client.Context, mux *runtime.ServeMux) {
	if err := types.RegisterQueryHandlerClient(context.Background(), mux, types.NewQueryClient(clientCtx)); err != nil {
		panic(err)
	}
}

// GetQueryCmd returns the CLI query commands for this module.
// TODO(@julienrbrt): Use AutoCLI
func (AppModule) GetQueryCmd() *cobra.Command {
	return cli.GetQueryCmd()
}

// GetTxCmd returns the CLI transaction commands for this module.
func (AppModule) GetTxCmd() *cobra.Command {
	return cli.GetTxCmd()
}

// DefaultGenesis returns the blob module's default genesis state.
func (am AppModule) DefaultGenesis(_ codec.JSONCodec) json.RawMessage {
	return []byte("{}")
}

// ValidateGenesis is always successful, as we ignore the value.
func (am AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	return nil
}

// RegisterServices registers a GRPC query service to respond to the
// module-specific GRPC queries.
func (am AppModule) RegisterServices(registrar grpc.ServiceRegistrar) {
	types.RegisterMsgServer(registrar, &am.keeper)
	types.RegisterQueryServer(registrar, &am.keeper)
}

// ConsensusVersion returns the consensus version of this module.
func (AppModule) ConsensusVersion() uint64 { return consensusVersion }
