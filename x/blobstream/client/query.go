package client

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	"github.com/spf13/cobra"
)

// GetQueryCmd returns the CLI query commands for this module
func GetQueryCmd() *cobra.Command {
	// Group Blobstream queries under a subcommand
	cmd := &cobra.Command{
		Use:                        types.ModuleName,
		Short:                      fmt.Sprintf("Querying commands for the %s module", types.ModuleName),
		DisableFlagParsing:         true,
		SuggestionsMinimumDistance: 2,
		RunE:                       client.ValidateCmd,
	}

	cmd.AddCommand(CmdQueryAttestationByNonce(), CmdQueryEVMAddress())

	return cmd
}

func CmdQueryAttestationByNonce() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "attestation <nonce>",
		Aliases: []string{"att"},
		Short:   "query an attestation by nonce",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			queryClient := types.NewQueryClient(clientCtx)

			nonce, err := strconv.ParseUint(args[0], 10, 0)
			if err != nil {
				return err
			}
			res, err := queryClient.AttestationRequestByNonce(
				cmd.Context(),
				&types.QueryAttestationRequestByNonceRequest{Nonce: nonce},
			)
			if err != nil {
				return err
			}
			if res.Attestation == nil {
				return types.ErrNilAttestation
			}
			att, err := unmarshallAttestation(res.Attestation)
			if err != nil {
				return err
			}

			switch att.(type) {
			case *types.Valset, *types.DataCommitment:
				jsonDC, err := json.Marshal(att)
				if err != nil {
					return err
				}
				return clientCtx.PrintString(string(jsonDC))
			default:
				return types.ErrUnknownAttestationType
			}
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func CmdQueryEVMAddress() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "evm <validator_valoper_address>",
		Short: "query the evm address corresponding to a validator bech32 valoper address",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx := client.GetClientContextFromCmd(cmd)
			queryClient := types.NewQueryClient(clientCtx)

			res, err := queryClient.EVMAddress(
				cmd.Context(),
				&types.QueryEVMAddressRequest{ValidatorAddress: args[0]},
			)
			if err != nil {
				return err
			}
			if res.EvmAddress == "" {
				return types.ErrEVMAddressNotFound
			}
			fmt.Println(res.EvmAddress)
			return nil
		},
	}

	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

// unmarshallAttestation unmarshal a wrapper protobuf `Any` type to an `AttestationRequestI`.
func unmarshallAttestation(attestation *codectypes.Any) (types.AttestationRequestI, error) {
	var unmarshalledAttestation types.AttestationRequestI
	err := makeInterfaceRegistry().UnpackAny(attestation, &unmarshalledAttestation)
	if err != nil {
		return nil, err
	}
	return unmarshalledAttestation, nil
}

// makeInterfaceRegistry creates the interface registry containing the Blobstream interfaces
func makeInterfaceRegistry() codectypes.InterfaceRegistry {
	// create the codec
	interfaceRegistry := codectypes.NewInterfaceRegistry()

	// register the standard types from the sdk
	std.RegisterInterfaces(interfaceRegistry)

	// register the blobstream module interfaces
	types.RegisterInterfaces(interfaceRegistry)

	return interfaceRegistry
}
