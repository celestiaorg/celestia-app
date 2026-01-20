package cli

import (
	"encoding/hex"
	"fmt"
	"os"
	"strings"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v6/x/zkism/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"
)

// NewCreateInterchainSecurityModuleCmd creates and returns the zk ism creation cmd.
func NewCreateInterchainSecurityModuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [state-hex] [merkle-tree-address-hex] [groth16-vkey-file] [state-transition-key-hex] [state-membership-key-hex]",
		Short: "Create a Hyperlane zk ism",
		Long: strings.TrimSpace(`Create a Hyperlane zk interchain security module (ISM) for use with the Hyperlane messaging protocol.
The command registers the initial trusted state, Merkle tree address, and verifier configuration.

Arguments:
  [state-hex]                 Hex-encoded initial trusted state bytes.
  [merkle-tree-address-hex]   Hex-encoded 32-byte Merkle tree address.
  [groth16-vkey-file]         Path to the Groth16 verifier key file (binary bytes).
  [state-transition-key-hex]  Hex-encoded 32-byte commitment to the state transition verifier key.
  [state-membership-key-hex]  Hex-encoded 32-byte commitment to the state membership verifier key.`),
		Example: fmt.Sprintf("%s tx %s create 0xdead...beef 0xdead...beef ./groth16.vkey 3f8a8f3be3cd62e2f9b742de9e4b2c1f5a62a7e0e52a29b4bb4d7a6a2fcaf9c2 2c9fafc2a6a7d4bb4b92a2e5e0a7625a1f2c4b9ede42b7f9e262cde3b8f8a3f", version.AppName, types.ModuleName),
		Args:    cobra.ExactArgs(5),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			state, err := decodeHexString(args[0])
			if err != nil {
				return err
			}

			merkleTreeAddress, err := decodeHexString(args[1])
			if err != nil {
				return err
			}

			groth16Vkey, err := os.ReadFile(args[2])
			if err != nil {
				return err
			}

			stateTransitionVerKey, err := decodeHexString(args[3])
			if err != nil {
				return err
			}

			stateMembershipVerKey, err := decodeHexString(args[4])
			if err != nil {
				return err
			}

			msg := types.MsgCreateInterchainSecurityModule{
				Creator:             clientCtx.GetFromAddress().String(),
				State:               state,
				MerkleTreeAddress:   merkleTreeAddress,
				Groth16Vkey:         groth16Vkey,
				StateTransitionVkey: stateTransitionVerKey,
				StateMembershipVkey: stateMembershipVerKey,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewUpdateInterchainSecurityModuleCmd creates and returns the zk ism update cmd.
func NewUpdateInterchainSecurityModuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update [ism-id] [proof-hex] [public-values-hex]",
		Short: "Update a Hyperlane zk ism",
		Long: strings.TrimSpace(`Update a Hyperlane zk interchain security module (ISM) with a new trusted state.
The command submits a Groth16 state transition proof plus its public values to advance the ISM's state.

Arguments:
  [ism-id]             Hex-encoded 32-byte ISM identifier (0x-prefixed is accepted).
  [proof-hex]          Hex-encoded SP1 Groth16 proof bytes (260 bytes: 4-byte prefix + 256-byte proof).
  [public-values-hex]  Hex-encoded bincode-serialized StateTransitionValues (state || new_state with length prefixes).`),
		Example: fmt.Sprintf("%s tx %s update 0x726f757465725f69736d000000000000000000000000002a0000000000000000 0xdead...beef 0xdead...beef", version.AppName, types.ModuleName),
		Args:    cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			ismID, err := util.DecodeHexAddress(args[0])
			if err != nil {
				return fmt.Errorf("invalid ism identifier: %w", err)
			}

			proof, err := decodeHexString(args[1])
			if err != nil {
				return fmt.Errorf("invalid proof: %w", err)
			}

			publicValues, err := decodeHexString(args[2])
			if err != nil {
				return fmt.Errorf("invalid public values: %w", err)
			}

			msg := types.MsgUpdateInterchainSecurityModule{
				Id:           ismID,
				Proof:        proof,
				PublicValues: publicValues,
				Signer:       clientCtx.GetFromAddress().String(),
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSubmitMessagesCmd creates and returns the submit messages cmd.
func NewSubmitMessagesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit-messages [ism-id] [proof-hex] [public-values-hex]",
		Short: "Submit a batched Hyperlane message proof for an ISM",
		Long: strings.TrimSpace(`Submit a Hyperlane zk membership proof to authorize message IDs for processing.
The command submits a Groth16 state membership proof and public values against an existing ISM.

Arguments:
  [ism-id]             Hex-encoded 32-byte ISM identifier (0x-prefixed is accepted).
  [proof-hex]          Hex-encoded SP1 Groth16 proof bytes (260 bytes: 4-byte prefix + 256-byte proof).
  [public-values-hex]  Hex-encoded bincode-serialized StateMembershipValues (state_root || merkle_tree_address || message_ids).`),
		Example: fmt.Sprintf("%s tx %s submit-messages 0x726f757465725f69736d000000000000000000000000002a0000000000000000 0xdead...beef 0xdead...beef", version.AppName, types.ModuleName),
		Args:    cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			ismID, err := util.DecodeHexAddress(args[0])
			if err != nil {
				return fmt.Errorf("invalid ism identifier: %w", err)
			}

			proof, err := decodeHexString(args[1])
			if err != nil {
				return fmt.Errorf("invalid proof: %w", err)
			}

			publicValues, err := decodeHexString(args[2])
			if err != nil {
				return fmt.Errorf("invalid public values: %w", err)
			}

			msg := types.MsgSubmitMessages{
				Id:           ismID,
				Proof:        proof,
				PublicValues: publicValues,
				Signer:       clientCtx.GetFromAddress().String(),
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func decodeHexString(input string) ([]byte, error) {
	input = strings.TrimPrefix(input, "0x")
	return hex.DecodeString(input)
}
