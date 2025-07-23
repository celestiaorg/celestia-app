package docker_e2e

import (
	"context"
	"fmt"

	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
)

// default values used when caller doesn't override with With* methods.
const (
	defaultHomeDir        = "/var/cosmos-chain/celestia"
	defaultFees           = "200000utia"
	defaultKeyringBackend = "test"
)

// CommandBuilder constructs celestia-appd CLI invocations in a fluent, readable way.
//
// Usage example:
//
//	cmd, _ := NewCommandBuilder(ctx, node, []string{"tx", "bank", "send", from, to, "100utia"}).
//	    WithFees("10000utia").WithYes(true).Build()
//	stdout, stderr, err := node.Exec(ctx, cmd, nil)
//
// The builder auto-fills sensible defaults (home dir, node RPC address, chain-id, etc.)
// so tests don’t need to repeat them everywhere. Optional setters let callers override
// any default without dealing with flag presence/duplication logic.
type CommandBuilder struct {
	// required
	ctx       context.Context
	chainNode tastoratypes.ChainNode
	cmd       []string

	// optional / overridable via With* (all have sensible defaults)
	homeDir        string
	chainID        string
	nodeRPC        string
	fees           string
	keyringBackend string
	yesFlag        bool
}

// NewCommandBuilder returns a builder with sensible defaults populated.
// Only the context, node and the sub-command (e.g. ["tx" "bank" ...]) are required.
func NewCommandBuilder(ctx context.Context, chainNode tastoratypes.ChainNode, cmd []string) *CommandBuilder {
	return &CommandBuilder{
		ctx:            ctx,
		chainNode:      chainNode,
		cmd:            stripAppdPrefix(cmd),
		homeDir:        defaultHomeDir,
		chainID:        appconsts.TestChainID,
		fees:           defaultFees,
		keyringBackend: defaultKeyringBackend,
		yesFlag:        true, // default to non-interactive for tx commands
	}
}

// WithHome overrides the default --home flag directory.
func (b *CommandBuilder) WithHome(home string) *CommandBuilder {
	b.homeDir = home
	return b
}

// WithChainID overrides the default --chain-id flag.
func (b *CommandBuilder) WithChainID(id string) *CommandBuilder {
	b.chainID = id
	return b
}

// WithNodeAddress sets a custom RPC address for --node (e.g. tcp://host:26657).
func (b *CommandBuilder) WithNodeAddress(addr string) *CommandBuilder {
	b.nodeRPC = addr
	return b
}

// WithFees overrides the default fee for tx commands.
func (b *CommandBuilder) WithFees(fees string) *CommandBuilder {
	b.fees = fees
	return b
}

// WithKeyringBackend sets the keyring backend for tx commands.
func (b *CommandBuilder) WithKeyringBackend(backend string) *CommandBuilder {
	b.keyringBackend = backend
	return b
}

// WithYes toggles the automatic --yes flag for tx commands.
func (b *CommandBuilder) WithYes(yes bool) *CommandBuilder {
	b.yesFlag = yes
	return b
}

// Build assembles the final argument slice ready to be passed to ChainNode.Exec.
func (b *CommandBuilder) Build() ([]string, error) {
	if len(b.cmd) == 0 {
		return nil, fmt.Errorf("command cannot be empty")
	}

	isKeys := b.cmd[0] == "keys"
	isTx := b.cmd[0] == "tx"

	// -------------------- resolve defaults up-front --------------------
	// nodeRPC is only needed for non-keys commands.
	nodeRPC := b.nodeRPC
	if nodeRPC == "" && !isKeys && b.chainNode != nil {
		host, err := b.chainNode.GetInternalHostName(b.ctx)
		if err != nil {
			return nil, err
		}
		nodeRPC = fmt.Sprintf("tcp://%s:26657", host)
	}

	// -------------------- assemble args in a flat pass -----------------
	args := append([]string{"celestia-appd"}, b.cmd...)

	// universal flag
	args = append(args, "--home", b.homeDir)

	// non-keys flags
	if !isKeys {
		if nodeRPC != "" {
			args = append(args, "--node", nodeRPC)
		}
		args = append(args, "--chain-id", b.chainID)
	}

	if !isTx {
		return args, nil
	}

	// tx-specific flags
	args = append(args, "--fees", b.fees)
	args = append(args, "--keyring-backend", b.keyringBackend)
	if b.yesFlag {
		args = append(args, "--yes")
	}
	return args, nil
}

// stripAppdPrefix removes an optional leading "celestia-appd" from the cmd slice so
// callers can pass either the full command or just the sub-command portion.
func stripAppdPrefix(cmd []string) []string {
	if len(cmd) > 0 && cmd[0] == "celestia-appd" {
		return cmd[1:]
	}
	return cmd
}
