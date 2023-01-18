package client

import (
	"fmt"

	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
)

const (
	celestiaChainIDFlag = "celes-chain-id"
	evmChainIDFlag      = "evm-chain-id"
	celesGRPCFlag       = "celes-grpc"
	tendermintRPCFlag   = "celes-http-rpc"
	evmRPCFlag          = "evm-rpc"
	contractAddressFlag = "contract-address"
)

func addVerifyFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringP(celestiaChainIDFlag, "x", "user", "Specify the celestia chain id")
	cmd.Flags().Uint64P(evmChainIDFlag, "z", 5, "Specify the evm chain id")
	cmd.Flags().StringP(tendermintRPCFlag, "t", "http://localhost:26657", "Specify the rest rpc address")
	cmd.Flags().StringP(evmRPCFlag, "e", "http://localhost:8545", "Specify the EVM rpc address")
	cmd.Flags().StringP(contractAddressFlag, "a", "", "Specify the contract address at which the qgb is deployed")
	cmd.Flags().StringP(celesGRPCFlag, "c", "localhost:9090", "Specify the grpc address")

	return cmd
}

type VerifyConfig struct {
	CelestiaChainID                  string
	EVMRPC, CelesGRPC, TendermintRPC string
	EVMChainID                       uint64
	ContractAddr                     ethcmn.Address
}

func parseVerifyFlags(cmd *cobra.Command) (VerifyConfig, error) {
	chainID, err := cmd.Flags().GetString(celestiaChainIDFlag)
	if err != nil {
		return VerifyConfig{}, err
	}
	evmChainID, err := cmd.Flags().GetUint64(evmChainIDFlag)
	if err != nil {
		return VerifyConfig{}, err
	}
	tendermintRPC, err := cmd.Flags().GetString(tendermintRPCFlag)
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
