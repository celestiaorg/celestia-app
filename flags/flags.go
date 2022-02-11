package flags

import (
	"github.com/cosmos/cosmos-sdk/client/flags"

	"github.com/spf13/cobra"
	"github.com/tendermint/tendermint/pkg/consts"
)

// List of CLI flags
const FlagSquareSizes = "square-sizes"

// AddTxFlagsToCmd adds common flags to a module tx command.
func AddTxFlagsToCmd(cmd *cobra.Command) {
	flags.AddTxFlagsToCmd(cmd)
	cmd.Flags().UintSlice(FlagSquareSizes, []uint{consts.MaxSquareSize, 128, 64}, "Specify the square sizes, must be power of 2")
}