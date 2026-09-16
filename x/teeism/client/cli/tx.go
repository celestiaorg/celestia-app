package cli

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v10/x/teeism/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/version"
	"github.com/spf13/cobra"
)

// ismSpec is the JSON an ISM is created from.
//
// A file rather than positional arguments: an enclave identity is five
// measurements, and the tooling that reads them off a running CVM can write this
// file directly.
type ismSpec struct {
	State             string `json:"state"`
	MerkleTreeAddress string `json:"merkle_tree_address"`
	Identity          struct {
		MrTd        string `json:"mr_td"`
		OsImageHash string `json:"os_image_hash"`
		ComposeHash string `json:"compose_hash"`
		MrKms       string `json:"mr_kms"`
		KeyProvider string `json:"key_provider"`
	} `json:"identity"`
}

// attestationSpec is the JSON an attestation is submitted from, as the enclave
// and coprocessor produce it.
type attestationSpec struct {
	Quote      string `json:"quote"`
	EventLog   string `json:"event_log"`
	Payload    string `json:"payload"`
	Collateral struct {
		PckCrl                string `json:"pck_crl"`
		PckCrlIssuerChain     string `json:"pck_crl_issuer_chain"`
		TcbInfo               string `json:"tcb_info"`
		TcbInfoIssuerChain    string `json:"tcb_info_issuer_chain"`
		QeIdentity            string `json:"qe_identity"`
		QeIdentityIssuerChain string `json:"qe_identity_issuer_chain"`
		RootCaCrl             string `json:"root_ca_crl"`
	} `json:"collateral"`
}

// NewCreateInterchainSecurityModuleCmd creates and returns the tee ism creation cmd.
func NewCreateInterchainSecurityModuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [spec-file]",
		Short: "Create a Hyperlane TEE ism",
		Long: strings.TrimSpace(`Create a Hyperlane TEE interchain security module (ISM).

The spec file is JSON holding the initial trusted state, the origin merkle tree
hook address, and the measurements of the enclave permitted to advance the ism.
Every value is hex, with or without a 0x prefix.

  {
    "state": "0x...",
    "merkle_tree_address": "0x...",
    "identity": {
      "mr_td": "0x...",
      "os_image_hash": "0x...",
      "compose_hash": "0x...",
      "mr_kms": "0x...",
      "key_provider": "0x..."
    }
  }`),
		Example: fmt.Sprintf("%s tx %s create ./ism.json --from mykey", version.AppName, types.ModuleName),
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			var spec ismSpec
			if err := readJSONFile(args[0], &spec); err != nil {
				return err
			}

			state, err := decodeHexString(spec.State)
			if err != nil {
				return fmt.Errorf("state: %w", err)
			}
			merkleTreeAddress, err := decodeHexString(spec.MerkleTreeAddress)
			if err != nil {
				return fmt.Errorf("merkle_tree_address: %w", err)
			}

			identity := &types.EnclaveIdentity{}
			for _, f := range []struct {
				name string
				src  string
				dst  *[]byte
			}{
				{"mr_td", spec.Identity.MrTd, &identity.MrTd},
				{"os_image_hash", spec.Identity.OsImageHash, &identity.OsImageHash},
				{"compose_hash", spec.Identity.ComposeHash, &identity.ComposeHash},
				{"mr_kms", spec.Identity.MrKms, &identity.MrKms},
				{"key_provider", spec.Identity.KeyProvider, &identity.KeyProvider},
			} {
				v, err := decodeHexString(f.src)
				if err != nil {
					return fmt.Errorf("identity.%s: %w", f.name, err)
				}
				*f.dst = v
			}

			msg := types.MsgCreateInterchainSecurityModule{
				Creator:           clientCtx.GetFromAddress().String(),
				State:             state,
				MerkleTreeAddress: merkleTreeAddress,
				Identity:          identity,
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

// NewSubmitAttestationCmd creates and returns the attestation submission cmd.
func NewSubmitAttestationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "submit-attestation [ism-id] [attestation-file]",
		Short: "Advance a TEE ism and authorize its message batch",
		Long: strings.TrimSpace(`Submit an enclave attestation to a Hyperlane TEE ism.

One attestation advances the trusted state and authorizes the messages it covers,
so this is the only transaction a relayer sends per batch.

The attestation file is JSON holding hex-encoded fields:

  {
    "quote": "0x...",
    "event_log": "0x...",
    "payload": "0x...",
    "collateral": {
      "pck_crl": "0x...",
      "pck_crl_issuer_chain": "0x...",
      "tcb_info": "0x...",
      "tcb_info_issuer_chain": "0x...",
      "qe_identity": "0x...",
      "qe_identity_issuer_chain": "0x...",
      "root_ca_crl": "0x..."
    }
  }`),
		Example: fmt.Sprintf("%s tx %s submit-attestation 0x...2b ./attestation.json --from mykey", version.AppName, types.ModuleName),
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

			var spec attestationSpec
			if err := readJSONFile(args[1], &spec); err != nil {
				return err
			}

			collateral := &types.Collateral{}
			for _, f := range []struct {
				name string
				src  string
				dst  *[]byte
			}{
				{"pck_crl", spec.Collateral.PckCrl, &collateral.PckCrl},
				{"pck_crl_issuer_chain", spec.Collateral.PckCrlIssuerChain, &collateral.PckCrlIssuerChain},
				{"tcb_info", spec.Collateral.TcbInfo, &collateral.TcbInfo},
				{"tcb_info_issuer_chain", spec.Collateral.TcbInfoIssuerChain, &collateral.TcbInfoIssuerChain},
				{"qe_identity", spec.Collateral.QeIdentity, &collateral.QeIdentity},
				{"qe_identity_issuer_chain", spec.Collateral.QeIdentityIssuerChain, &collateral.QeIdentityIssuerChain},
				{"root_ca_crl", spec.Collateral.RootCaCrl, &collateral.RootCaCrl},
			} {
				v, err := decodeHexString(f.src)
				if err != nil {
					return fmt.Errorf("collateral.%s: %w", f.name, err)
				}
				*f.dst = v
			}

			quote, err := decodeHexString(spec.Quote)
			if err != nil {
				return fmt.Errorf("quote: %w", err)
			}
			eventLog, err := decodeHexString(spec.EventLog)
			if err != nil {
				return fmt.Errorf("event_log: %w", err)
			}
			payload, err := decodeHexString(spec.Payload)
			if err != nil {
				return fmt.Errorf("payload: %w", err)
			}

			msg := types.MsgSubmitAttestation{
				Id:         ismId,
				Quote:      quote,
				EventLog:   eventLog,
				Collateral: collateral,
				Payload:    payload,
				Signer:     clientCtx.GetFromAddress().String(),
			}

			return tx.GenerateOrBroadcastTxCLI(clientCtx, cmd.Flags(), &msg)
		},
	}

	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func readJSONFile(path string, out any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}

func decodeHexString(s string) ([]byte, error) {
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}
