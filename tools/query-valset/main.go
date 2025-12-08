package main

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/types/query"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultEndpoint   = "localhost:9090"
	defaultOutputFile = "validators.json"
	envEndpoint       = "GRPC_ENDPOINT"
)

var (
	endpoint   string
	outputDir  string
	outputFile string
	status     string
	useTLS     bool
	skipVerify bool
	autoDetect bool
)

// ValidatorInfo contains the validator metadata mapped by hex address.
type ValidatorInfo struct {
	Moniker           string `json:"moniker"`
	OperatorAddress   string `json:"operator_address"`
	ConsensusPubkey   string `json:"consensus_pubkey,omitempty"`
	Jailed            bool   `json:"jailed"`
	Status            string `json:"status"`
	Tokens            string `json:"tokens"`
	DelegatorShares   string `json:"delegator_shares"`
	VotingPower       int64  `json:"voting_power"`
	Commission        string `json:"commission"`
	Website           string `json:"website,omitempty"`
	Identity          string `json:"identity,omitempty"`
	Details           string `json:"details,omitempty"`
	SecurityContact   string `json:"security_contact,omitempty"`
	UnbondingHeight   int64  `json:"unbonding_height,omitempty"`
	MinSelfDelegation string `json:"min_self_delegation"`
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query-valset",
		Short: "Query the validator set and output a mapping of hex addresses to validator metadata",
		Long: `A tool for querying the validator set from a Celestia/Cosmos chain via gRPC.
Outputs a JSON mapping of validator hex addresses to their metadata including
moniker, voting power, commission, and other details.

The output is formatted JSON and can be piped to other applications.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return queryValidatorSet()
		},
	}

	// Determine default endpoint: flag > env > hardcoded default
	defaultEp := defaultEndpoint
	if envVal := os.Getenv(envEndpoint); envVal != "" {
		defaultEp = envVal
	}

	cmd.Flags().StringVarP(&endpoint, "endpoint", "e", defaultEp,
		fmt.Sprintf("gRPC endpoint to connect to (env: %s)", envEndpoint))
	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", ".",
		"Directory to save the output JSON file")
	cmd.Flags().StringVarP(&outputFile, "file", "f", defaultOutputFile,
		"Output filename")
	cmd.Flags().StringVarP(&status, "status", "s", "",
		"Filter validators by status (BOND_STATUS_BONDED, BOND_STATUS_UNBONDING, BOND_STATUS_UNBONDED)")
	cmd.Flags().BoolVarP(&useTLS, "tls", "t", false,
		"Use TLS for gRPC connection (required for most public endpoints)")
	cmd.Flags().BoolVarP(&skipVerify, "skip-verify", "k", false,
		"Skip TLS certificate verification (insecure)")
	cmd.Flags().BoolVarP(&autoDetect, "auto", "a", false,
		"Auto-detect TLS settings by trying multiple connection modes")

	return cmd
}

// connectionMode represents a TLS configuration to try
type connectionMode struct {
	name       string
	tls        bool
	skipVerify bool
}

func queryValidatorSet() error {
	ctx := context.Background()

	// Create interface registry and register crypto types for unpacking
	interfaceRegistry := codectypes.NewInterfaceRegistry()
	cryptocodec.RegisterInterfaces(interfaceRegistry)
	stakingtypes.RegisterInterfaces(interfaceRegistry)
	cdc := codec.NewProtoCodec(interfaceRegistry)

	var conn *grpc.ClientConn
	var err error

	if autoDetect {
		// Try multiple connection modes
		modes := []connectionMode{
			{"TLS", true, false},
			{"TLS (skip verify)", true, true},
			{"plaintext", false, false},
		}

		for _, mode := range modes {
			fmt.Fprintf(os.Stderr, "Trying %s connection...\n", mode.name)
			conn, err = createConnection(endpoint, mode.tls, mode.skipVerify)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  Failed to create connection: %v\n", err)
				continue
			}

			// Try a test query to see if the connection works
			stakingClient := stakingtypes.NewQueryClient(conn)
			_, err = stakingClient.Validators(ctx, &stakingtypes.QueryValidatorsRequest{
				Pagination: &query.PageRequest{Limit: 1},
			})
			if err == nil {
				fmt.Fprintf(os.Stderr, "  Success! Using %s mode.\n", mode.name)
				break
			}
			fmt.Fprintf(os.Stderr, "  Query failed: %v\n", err)
			conn.Close()
			conn = nil
		}

		if conn == nil {
			return fmt.Errorf("failed to connect with any mode. Try specifying --tls or check the endpoint")
		}
	} else {
		conn, err = createConnection(endpoint, useTLS, skipVerify)
		if err != nil {
			return fmt.Errorf("failed to create gRPC connection: %w", err)
		}
	}
	defer conn.Close()

	// Create staking query client
	stakingClient := stakingtypes.NewQueryClient(conn)

	// Query all validators with pagination
	validators := make(map[string]ValidatorInfo)
	var nextKey []byte

	for {
		req := &stakingtypes.QueryValidatorsRequest{
			Status: status,
			Pagination: &query.PageRequest{
				Key:   nextKey,
				Limit: 100,
			},
		}

		resp, err := stakingClient.Validators(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to query validators: %w", err)
		}

		for _, val := range resp.Validators {
			// Unpack the consensus pubkey
			if err := val.UnpackInterfaces(cdc); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to unpack interfaces for validator %s: %v\n", val.OperatorAddress, err)
				continue
			}

			// Get consensus address (hex)
			consAddr, err := val.GetConsAddr()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Warning: failed to get consensus address for validator %s: %v\n", val.OperatorAddress, err)
				continue
			}
			hexAddr := strings.ToUpper(hex.EncodeToString(consAddr))

			// Get consensus pubkey as string
			var pubkeyStr string
			if pk, ok := val.ConsensusPubkey.GetCachedValue().(cryptotypes.PubKey); ok {
				pubkeyStr = hex.EncodeToString(pk.Bytes())
			}

			// Calculate voting power (tokens / 10^6 for bonded validators)
			var votingPower int64
			if val.IsBonded() {
				// Tokens are in utia (10^-6), voting power is in whole units
				votingPower = val.Tokens.Int64() / 1_000_000
			}

			validators[hexAddr] = ValidatorInfo{
				Moniker:           val.Description.Moniker,
				OperatorAddress:   val.OperatorAddress,
				ConsensusPubkey:   pubkeyStr,
				Jailed:            val.Jailed,
				Status:            val.Status.String(),
				Tokens:            val.Tokens.String(),
				DelegatorShares:   val.DelegatorShares.String(),
				VotingPower:       votingPower,
				Commission:        val.Commission.CommissionRates.Rate.String(),
				Website:           val.Description.Website,
				Identity:          val.Description.Identity,
				Details:           val.Description.Details,
				SecurityContact:   val.Description.SecurityContact,
				UnbondingHeight:   val.UnbondingHeight,
				MinSelfDelegation: val.MinSelfDelegation.String(),
			}
		}

		// Check if there are more results
		if resp.Pagination == nil || len(resp.Pagination.NextKey) == 0 {
			break
		}
		nextKey = resp.Pagination.NextKey
	}

	// Format JSON output
	output, err := json.MarshalIndent(validators, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// Write to stdout (for piping) and to file
	fmt.Println(string(output))

	// Write to file
	outputPath := filepath.Join(outputDir, outputFile)
	if err := os.WriteFile(outputPath, output, 0o644); err != nil {
		return fmt.Errorf("failed to write output file: %w", err)
	}
	fmt.Fprintf(os.Stderr, "\nOutput saved to: %s\n", outputPath)

	return nil
}

func createConnection(endpoint string, useTLS, skipVerify bool) (*grpc.ClientConn, error) {
	var creds credentials.TransportCredentials
	if useTLS {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: skipVerify, //nolint:gosec
		}
		creds = credentials.NewTLS(tlsConfig)
	} else {
		creds = insecure.NewCredentials()
	}

	return grpc.NewClient(
		endpoint,
		grpc.WithTransportCredentials(creds),
	)
}
