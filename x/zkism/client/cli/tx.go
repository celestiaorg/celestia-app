package cli

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"

	"github.com/celestiaorg/celestia-app/v4/x/zkism/types"
)

// NewCreateZKExecutionIsmCmd creates and returns the zk ism creation cmd.
func NewCreateZKExecutionIsmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [state-transition-key-hex] [state-membership-key-hex]",
		Short: "Create a new ZK Execution ISM for use with the Hyperlane messaging protocol.",
		Long: strings.TrimSpace(`Create a new ZK Execution Interchain Security Module (ISM) for use with the Hyperlane messaging protocol.
This CLI command requires both the StateTransitionVerifierKey and the StateMembershipVerifierKey to be provided a hex-encoded byte string arguments.`),
		Example: fmt.Sprintf("%s tx %s create 4f2c7b...3f d18e6c7...3a", version.AppName, types.ModuleName),
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

			msg := types.MsgCreateZKExecutionISM{
				Creator:                    creator,
				StateTransitionVerifierKey: stateTransitionVerKey,
				StateMembershipVerifierKey: stateMembershipVerKey,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
