package flags

import (
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/pkg/consts"
)

// List of CLI flags
const FlagSquareSize = "square-size"

// AddTxFlagsToCmd adds common flags to a module tx command.
func AddTxFlagsToCmd(cmd *cobra.Command) {
	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().Uint64(FlagSquareSize, consts.MaxSquareSize, "Specify the square size")
}