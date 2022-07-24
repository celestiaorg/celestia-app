package orchestrator

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"github.com/spf13/cobra"
	"log"
	"os"
	"path/filepath"

	ethcmn "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

var HomeDir string

func init() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	HomeDir = homeDir
}

const (
	// cosmos-sdk keyring flags
	keyringBackendFlag  = "keyring-backend"
	keyringPathFlag     = "keyring-path"
	keyringAccountName  = "keyring-account"
	celestiaChainIDFlag = "celes-chain-id"

	// ethereum signing
	privateKeyFlag = "eth-priv-key"
	evmChainIDFlag = "evm-chain-id"

	// rpc
	celesGRPCFlag     = "celes-grpc"
	tendermintRPCFlag = "celes-http-rpc"
	evmRPCFlag        = "evm-rpc"

	contractAddressFlag  = "contract-address"
	startingNonceFlag    = "starting-nonce"
	evmGasLimitFlag      = "evm-gas-limit"
	celestiaGasLimitFlag = "celestia-gas-limit"
)

func addOrchestratorFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringP(keyringBackendFlag, "b", "test", "Select keyring's backend (os|file|kwallet|pass|test)")
	cmd.Flags().StringP(
		keyringPathFlag,
		"p",
		filepath.Join(HomeDir, ".celestia-app"),
		"Specify the path to the keyring keys",
	)
	cmd.Flags().StringP(keyringAccountName, "n", "user", "Specify the account name used with the keyring")
	cmd.Flags().StringP(celestiaChainIDFlag, "x", "user", "Specify the celestia chain id")
	cmd.Flags().StringP(tendermintRPCFlag, "t", "http://localhost:26657", "Specify the rest rpc address")
	cmd.Flags().StringP(celesGRPCFlag, "c", "localhost:9090", "Specify the grpc address")
	cmd.Flags().StringP(
		privateKeyFlag,
		"d",
		"",
		"Specify the ECDSA private key used to sign orchestrator commitments in hex",
	)
	cmd.Flags().Uint64P(celestiaGasLimitFlag, "l", DEFAULTCELESTIAGASLIMIT, "Specify the celestia gas limit")

	return cmd
}

type orchestratorConfig struct {
	keyringBackend, keyringPath, keyringAccount string
	celestiaChainID, celesGRPC, tendermintRPC   string
	privateKey                                  *ecdsa.PrivateKey
	celestiaGasLimit                            uint64
}

func parseOrchestratorFlags(cmd *cobra.Command) (orchestratorConfig, error) {
	keyringBackend, err := cmd.Flags().GetString(keyringBackendFlag)
	if err != nil {
		return orchestratorConfig{}, err
	}
	keyringPath, err := cmd.Flags().GetString(keyringPathFlag)
	if err != nil {
		return orchestratorConfig{}, err
	}
	keyringAccount, err := cmd.Flags().GetString(keyringAccountName)
	if err != nil {
		return orchestratorConfig{}, err
	}
	rawPrivateKey, err := cmd.Flags().GetString(privateKeyFlag)
	if err != nil {
		return orchestratorConfig{}, err
	}
	if rawPrivateKey == "" {
		return orchestratorConfig{}, errors.New("private key flag required")
	}
	ethPrivKey, err := ethcrypto.HexToECDSA(rawPrivateKey)
	if err != nil {
		return orchestratorConfig{}, fmt.Errorf("failed to hex-decode Ethereum ECDSA Private Key: %w", err)
	}
	chainID, err := cmd.Flags().GetString(celestiaChainIDFlag)
	if err != nil {
		return orchestratorConfig{}, err
	}
	tendermintRPC, err := cmd.Flags().GetString(tendermintRPCFlag)
	if err != nil {
		return orchestratorConfig{}, err
	}
	celesGRPC, err := cmd.Flags().GetString(celesGRPCFlag)
	if err != nil {
		return orchestratorConfig{}, err
	}
	celestiaGasLimit, err := cmd.Flags().GetUint64(celestiaGasLimitFlag)
	if err != nil {
		return orchestratorConfig{}, err
	}

	return orchestratorConfig{
		keyringBackend:   keyringBackend,
		keyringPath:      keyringPath,
		keyringAccount:   keyringAccount,
		privateKey:       ethPrivKey,
		celestiaChainID:  chainID,
		celesGRPC:        celesGRPC,
		tendermintRPC:    tendermintRPC,
		celestiaGasLimit: celestiaGasLimit,
	}, nil
}

func addRelayerFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringP(privateKeyFlag, "d", "", "Provide the private key used to sign relayed evm transactions")
	cmd.Flags().Uint64P(evmChainIDFlag, "z", 5, "Specify the evm chain id")
	cmd.Flags().StringP(celesGRPCFlag, "c", "localhost:9090", "Specify the grpc address")
	cmd.Flags().StringP(tendermintRPCFlag, "t", "http://localhost:26657", "Specify the rest rpc address")
	cmd.Flags().StringP(evmRPCFlag, "e", "http://localhost:8545", "Specify the ethereum rpc address")
	cmd.Flags().StringP(contractAddressFlag, "a", "", "Specify the contract at which the qgb is deployed")
	cmd.Flags().Uint64P(evmGasLimitFlag, "l", DEFAULTEVMGASLIMIT, "Specify the evm gas limit")

	return cmd
}

type relayerConfig struct {
	evmChainID                       uint64
	evmRPC, celesGRPC, tendermintRPC string
	privateKey                       *ecdsa.PrivateKey
	contractAddr                     ethcmn.Address
	evmGasLimit                      uint64
}

func parseRelayerFlags(cmd *cobra.Command) (relayerConfig, error) {
	rawPrivateKey, err := cmd.Flags().GetString(privateKeyFlag)
	if err != nil {
		return relayerConfig{}, err
	}
	if rawPrivateKey == "" {
		return relayerConfig{}, errors.New("private key flag required")
	}
	ethPrivKey, err := ethcrypto.HexToECDSA(rawPrivateKey)
	if err != nil {
		return relayerConfig{}, fmt.Errorf("failed to hex-decode Ethereum ECDSA Private Key: %w", err)
	}
	evmChainID, err := cmd.Flags().GetUint64(evmChainIDFlag)
	if err != nil {
		return relayerConfig{}, err
	}
	tendermintRPC, err := cmd.Flags().GetString(tendermintRPCFlag)
	if err != nil {
		return relayerConfig{}, err
	}
	celesGRPC, err := cmd.Flags().GetString(celesGRPCFlag)
	if err != nil {
		return relayerConfig{}, err
	}
	contractAddr, err := cmd.Flags().GetString(contractAddressFlag)
	if err != nil {
		return relayerConfig{}, err
	}
	if contractAddr == "" {
		return relayerConfig{}, fmt.Errorf("contract address flag is required: %s", contractAddressFlag)
	}
	if !ethcmn.IsHexAddress(contractAddr) {
		return relayerConfig{}, fmt.Errorf("valid contract address flag is required: %s", contractAddressFlag)
	}
	address := ethcmn.HexToAddress(contractAddr)
	ethRPC, err := cmd.Flags().GetString(evmRPCFlag)
	if err != nil {
		return relayerConfig{}, err
	}
	evmGasLimit, err := cmd.Flags().GetUint64(evmGasLimitFlag)
	if err != nil {
		return relayerConfig{}, err
	}

	return relayerConfig{
		privateKey:    ethPrivKey,
		evmChainID:    evmChainID,
		celesGRPC:     celesGRPC,
		tendermintRPC: tendermintRPC,
		contractAddr:  address,
		evmRPC:        ethRPC,
		evmGasLimit:   evmGasLimit,
	}, nil
}

func addDeployFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringP(privateKeyFlag, "d", "", "Provide the private key used to sign the deploy transaction")
	cmd.Flags().StringP(celestiaChainIDFlag, "x", "user", "Specify the celestia chain id")
	cmd.Flags().Uint64P(evmChainIDFlag, "z", 5, "Specify the evm chain id")
	cmd.Flags().StringP(celesGRPCFlag, "c", "localhost:9090", "Specify the grpc address")
	cmd.Flags().StringP(tendermintRPCFlag, "t", "http://localhost:26657", "Specify the rest rpc address")
	cmd.Flags().StringP(evmRPCFlag, "e", "http://localhost:8545", "Specify the ethereum rpc address")
	cmd.Flags().StringP(
		startingNonceFlag,
		"n",
		"latest",
		"Specify the nonce to start the QGB contract from. "+
			"\"earliest\": for genesis, "+
			"\"latest\": for latest valset nonce, "+
			"\"nonce\": for the latest valset before the provided nonce, provided nonce included.",
	)
	cmd.Flags().Uint64P(evmGasLimitFlag, "l", DEFAULTEVMGASLIMIT, "Specify the evm gas limit")

	return cmd
}

type deployConfig struct {
	celestiaChainID                  string
	evmRPC, celesGRPC, tendermintRPC string
	evmChainID                       uint64
	privateKey                       *ecdsa.PrivateKey
	startingNonce                    string
	evmGasLimit                      uint64
}

func parseDeployFlags(cmd *cobra.Command) (deployConfig, error) {
	rawPrivateKey, err := cmd.Flags().GetString(privateKeyFlag)
	if err != nil {
		return deployConfig{}, err
	}
	if rawPrivateKey == "" {
		return deployConfig{}, errors.New("private key flag required")
	}
	ethPrivKey, err := ethcrypto.HexToECDSA(rawPrivateKey)
	if err != nil {
		return deployConfig{}, fmt.Errorf("failed to hex-decode Ethereum ECDSA Private Key: %w", err)
	}
	chainID, err := cmd.Flags().GetString(celestiaChainIDFlag)
	if err != nil {
		return deployConfig{}, err
	}
	evmChainID, err := cmd.Flags().GetUint64(evmChainIDFlag)
	if err != nil {
		return deployConfig{}, err
	}
	tendermintRPC, err := cmd.Flags().GetString(tendermintRPCFlag)
	if err != nil {
		return deployConfig{}, err
	}
	celesGRPC, err := cmd.Flags().GetString(celesGRPCFlag)
	if err != nil {
		return deployConfig{}, err
	}
	evmRPC, err := cmd.Flags().GetString(evmRPCFlag)
	if err != nil {
		return deployConfig{}, err
	}
	startingNonce, err := cmd.Flags().GetString(startingNonceFlag)
	if err != nil {
		return deployConfig{}, err
	}
	evmGasLimit, err := cmd.Flags().GetUint64(evmGasLimitFlag)
	if err != nil {
		return deployConfig{}, err
	}

	return deployConfig{
		privateKey:      ethPrivKey,
		celestiaChainID: chainID,
		evmChainID:      evmChainID,
		celesGRPC:       celesGRPC,
		tendermintRPC:   tendermintRPC,
		evmRPC:          evmRPC,
		startingNonce:   startingNonce,
		evmGasLimit:     evmGasLimit,
	}, nil
}
