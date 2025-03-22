package testnode

import (
	pruningtypes "github.com/cosmos/cosmos-sdk/pruning/types"
	"github.com/cosmos/cosmos-sdk/server"
)

// KVAppOptions implements the AppOptions interface backed by a simple key-value map
type KVAppOptions struct {
	options map[string]any
}

// Get returns the option value for the given option key.
func (ao *KVAppOptions) Get(option string) any {
	return ao.options[option]
}

// Set sets a key-value app option.
func (ao *KVAppOptions) Set(option string, value any) {
	ao.options[option] = value
}

// DefaultAppOptions returns the default application options.
func DefaultAppOptions() *KVAppOptions {
	opts := &KVAppOptions{options: make(map[string]any)}
	opts.Set(server.FlagPruning, pruningtypes.PruningOptionNothing)
	return opts
}

// NewKVAppOptions creates a new instance of KVAppOptions
func NewKVAppOptions() *KVAppOptions {
	opts := &KVAppOptions{options: make(map[string]any)}
	return opts
}
