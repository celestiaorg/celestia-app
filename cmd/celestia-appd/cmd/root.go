package cmd

import (
	"fmt"
	"os"

	"cosmossdk.io/log"
	confixcmd "cosmossdk.io/tools/confix/cmd"
	"github.com/01builders/nova"
	"github.com/cometbft/cometbft/cmd/cometbft/commands"
	tmcli "github.com/cometbft/cometbft/libs/cli"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/client"
	clientconfig "github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	kitlog "github.com/go-kit/log"
	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/v4/app"
)

const (
	// FlagLogToFile specifies whether to log to file or not.
	FlagLogToFile = "log-to-file"

	// UpgradeHeightFlag is the flag to specify the upgrade height for v1 to v2
	// application upgrade.
	UpgradeHeightFlag = "v2-upgrade-height"

	// TimeoutCommit is a flag that can be used to override the timeout_commit.
	TimeoutCommitFlag = "timeout-commit"
)

// NewRootCmd creates a new root command for celestia-appd.
func NewRootCmd() *cobra.Command {
	// we "pre"-instantiate the application for getting the injected/configured encoding configuration
	// note, this is not necessary when using app wiring, as depinject can be directly used.
	tempApp := app.New(log.NewNopLogger(), dbm.NewMemDB(), nil, 0, simtestutil.EmptyAppOptions{})
	encodingConfig := tempApp.GetEncodingConfig()

	initClientContext := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(tempApp.GetTxConfig()).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(types.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastSync).
		WithHomeDir(app.DefaultNodeHome).
		WithViper(app.EnvPrefix)

	rootCommand := &cobra.Command{
		Use: "celestia-appd",
		PersistentPreRunE: func(command *cobra.Command, _ []string) error {
			command.SetOut(command.OutOrStdout())
			command.SetErr(command.ErrOrStderr())

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

			return nil
		},
		SilenceUsage: true,
	}

	rootCommand.PersistentFlags().String(FlagLogToFile, "", "Write logs directly to a file. If empty, logs are written to stderr")
	initRootCommand(rootCommand, tempApp)

	autoCliOpts := tempApp.AutoCliOpts()
	autoCliOpts.ClientCtx = initClientContext
	if err := autoCliOpts.EnhanceRootCommand(rootCommand); err != nil {
		panic(fmt.Errorf("failed to enhance root command: %w", err))
	}

	return rootCommand
}

// initRootCommand performs a bunch of side-effects on the root command.
func initRootCommand(rootCommand *cobra.Command, capp *app.App) {
	versions := Versions()

	debugCmd := debug.Cmd()
	debugCmd.AddCommand(
		NewInPlaceTestnetCmd(),
		AppGenesisToCometGenesisConverterCmd(),
	)
	passthroughCmd, _ := nova.NewPassthroughCmd(versions)
	// TODO: handle the error here. (currently breaking ledger tests as they do a cli exec and the expected binary bytes are not there)

	rootCommand.AddCommand(
		genutilcli.InitCmd(capp.BasicManager, app.DefaultNodeHome),
		genutilcli.Commands(capp.GetTxConfig(), capp.BasicManager, app.DefaultNodeHome),
		tmcli.NewCompletionCmd(rootCommand, true),
		debugCmd,
		confixcmd.ConfigCommand(),
		commands.CompactGoLevelDBCmd,
		addrbookCommand(),
		downloadGenesisCommand(),
		addrConversionCmd(),
		server.StatusCommand(),
		queryCommand(capp.BasicManager),
		txCommand(capp.BasicManager),
		keys.Commands(),
		snapshot.Cmd(NewAppServer),
		passthroughCmd,
	)

	// Add the following commands to the rootCommand: start, tendermint, export, version, and rollback.
	server.AddCommandsWithStartCmdOptions(rootCommand, app.DefaultNodeHome, NewAppServer, appExporter, server.StartCmdOptions{
		AddFlags:            addStartFlags,
		StartCommandHandler: nova.New(versions), // multiplexer
	})

	// find start command
	startCmd, _, err := rootCommand.Find([]string{"start"})
	if err != nil {
		panic(fmt.Errorf("failed to find start command: %w", err))
	}
	startCmdRunE := startCmd.RunE

	// Add the BBR check to the start command
	startCmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := checkBBR(cmd); err != nil {
			return err
		}

		return startCmdRunE(cmd, args)
	}
}

// addStartFlags adds flags to the start command.
func addStartFlags(startCmd *cobra.Command) {
	startCmd.Flags().Int64(UpgradeHeightFlag, 0, "Upgrade height to switch from v1 to v2. Must be coordinated amongst all validators")
	if err := startCmd.Flags().MarkDeprecated(UpgradeHeightFlag, "This flag is deprecated and was only useful prior to v4."); err != nil {
		panic(err)
	}

	startCmd.Flags().Duration(TimeoutCommitFlag, 0, "Override the application configured timeout_commit. Note: only for testing purposes.")
	startCmd.Flags().Bool(FlagForceNoBBR, false, "bypass the requirement to use bbr locally")
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
	sctx.Logger = log.NewLogger(kitlog.NewSyncWriter(file))
	return server.SetCmdServerContext(cmd, sctx)
}
