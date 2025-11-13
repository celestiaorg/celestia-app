package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"

	hyperlanecli "github.com/bcp-innovations/hyperlane-cosmos/x/warp/client/cli"
	"github.com/celestiaorg/celestia-app/v6/x/warp/types"
)

// GetTxCmd returns the transaction commands for the Celestia warp module.
// It extends the base hyperlane warp commands with Celestia-specific commands.
func GetTxCmd() *cobra.Command {
	// Get the base hyperlane warp tx commands
	cmd := hyperlanecli.GetTxCmd()

	// Add Celestia-specific commands
	cmd.AddCommand(CmdSetupPermissionlessInfrastructure())

	return cmd
}

// CmdSetupPermissionlessInfrastructure returns a CLI command to setup permissionless infrastructure.
func CmdSetupPermissionlessInfrastructure() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "setup-permissionless-infrastructure",
		Short: "Setup permissionless Hyperlane infrastructure (mailbox, routing ISM, collateral token)",
		Long: `Setup permissionless Hyperlane infrastructure owned by the warp module.

This command creates:
- Mailbox (owned by warp module)
- Routing ISM (owned by warp module)
- Collateral token for the origin denomination

The infrastructure is created with ownership assigned to the warp module address,
allowing anyone to permissionlessly enroll routes and create synthetic tokens.

Modes:
- create: Create new infrastructure if it doesn't exist
- use:    Use existing infrastructure (validates it exists)

Example:
  celestia-appd tx warp setup-permissionless-infrastructure \
    --mode create \
    --local-domain 69420 \
    --origin-denom utia \
    --from mykey
`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			mode, err := cmd.Flags().GetString("mode")
			if err != nil {
				return fmt.Errorf("failed to get mode flag: %w", err)
			}

			if mode != "create" && mode != "use" {
				return fmt.Errorf("invalid mode: %s (must be 'create' or 'use')", mode)
			}

			localDomain, err := cmd.Flags().GetUint32("local-domain")
			if err != nil {
				return fmt.Errorf("failed to get local-domain flag: %w", err)
			}

			originDenom, err := cmd.Flags().GetString("origin-denom")
			if err != nil {
				return fmt.Errorf("failed to get origin-denom flag: %w", err)
			}

			msg := types.MsgSetupPermissionlessInfrastructure{
				Creator:      clientCtx.GetFromAddress().String(),
				Mode:         mode,
				LocalDomain:  localDomain,
				OriginDenom:  originDenom,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	cmd.Flags().String("mode", "create", "Mode: 'create' to create new infrastructure, 'use' to use existing")
	cmd.Flags().Uint32("local-domain", 0, "Local domain ID for this chain")
	cmd.Flags().String("origin-denom", "", "Origin denomination for the collateral token (e.g., utia)")

	flags.AddTxFlagsToCmd(cmd)

	return cmd
}
