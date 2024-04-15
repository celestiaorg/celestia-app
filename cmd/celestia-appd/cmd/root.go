package cmd

import (
	"io"
	"os"
	"path/filepath"

	bscmd "github.com/celestiaorg/celestia-app/v2/x/blobstream/client"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/simapp/simd/cmd"
	"github.com/cosmos/cosmos-sdk/x/crisis"
	"github.com/tendermint/tendermint/cmd/cometbft/commands"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/config"
	"github.com/cosmos/cosmos-sdk/client/debug"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/keys"
	"github.com/cosmos/cosmos-sdk/client/rpc"
	"github.com/cosmos/cosmos-sdk/client/snapshot"
	"github.com/cosmos/cosmos-sdk/server"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/snapshots"
	snapshottypes "github.com/cosmos/cosmos-sdk/snapshots/types"
	"github.com/cosmos/cosmos-sdk/store"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authcmd "github.com/cosmos/cosmos-sdk/x/auth/client/cli"
	"github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutilcli "github.com/cosmos/cosmos-sdk/x/genutil/client/cli"
	"github.com/spf13/cast"
	"github.com/spf13/cobra"
	tmcli "github.com/tendermint/tendermint/libs/cli"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

const (
	EnvPrefix = "CELESTIA"

	// FlagLogToFile specifies whether to log to file or not.
	FlagLogToFile = "log-to-file"

	UpgradeHeightFlag = "v2-upgrade-height"
)

// NewRootCmd creates a new root command for celestia-appd. It is called once in the
// main function.
func NewRootCmd() *cobra.Command {
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	initClientCtx := client.Context{}.
		WithCodec(encodingConfig.Codec).
		WithInterfaceRegistry(encodingConfig.InterfaceRegistry).
		WithTxConfig(encodingConfig.TxConfig).
		WithLegacyAmino(encodingConfig.Amino).
		WithInput(os.Stdin).
		WithAccountRetriever(types.AccountRetriever{}).
		WithBroadcastMode(flags.BroadcastBlock).
		WithHomeDir(app.DefaultNodeHome).
		WithViper(EnvPrefix)

	rootCmd := &cobra.Command{
		Use:   "celestia-appd",
		Short: "Start celestia app",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			initClientCtx, err := client.ReadPersistentCommandFlags(initClientCtx, cmd.Flags())
			if err != nil {
				return err
			}
			initClientCtx, err = config.ReadFromClientConfig(initClientCtx)
			if err != nil {
				return err
			}

			if err := client.SetCmdClientContextHandler(initClientCtx, cmd); err != nil {
				return err
			}

			// Override the default tendermint config and app config for celestia-app
			var (
				tmCfg       = app.DefaultConsensusConfig()
				appConfig   = app.DefaultAppConfig()
				appTemplate = serverconfig.DefaultConfigTemplate
			)

			err = server.InterceptConfigsPreRunHandler(cmd, appTemplate, appConfig, tmCfg)
			if err != nil {
				return err
			}

			// optionally log to file by replacing the default logger with a file logger
			if cmd.Flags().Changed(FlagLogToFile) {
				err = replaceLogger(cmd)
				if err != nil {
					return err
				}
			}

			return setDefaultConsensusParams(cmd)
		},
		SilenceUsage: true,
	}

	rootCmd.PersistentFlags().String(FlagLogToFile, "", "Write logs directly to a file. If empty, logs are written to stderr")
	initRootCmd(rootCmd, encodingConfig)

	return rootCmd
}

// setDefaultConsensusParams sets the default consensus parameters for the
// embedded server context.
func setDefaultConsensusParams(command *cobra.Command) error {
	ctx := server.GetServerContextFromCmd(command)
	ctx.DefaultConsensusParams = app.DefaultConsensusParams()
	return server.SetCmdServerContext(command, ctx)
}

func initRootCmd(rootCmd *cobra.Command, encodingConfig encoding.Config) {
	cfg := sdk.GetConfig()
	cfg.Seal()

	debugCmd := debug.Cmd()

	rootCmd.AddCommand(
		genutilcli.InitCmd(app.ModuleBasics, app.DefaultNodeHome),
		genutilcli.CollectGenTxsCmd(banktypes.GenesisBalancesIterator{}, app.DefaultNodeHome),
		genutilcli.MigrateGenesisCmd(),
		cmd.AddGenesisAccountCmd(app.DefaultNodeHome),
		genutilcli.GenTxCmd(app.ModuleBasics, encodingConfig.TxConfig, banktypes.GenesisBalancesIterator{}, app.DefaultNodeHome),
		genutilcli.ValidateGenesisCmd(app.ModuleBasics),
		tmcli.NewCompletionCmd(rootCmd, true),
		debugCmd,
		config.Cmd(),
		commands.CompactGoLevelDBCmd,
		addrbookCommand(),
		downloadGenesisCommand(),
	)

	server.AddCommands(rootCmd, app.DefaultNodeHome, NewAppServer, createAppAndExport, addModuleInitFlags)

	// add status, query, tx, and keys subcommands
	rootCmd.AddCommand(
		rpc.StatusCommand(),
		queryCommand(),
		txCommand(),
		keys.Commands(app.DefaultNodeHome),
		bscmd.VerifyCmd(),
	)
	rootCmd.AddCommand(
		snapshot.Cmd(NewAppServer),
	)
}

func addModuleInitFlags(startCmd *cobra.Command) {
	crisis.AddModuleInitFlags(startCmd)
	startCmd.Flags().Int64(UpgradeHeightFlag, 0, "Upgrade height to switch from v1 to v2. Must be coordinated amongst all validators")
}

func queryCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "query",
		Aliases:                    []string{"q"},
		Short:                      "Querying subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetAccountCmd(),
		rpc.ValidatorCommand(),
		rpc.BlockCommand(),
		authcmd.QueryTxsByEventsCmd(),
		authcmd.QueryTxCmd(),
	)

	app.ModuleBasics.AddQueryCommands(cmd)
	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")

	return cmd
}

func txCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:                        "tx",
		Short:                      "Transactions subcommands",
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(
		authcmd.GetSignCommand(),
		authcmd.GetSignBatchCommand(),
		authcmd.GetMultiSignCommand(),
		authcmd.GetValidateSignaturesCommand(),
		flags.LineBreak,
		authcmd.GetBroadcastCommand(),
		authcmd.GetEncodeCommand(),
		authcmd.GetDecodeCommand(),
	)

	app.ModuleBasics.AddTxCommands(cmd)
	cmd.PersistentFlags().String(flags.FlagChainID, "", "The network chain ID")

	return cmd
}

func NewAppServer(logger log.Logger, db dbm.DB, traceStore io.Writer, appOpts servertypes.AppOptions) servertypes.Application {
	var cache sdk.MultiStorePersistentCache

	if cast.ToBool(appOpts.Get(server.FlagInterBlockCache)) {
		cache = store.NewCommitKVStoreCacheManager()
	}

	pruningOpts, err := server.GetPruningOptionsFromFlags(appOpts)
	if err != nil {
		panic(err)
	}

	// Add snapshots
	snapshotDir := filepath.Join(cast.ToString(appOpts.Get(flags.FlagHome)), "data", "snapshots")
	//nolint: staticcheck
	snapshotDB, err := sdk.NewLevelDB("metadata", snapshotDir)
	if err != nil {
		panic(err)
	}
	snapshotStore, err := snapshots.NewStore(snapshotDB, snapshotDir)
	if err != nil {
		panic(err)
	}

	return app.New(
		logger, db, traceStore,
		cast.ToUint(appOpts.Get(server.FlagInvCheckPeriod)),
		encoding.MakeConfig(app.ModuleEncodingRegisters...), // Ideally, we would reuse the one created by NewRootCmd.
		cast.ToInt64(appOpts.Get(UpgradeHeightFlag)),
		appOpts,
		baseapp.SetPruning(pruningOpts),
		baseapp.SetMinGasPrices(cast.ToString(appOpts.Get(server.FlagMinGasPrices))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(server.FlagMinRetainBlocks))),
		baseapp.SetHaltHeight(cast.ToUint64(appOpts.Get(server.FlagHaltHeight))),
		baseapp.SetHaltTime(cast.ToUint64(appOpts.Get(server.FlagHaltTime))),
		baseapp.SetMinRetainBlocks(cast.ToUint64(appOpts.Get(server.FlagMinRetainBlocks))),
		baseapp.SetInterBlockCache(cache),
		baseapp.SetTrace(cast.ToBool(appOpts.Get(server.FlagTrace))),
		baseapp.SetIndexEvents(cast.ToStringSlice(appOpts.Get(server.FlagIndexEvents))),
		baseapp.SetSnapshot(snapshotStore, snapshottypes.NewSnapshotOptions(cast.ToUint64(appOpts.Get(server.FlagStateSyncSnapshotInterval)), cast.ToUint32(appOpts.Get(server.FlagStateSyncSnapshotKeepRecent)))),
	)
}

func createAppAndExport(
	logger log.Logger, db dbm.DB, traceStore io.Writer, height int64, forZeroHeight bool, jailWhiteList []string,
	appOpts servertypes.AppOptions,
) (servertypes.ExportedApp, error) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...) // Ideally, we would reuse the one created by NewRootCmd.
	encCfg.Codec = codec.NewProtoCodec(encCfg.InterfaceRegistry)
	var capp *app.App
	if height != -1 {
		capp = app.New(logger, db, traceStore, uint(1), encCfg, 0, appOpts)

		if err := capp.LoadHeight(height); err != nil {
			return servertypes.ExportedApp{}, err
		}
	} else {
		capp = app.New(logger, db, traceStore, uint(1), encCfg, 0, appOpts)
	}

	return capp.ExportAppStateAndValidators(forZeroHeight, jailWhiteList)
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
