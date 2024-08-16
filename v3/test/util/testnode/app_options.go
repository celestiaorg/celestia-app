package testnode

import (
	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/server"
)

type KVAppOptions struct {
	options map[string]interface{}
}

// Get returns the option value for the given option key.
func (ao *KVAppOptions) Get(option string) interface{} {
	return ao.options[option]
}

// Set sets a key-value app option.
func (ao *KVAppOptions) Set(option string, value interface{}) {
	ao.options[option] = value
}

// DefaultAppOptions returns the default application options.
func DefaultAppOptions() *KVAppOptions {
	opts := &KVAppOptions{options: make(map[string]interface{})}
	opts.Set(server.FlagPruning, pruningtypes.PruningOptionNothing)
	return opts
}
