package cli

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the CLI query commands for this module
func GetQueryCmd() *cobra.Command {
	// Group blob queries under a subcommand
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdQueryParams())

	return cmd
}
