package app

import (
	"github.com/cosmos/cosmos-sdk/server"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
)

type KVAppOptions struct {
	options map[string]interface{}
}

func NewKVAppOptions() *KVAppOptions {
	return &KVAppOptions{options: make(map[string]interface{})}
}

// Get implements AppOptions
func (ao *KVAppOptions) Get(o string) interface{} {
	return ao.options[o]
}

// Set adds an option to the KVAppOptions
func (ao *KVAppOptions) Set(o string, v interface{}) {
	ao.options[o] = v
}

// SetMany adds an option to the KVAppOptions
func (ao *KVAppOptions) SetMany(o map[string]interface{}) {
	for k, v := range o {
		ao.Set(k, v)
	}
}

func (ao *KVAppOptions) SetFromAppConfig(appCfg *srvconfig.Config) {
	opts := map[string]interface{}{
		server.FlagPruning:                     appCfg.Pruning,
		server.FlagPruningKeepRecent:           appCfg.PruningKeepRecent,
		server.FlagPruningInterval:             appCfg.PruningInterval,
		server.FlagMinGasPrices:                appCfg.MinGasPrices,
		server.FlagMinRetainBlocks:             appCfg.MinRetainBlocks,
		server.FlagIndexEvents:                 appCfg.IndexEvents,
		server.FlagStateSyncSnapshotInterval:   appCfg.StateSync.SnapshotInterval,
		server.FlagStateSyncSnapshotKeepRecent: appCfg.StateSync.SnapshotKeepRecent,
		server.FlagHaltHeight:                  appCfg.HaltHeight,
		server.FlagHaltTime:                    appCfg.HaltTime,
	}
	ao.SetMany(opts)
}

// DefaultAppOptions returns the default application options. The options are
// set using the default app config. If the app config is set after this, it
// will overwrite these values.
func DefaultAppOptions() *KVAppOptions {
	opts := NewKVAppOptions()
	opts.SetFromAppConfig(DefaultAppConfig())
	return opts
}
