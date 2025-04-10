// Added flags for in-place testnet cmd
package cmd

import (
	"time"

	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	cmtcmd "github.com/tendermint/tendermint/cmd/cometbft/commands"
)

const (
	// CometBFT full-node start flags
	flagWithComet          = "with-comet"
	FlagMinGasPrices       = "minimum-gas-prices"
	FlagQueryGasLimit      = "query-gas-limit"
	FlagHaltHeight         = "halt-height"
	FlagHaltTime           = "halt-time"
	FlagInterBlockCache    = "inter-block-cache"
	FlagUnsafeSkipUpgrades = "unsafe-skip-upgrades"
	FlagTrace              = "trace"
	FlagInvCheckPeriod     = "inv-check-period"

	FlagPruning             = "pruning"
	FlagPruningKeepRecent   = "pruning-keep-recent"
	FlagPruningInterval     = "pruning-interval"
	FlagIndexEvents         = "index-events"
	FlagMinRetainBlocks     = "min-retain-blocks"
	FlagIAVLCacheSize       = "iavl-cache-size"
	FlagDisableIAVLFastNode = "iavl-disable-fastnode"
	FlagShutdownGrace       = "shutdown-grace"

	// state sync-related flags
	FlagStateSyncSnapshotInterval   = "state-sync.snapshot-interval"
	FlagStateSyncSnapshotKeepRecent = "state-sync.snapshot-keep-recent"

	// api-related flags
	FlagAPIEnable             = "api.enable"
	FlagAPISwagger            = "api.swagger"
	FlagAPIAddress            = "api.address"
	FlagAPIMaxOpenConnections = "api.max-open-connections"
	FlagRPCReadTimeout        = "api.rpc-read-timeout"
	FlagRPCWriteTimeout       = "api.rpc-write-timeout"
	FlagRPCMaxBodyBytes       = "api.rpc-max-body-bytes"
	FlagAPIEnableUnsafeCORS   = "api.enabled-unsafe-cors"

	// gRPC-related flags

	// mempool flags
	FlagMempoolMaxTxs = "mempool.max-txs"

	// testnet keys
	KeyIsTestnet             = "is-testnet"
	KeyNewChainID            = "new-chain-ID"
	KeyNewOpAddr             = "new-operator-addr"
	KeyNewValAddr            = "new-validator-addr"
	KeyUserPubKey            = "user-pub-key"
	KeyTriggerTestnetUpgrade = "trigger-testnet-upgrade"
)

// addStartNodeFlags should be added to any CLI commands that start the network.
func addStartNodeFlags(cmd *cobra.Command) {
	cmd.Flags().Bool(flagWithComet, true, "Run abci app embedded in-process with CometBFT")
	cmd.Flags().String(flagAddress, "tcp://127.0.0.1:26658", "Listen address")
	cmd.Flags().String(flagTransport, "socket", "Transport protocol: socket, grpc")
	cmd.Flags().String(flagTraceStore, "", "Enable KVStore tracing to an output file")
	cmd.Flags().String(FlagMinGasPrices, "", "Minimum gas prices to accept for transactions; Any fee in a tx must meet this minimum (e.g. 0.01photino;0.0001stake)")
	cmd.Flags().Uint64(FlagQueryGasLimit, 0, "Maximum gas a Rest/Grpc query can consume. Blank and 0 imply unbounded.")
	cmd.Flags().IntSlice(FlagUnsafeSkipUpgrades, []int{}, "Skip a set of upgrade heights to continue the old binary")
	cmd.Flags().Uint64(FlagHaltHeight, 0, "Block height at which to gracefully halt the chain and shutdown the node")
	cmd.Flags().Uint64(FlagHaltTime, 0, "Minimum block time (in Unix seconds) at which to gracefully halt the chain and shutdown the node")
	cmd.Flags().Bool(FlagInterBlockCache, true, "Enable inter-block caching")
	cmd.Flags().String(flagCPUProfile, "", "Enable CPU profiling and write to the provided file")
	cmd.Flags().Bool(FlagTrace, false, "Provide full stack traces for errors in ABCI Log")
	cmd.Flags().String(FlagPruning, pruningtypes.PruningOptionDefault, "Pruning strategy (default|nothing|everything|custom)")
	cmd.Flags().Uint64(FlagPruningKeepRecent, 0, "Number of recent heights to keep on disk (ignored if pruning is not 'custom')")
	cmd.Flags().Uint64(FlagPruningInterval, 0, "Height interval at which pruned heights are removed from disk (ignored if pruning is not 'custom')")
	cmd.Flags().Uint(FlagInvCheckPeriod, 0, "Assert registered invariants every N blocks")
	cmd.Flags().Uint64(FlagMinRetainBlocks, 0, "Minimum block height offset during ABCI commit to prune CometBFT blocks")
	cmd.Flags().Bool(FlagAPIEnable, false, "Define if the API server should be enabled")
	cmd.Flags().Bool(FlagAPISwagger, false, "Define if swagger documentation should automatically be registered (Note: the API must also be enabled)")
	cmd.Flags().String(FlagAPIAddress, serverconfig.DefaultAPIAddress, "the API server address to listen on")
	cmd.Flags().Uint(FlagAPIMaxOpenConnections, 1000, "Define the number of maximum open connections")
	cmd.Flags().Uint(FlagRPCReadTimeout, 10, "Define the CometBFT RPC read timeout (in seconds)")
	cmd.Flags().Uint(FlagRPCWriteTimeout, 0, "Define the CometBFT RPC write timeout (in seconds)")
	cmd.Flags().Uint(FlagRPCMaxBodyBytes, 1000000, "Define the CometBFT maximum request body (in bytes)")
	cmd.Flags().Bool(FlagAPIEnableUnsafeCORS, false, "Define if CORS should be enabled (unsafe - use it at your own risk)")
	cmd.Flags().Bool(flagGRPCOnly, false, "Start the node in gRPC query only mode (no CometBFT process is started)")
	cmd.Flags().Bool(flagGRPCEnable, true, "Define if the gRPC server should be enabled")
	cmd.Flags().String(flagGRPCAddress, serverconfig.DefaultGRPCAddress, "the gRPC server address to listen on")
	cmd.Flags().Bool(flagGRPCWebEnable, true, "Define if the gRPC-Web server should be enabled. (Note: gRPC must also be enabled)")
	cmd.Flags().Uint64(FlagStateSyncSnapshotInterval, 0, "State sync snapshot interval")
	cmd.Flags().Uint32(FlagStateSyncSnapshotKeepRecent, 2, "State sync snapshot to keep")
	cmd.Flags().Bool(FlagDisableIAVLFastNode, false, "Disable fast node for IAVL tree")
	// cmd.Flags().Int(FlagMempoolMaxTxs, mempool.DefaultMaxTx, "Sets MaxTx value for the app-side mempool")
	cmd.Flags().Duration(FlagShutdownGrace, 0*time.Second, "On Shutdown, duration to wait for resource clean up")

	// support old flags name for backwards compatibility
	cmd.Flags().SetNormalizeFunc(func(f *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "with-tendermint" {
			name = flagWithComet
		}

		return pflag.NormalizedName(name)
	})

	// add support for all CometBFT-specific command line options
	cmtcmd.AddNodeFlags(cmd)

	// if opts.AddFlags != nil {
	// 	opts.AddFlags(cmd)
	// }
}
