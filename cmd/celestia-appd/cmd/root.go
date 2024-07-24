package cmd

import (
	"os"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	blobstreamclient "github.com/celestiaorg/celestia-app/v3/x/blobstream/client"
	"github.com/cosmos/cosmos-sdk/client"
	clientconfig "github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	simdcmd "github.com/cosmos/cosmos-sdk/simapp/simd/cmd"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/cmd/cometbft/commands"
	tmcli "github.com/tendermint/tendermint/libs/cli"
	"github.com/tendermint/tendermint/libs/log"
)

const (
	// EnvPrefix is the environment variable prefix for celestia-appd.
	// Environment variables that Cobra reads must be prefixed with this value.
	EnvPrefix = "CELESTIA"

	// FlagLogToFile specifies whether to log to file or not.
	FlagLogToFile = "log-to-file"

	// UpgradeHeightFlag is the flag to specify the upgrade height for v1 to v2
	// application upgrade.
	UpgradeHeightFlag = "v2-upgrade-height"
)

// NewRootCmd creates a new root command for celestia-appd.
func NewRootCmd() *cobra.Command {
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	initClientContext := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(types.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastBlock).
		WithHomeDir(app.DefaultNodeHome).
		WithViper(EnvPrefix)

	rootCommand := &cobra.Command{
		Use:   "celestia-appd",
		Short: "Start celestia app",
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			clientContext, err := client.ReadPersistentCommandFlags(initClientContext, command.Flags())
			if err != nil {
				return err
			}
			clientContext, err = clientconfig.ReadFromClientConfig(clientContext)
			if err != nil {
				return err
			}
			if err := client.SetCmdClientContextHandler(clientContext, command); err != nil {
				return err
			}

			appTemplate := serverconfig.DefaultConfigTemplate
			appConfig := app.DefaultAppConfig()
			tmConfig := app.DefaultConsensusConfig()

			// Override the default tendermint config and app config for celestia-app
			err = server.InterceptConfigsPreRunHandler(command, appTemplate, appConfig, tmConfig)
			if err != nil {
				return err
			}

			if command.Flags().Changed(FlagLogToFile) {
				// optionally log to file by replacing the default logger with a file logger
				err = replaceLogger(command)
				if err != nil {
					return err
				}
			}

			return setDefaultConsensusParams(command)
		},
		SilenceUsage: true,
	}

	rootCommand.PersistentFlags().String(FlagLogToFile, "", "Write logs directly to a file. If empty, logs are written to stderr")
	initRootCommand(rootCommand, encodingConfig)

	return rootCommand
}

// initRootCommand performs a bunch of side-effects on the root command.
func initRootCommand(rootCommand *cobra.Command, encodingConfig encoding.Config) {
	config := sdk.GetConfig()
	config.Seal()

	rootCommand.AddCommand(
		genutilcli.InitCmd(app.ModuleBasics, app.DefaultNodeHome),
		genutilcli.CollectGenTxsCmd(banktypes.GenesisBalancesIterator{}, app.DefaultNodeHome),
		genutilcli.MigrateGenesisCmd(),
		simdcmd.AddGenesisAccountCmd(app.DefaultNodeHome),
		genutilcli.GenTxCmd(app.ModuleBasics, encodingConfig.TxConfig, banktypes.GenesisBalancesIterator{}, app.DefaultNodeHome),
		genutilcli.ValidateGenesisCmd(app.ModuleBasics),
		tmcli.NewCompletionCmd(rootCommand, true),
		debug.Cmd(),
		clientconfig.Cmd(),
		commands.CompactGoLevelDBCmd,
		addrbookCommand(),
		downloadGenesisCommand(),
		addrConversionCmd(),
		rpc.StatusCommand(),
		queryCommand(),
		txCommand(),
		keys.Commands(app.DefaultNodeHome),
		blobstreamclient.VerifyCmd(),
		snapshot.Cmd(NewAppServer),
	)

	server.AddCommands(rootCommand, app.DefaultNodeHome, NewAppServer, appExporter, addModuleInitFlags)
}

// setDefaultConsensusParams sets the default consensus parameters for the
// embedded server context.
func setDefaultConsensusParams(command *cobra.Command) error {
	ctx := server.GetServerContextFromCmd(command)
	ctx.DefaultConsensusParams = app.DefaultConsensusParams()
	return server.SetCmdServerContext(command, ctx)
}

func addModuleInitFlags(startCmd *cobra.Command) {
	crisis.AddModuleInitFlags(startCmd)
	startCmd.Flags().Int64(UpgradeHeightFlag, 0, "Upgrade height to switch from v1 to v2. Must be coordinated amongst all validators")
}

// replaceLogger optionally replaces the logger with a file logger if the flag
// is set to something other than the default.
func replaceLogger(cmd *cobra.Command) error {
	logFilePath, err := cmd.Flags().GetString(FlagLogToFile)
	if err != nil {
		return err
	}

	if logFilePath == "" {
		return nil
	}

	file, err := os.OpenFile(logFilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	sctx := server.GetServerContextFromCmd(cmd)
	sctx.Logger = log.NewTMLogger(log.NewSyncWriter(file))
	return server.SetCmdServerContext(cmd, sctx)
}
