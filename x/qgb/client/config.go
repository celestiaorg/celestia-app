package client

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client/flags"

	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
)

const (
	evmChainIDFlag      = "evm-chain-id"
	celesGRPCFlag       = "celes-grpc"
	evmRPCFlag          = "evm-rpc"
	contractAddressFlag = "contract-address"
)

func addVerifyFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringP(flags.FlagChainID, "x", "mocha", "The network chain ID")
	cmd.Flags().Uint64P(evmChainIDFlag, "z", 5, "The EVM chain ID")
	cmd.Flags().StringP(flags.FlagNode, "t", "http://localhost:26657", "<host>:<port> to Tendermint RPC interface for this chain")
	cmd.Flags().StringP(evmRPCFlag, "e", "http://localhost:8545", "The EVM RPC address")
	cmd.Flags().StringP(contractAddressFlag, "a", "", "The contract address at which the QGB is deployed")
	cmd.Flags().StringP(celesGRPCFlag, "c", "localhost:9090", "<host>:<port> To Celestia GRPC address")

	return cmd
}

type VerifyConfig struct {
	CelestiaChainID                  string
	EVMRPC, CelesGRPC, TendermintRPC string
	EVMChainID                       uint64
	ContractAddr                     ethcmn.Address
}

func parseVerifyFlags(cmd *cobra.Command) (VerifyConfig, error) {
	chainID, err := cmd.Flags().GetString(flags.FlagChainID)
	if err != nil {
		return VerifyConfig{}, err
	}
	evmChainID, err := cmd.Flags().GetUint64(evmChainIDFlag)
	if err != nil {
		return VerifyConfig{}, err
	}
	tendermintRPC, err := cmd.Flags().GetString(flags.FlagNode)
	if err != nil {
		return VerifyConfig{}, err
	}
	celesGRPC, err := cmd.Flags().GetString(celesGRPCFlag)
	if err != nil {
		return VerifyConfig{}, err
	}
	evmRPC, err := cmd.Flags().GetString(evmRPCFlag)
	if err != nil {
		return VerifyConfig{}, err
	}
	contractAddr, err := cmd.Flags().GetString(contractAddressFlag)
	if err != nil {
		return VerifyConfig{}, err
	}
	if contractAddr == "" {
		return VerifyConfig{}, fmt.Errorf("contract address flag is required: %s", contractAddressFlag)
	}
	if !ethcmn.IsHexAddress(contractAddr) {
		return VerifyConfig{}, fmt.Errorf("valid contract address flag is required: %s", contractAddressFlag)
	}
	address := ethcmn.HexToAddress(contractAddr)

	return VerifyConfig{
		CelestiaChainID: chainID,
		EVMChainID:      evmChainID,
		CelesGRPC:       celesGRPC,
		TendermintRPC:   tendermintRPC,
		EVMRPC:          evmRPC,
		ContractAddr:    address,
	}, nil
}
