package cli

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"
)

// NewCreateZKExecutionIsmCmd creates and returns the zk ism creation cmd.
func NewCreateZKExecutionIsmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [state-transition-key-hex] [state-membership-key-hex]",
		Short: "Create a new ZK Execution ISM for use with the Hyperlane messaging protocol.",
		Long: strings.TrimSpace(`Create a new ZK Execution Interchain Security Module (ISM) for use with the Hyperlane messaging protocol.
This CLI command requires both the StateTransitionVkey and the StateMembershipVkey to be provided as hex-encoded byte string arguments.`),
		Example: fmt.Sprintf("%s tx %s create 3f8a8f3be3cd62e2f9b742de9e4b2c1f5a62a7e0e52a29b4bb4d7a6a2fcaf9c2 2c9fafc2a6a7d4bb4b92a2e5e0a7625a1f2c4b9ede42b7f9e262cde3b8f8a3f", version.AppName, types.ModuleName),
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			creator := clientCtx.GetFromAddress().String()
			stateTransitionVerKey, err := hex.DecodeString(args[0])
			if err != nil {
				return err
			}

			stateMembershipVerKey, err := hex.DecodeString(args[1])
			if err != nil {
				return err
			}

			// TODO: fill in the remaining fields for the CLI cmd
			msg := types.MsgCreateZKExecutionISM{
				Creator:             creator,
				StateTransitionVkey: stateTransitionVerKey,
				StateMembershipVkey: stateMembershipVerKey,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
