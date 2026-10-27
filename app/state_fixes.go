package app

import (
	"fmt"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// StateFix repairs committed state at one height of one chain. It is an
// emergency tool for a chain that halted on bad state: validators restart on a
// release that lists the fix, and every node that executes Height runs it.
type StateFix struct {
	// ChainID is the chain the fix applies to.
	ChainID string
	// Height is the block height at which the fix runs, before BeginBlock.
	Height int64
	// Name identifies the fix in logs and errors.
	Name string
	// Apply repairs the state. It must be deterministic. An error halts the
	// chain at Height.
	Apply func(ctx sdk.Context, app *App) error
}

// stateFixes lists the state fixes of this release. A fix must stay in every
// later patch of the release line that executes its height.
var stateFixes = []StateFix{}

// applyStateFixes runs the fixes that match the block's chain-id and height.
// The chain-id comes from the block header that consensus validated, not from
// the node's local config.
func (app *App) applyStateFixes(ctx sdk.Context, req *abci.RequestFinalizeBlock) error {
	for _, fix := range app.stateFixes {
		if fix.Height != req.Height {
			continue
		}
		if req.Header == nil {
			return fmt.Errorf("state fix %s at height %d: block header is missing", fix.Name, fix.Height)
		}
		if fix.ChainID != req.Header.ChainID {
			continue
		}
		if err := fix.Apply(ctx, app); err != nil {
			return fmt.Errorf("state fix %s at height %d: %w", fix.Name, fix.Height, err)
		}
		app.Logger().Info("applied state fix", "name", fix.Name, "height", fix.Height)
	}
	return nil
}
