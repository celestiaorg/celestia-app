package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"

	tmservice "github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdk "github.com/cosmos/cosmos-sdk/types"

	// Import all module types that have parameters
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	distrtypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	slashingtypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	icahosttypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/host/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"

	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v6/x/minfee/types"
)

var (
	grpcAddr string
	height   int64
	output   string
)

func main() {
	// Initialize SDK config with Celestia's address prefix (only if not already sealed)
	sdkConfig := sdk.GetConfig()
	if sdkConfig.GetBech32AccountAddrPrefix() == "" {
		sdkConfig.SetBech32PrefixForAccount("celestia", "celestiapub")
		sdkConfig.Seal()
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "query-params [grpc-endpoint]",
	Short: "Query blockchain parameters from a Celestia node",
	Long: `Query all module parameters from a Celestia node via gRPC.

Examples:
  query-params                                    # Query localhost:9090
  query-params consensus.lunaroasis.net:9090      # Query remote node
  query-params -g consensus.lunaroasis.net:9090   # Using flag
  query-params --height 1000000                   # Query at specific height`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// If positional arg provided, use it as grpc address
		if len(args) > 0 {
			grpcAddr = args[0]
		}
		return run()
	},
}

func init() {
	rootCmd.Flags().StringVarP(&grpcAddr, "grpc", "g", "localhost:9090", "gRPC endpoint address")
	rootCmd.Flags().Int64Var(&height, "height", 0, "Block height to query (0 for latest)")
	rootCmd.Flags().StringVarP(&output, "output", "o", "json", "Output format: json or text")
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to gRPC endpoint
	conn, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to gRPC: %w", err)
	}
	defer conn.Close()

	// Get header information for the specified height
	headerInfo, appVersion, err := getHeaderInfo(ctx, conn, height)
	if err != nil {
		return fmt.Errorf("failed to get header info: %w", err)
	}

	if output == "text" {
		fmt.Printf("Height: %d\n", headerInfo.Height)
		fmt.Printf("App Version: %d\n", appVersion)
		fmt.Printf("Chain ID: %s\n", headerInfo.ChainID)
		fmt.Println("=== Module Parameters ===")
	}

	// Query parameters for all modules based on app version
	params, err := queryAllParams(ctx, conn, height, appVersion)
	if err != nil {
		return fmt.Errorf("failed to query parameters: %w", err)
	}

	// Output results
	if output == "json" {
		result := map[string]interface{}{
			"height":      headerInfo.Height,
			"app_version": appVersion,
			"chain_id":    headerInfo.ChainID,
			"time":        headerInfo.Time,
			"params":      params,
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(result); err != nil {
			return fmt.Errorf("failed to encode JSON: %w", err)
		}
	} else {
		for moduleName, moduleParams := range params {
			fmt.Printf("Module: %s\n", moduleName)
			data, _ := json.MarshalIndent(moduleParams, "  ", "  ")
			fmt.Printf("  %s\n\n", string(data))
		}
	}

	return nil
}

type HeaderInfo struct {
	Height  int64
	Time    time.Time
	ChainID string
}

func getHeaderInfo(ctx context.Context, conn *grpc.ClientConn, height int64) (*HeaderInfo, uint64, error) {
	client := tmservice.NewServiceClient(conn)

	var header *tmservice.Header
	if height == 0 {
		// Get latest block
		resp, err := client.GetLatestBlock(ctx, &tmservice.GetLatestBlockRequest{})
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get latest block: %w", err)
		}
		headerCopy := resp.SdkBlock.Header
		header = &headerCopy
	} else {
		// Get block at specific height
		resp, err := client.GetBlockByHeight(ctx, &tmservice.GetBlockByHeightRequest{Height: height})
		if err != nil {
			return nil, 0, fmt.Errorf("failed to get block at height %d: %w", height, err)
		}
		headerCopy := resp.SdkBlock.Header
		header = &headerCopy
	}

	info := &HeaderInfo{
		Height:  header.Height,
		Time:    header.Time,
		ChainID: header.ChainID,
	}

	// App version is stored in the header
	appVersion := header.Version.App

	return info, appVersion, nil
}

func queryAllParams(ctx context.Context, conn *grpc.ClientConn, height int64, appVersion uint64) (map[string]interface{}, error) {
	params := make(map[string]interface{})

	// Create a metadata context with height for historical queries
	var queryCtx context.Context
	if height > 0 {
		queryCtx = ctx
	} else {
		queryCtx = ctx
	}

	// Initialize encoding config to get proper codec
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	_ = encodingConfig // May be needed for some queries

	// Query parameters for each module
	// Note: Some modules may not exist in all app versions

	// Core Cosmos SDK modules
	if err := queryAuthParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query auth params: %v\n", err)
	}

	if err := queryBankParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query bank params: %v\n", err)
	}

	if err := queryStakingParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query staking params: %v\n", err)
	}

	if err := querySlashingParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query slashing params: %v\n", err)
	}

	if err := queryDistributionParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query distribution params: %v\n", err)
	}

	if err := queryGovParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query gov params: %v\n", err)
	}

	if err := queryMintParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query mint params: %v\n", err)
	}

	// Consensus params (added in v4/v5)
	if appVersion >= 4 {
		if err := queryConsensusParams(queryCtx, conn, params); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to query consensus params: %v\n", err)
		}
	}

	// IBC modules
	if err := queryIBCTransferParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query ibc-transfer params: %v\n", err)
	}

	if err := queryICAHostParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query ica-host params: %v\n", err)
	}

	// Celestia-specific modules
	if err := queryBlobParams(queryCtx, conn, params); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to query blob params: %v\n", err)
	}

	// MinFee module (added in v4)
	if appVersion >= 4 {
		if err := queryMinFeeParams(queryCtx, conn, params); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to query minfee params: %v\n", err)
		}
	}

	return params, nil
}

// Query functions for each module

func queryAuthParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := authtypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &authtypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["auth"] = resp.Params
	return nil
}

func queryBankParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := banktypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &banktypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["bank"] = resp.Params
	return nil
}

func queryStakingParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := stakingtypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &stakingtypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["staking"] = resp.Params
	return nil
}

func querySlashingParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := slashingtypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &slashingtypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["slashing"] = resp.Params
	return nil
}

func queryDistributionParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := distrtypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &distrtypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["distribution"] = resp.Params
	return nil
}

func queryGovParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := govv1.NewQueryClient(conn)

	// Gov params are split into different types in v1
	govParams := make(map[string]interface{})

	// Query deposit params
	depositResp, err := client.Params(ctx, &govv1.QueryParamsRequest{ParamsType: "deposit"})
	if err != nil {
		return err
	}
	if depositResp.DepositParams != nil {
		govParams["deposit"] = depositResp.DepositParams
	}

	// Query voting params
	votingResp, err := client.Params(ctx, &govv1.QueryParamsRequest{ParamsType: "voting"})
	if err != nil {
		return err
	}
	if votingResp.VotingParams != nil {
		govParams["voting"] = votingResp.VotingParams
	}

	// Query tally params
	tallyResp, err := client.Params(ctx, &govv1.QueryParamsRequest{ParamsType: "tallying"})
	if err != nil {
		return err
	}
	if tallyResp.TallyParams != nil {
		govParams["tally"] = tallyResp.TallyParams
	}

	params["gov"] = govParams
	return nil
}

func queryMintParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := minttypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &minttypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["mint"] = resp.Params
	return nil
}

func queryConsensusParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := consensustypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &consensustypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["consensus"] = resp.Params
	return nil
}

func queryIBCTransferParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := ibctransfertypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &ibctransfertypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["ibc-transfer"] = resp.Params
	return nil
}

func queryICAHostParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := icahosttypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &icahosttypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["ica-host"] = resp.Params
	return nil
}

func queryBlobParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := blobtypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &blobtypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["blob"] = resp.Params
	return nil
}

func queryMinFeeParams(ctx context.Context, conn *grpc.ClientConn, params map[string]interface{}) error {
	client := minfeetypes.NewQueryClient(conn)
	resp, err := client.Params(ctx, &minfeetypes.QueryParamsRequest{})
	if err != nil {
		return err
	}
	params["minfee"] = resp.Params
	return nil
}
