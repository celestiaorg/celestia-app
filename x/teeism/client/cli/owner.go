package cli

import (
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/x/teeism/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"
)

const (
	flagTrustedRootCert = "trusted-root-cert"
	flagClear           = "clear"
)

// NewUpdateOwnerCmd creates and returns the cmd that transfers an ism to a new owner.
func NewUpdateOwnerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update-owner [ism-id] [new-owner]",
		Short: "Transfer a TEE ism to a new owner (owner only)",
		Long: strings.TrimSpace(`Hand a Hyperlane TEE ism to a new owner, typically a multisig.

The new owner gets every owner control: re-pinning the enclave
(update-identity) and overriding Intel's side of verification
(set-verifier-overrides). The current owner keeps none of them. There is no
renouncing: an ism always has an owner who can act in an emergency.`),
		Example: fmt.Sprintf("%s tx %s update-owner 0x...2b celestia1... --from owner", version.AppName, types.ModuleName),
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			ismId, err := util.DecodeHexAddress(args[0])
			if err != nil {
				return err
			}
			msg := types.MsgUpdateOwner{
				Owner:    clientCtx.GetFromAddress().String(),
				Id:       ismId,
				NewOwner: args[1],
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSetVerifierOverridesCmd creates and returns the cmd that sets or clears an
// ism's overrides of Intel's side of verification.
func NewSetVerifierOverridesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "set-verifier-overrides [ism-id]",
		Short: "Update the Intel root CA a TEE ism verifies against (owner only)",
		Long: strings.TrimSpace(`Set or clear the Intel root CA an ism's quotes must chain to, for when Intel
rotates its root while the ism is live. Verification is otherwise unchanged.

Each call replaces the previous setting. --clear goes back to the root built
into the verifier. Every change emits EventSetVerifierOverrides.

  --trusted-root-cert  Intel root CA certificate file (PEM or DER) to trust
                       instead of the embedded one. Repeat for several.`),
		Example: strings.Join([]string{
			fmt.Sprintf("%s tx %s set-verifier-overrides 0x...2b --trusted-root-cert ./Intel_SGX_RootCA.cer --from owner", version.AppName, types.ModuleName),
			fmt.Sprintf("%s tx %s set-verifier-overrides 0x...2b --clear --from owner", version.AppName, types.ModuleName),
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			ismId, err := util.DecodeHexAddress(args[0])
			if err != nil {
				return err
			}
			overrides, err := overridesFromFlags(cmd)
			if err != nil {
				return err
			}
			msg := types.MsgSetVerifierOverrides{
				Owner:     clientCtx.GetFromAddress().String(),
				Id:        ismId,
				Overrides: overrides,
			}
			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	cmd.Flags().StringArray(flagTrustedRootCert, nil, "root CA certificate file (PEM or DER) to trust instead of the embedded Intel root; repeatable")
	cmd.Flags().Bool(flagClear, false, "go back to the root built into the verifier")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func overridesFromFlags(cmd *cobra.Command) (*types.VerifierOverrides, error) {
	clear, err := cmd.Flags().GetBool(flagClear)
	if err != nil {
		return nil, err
	}
	rootFiles, err := cmd.Flags().GetStringArray(flagTrustedRootCert)
	if err != nil {
		return nil, err
	}

	if clear {
		if len(rootFiles) > 0 {
			return nil, fmt.Errorf("--%s cannot be combined with --%s", flagClear, flagTrustedRootCert)
		}
		return nil, nil
	}
	if len(rootFiles) == 0 {
		return nil, fmt.Errorf("set --%s, or --%s to go back to the built-in root", flagTrustedRootCert, flagClear)
	}

	overrides := &types.VerifierOverrides{}
	for _, path := range rootFiles {
		der, err := readCertificate(path)
		if err != nil {
			return nil, err
		}
		overrides.TrustedRootCerts = append(overrides.TrustedRootCerts, der)
	}
	return overrides, overrides.Validate()
}

// readCertificate reads a certificate file in either PEM or DER form, as Intel
// publishes its root CA as DER (.cer) and most tooling writes PEM.
func readCertificate(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if block, _ := pem.Decode(raw); block != nil {
		if block.Type != "CERTIFICATE" {
			return nil, fmt.Errorf("%s: expected a CERTIFICATE PEM block, got %s", path, block.Type)
		}
		return block.Bytes, nil
	}
	return raw, nil
}
