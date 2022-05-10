package orchestrator

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	ethcmn "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

var (
	HomeDir string
)

func init() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	HomeDir = homeDir
}

const (
	// cosmos-sdk keyring flags
	keyringBackendFlag = "keyring-backend"
	keyringPathFlag    = "keyring-path"
	keyringAccountName = "keyring-account"
	chainIDFlag        = "celes-chain-id"

	// ethereum signing
	privateKeyFlag = "eth-priv-key"
	evmChainIDFlag = "evm-chain-id"

	// rpc
	celesGRPCFlag     = "celes-grpc"
	tendermintRPCFlag = "celes-http-rpc"
	ethRPCFlag        = "eth-rpc"

	contractAddressFlag = "contract"
)

func addOrchestratorFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringP(keyringBackendFlag, "b", "test", "Select keyring's backend (os|file|kwallet|pass|test)")
	cmd.Flags().StringP(keyringPathFlag, "p", filepath.Join(HomeDir, ".celestia-app"), "Specify the path to the keyring keys")
	cmd.Flags().StringP(keyringAccountName, "n", "user", "Specify the account name used with the keyring")
	cmd.Flags().StringP(privateKeyFlag, "d", "", "Provide the private key used to sign relayed evm transactions or to sign orchestrator commitments")
	cmd.Flags().StringP(chainIDFlag, "x", "user", "Specify the celestia chain id")
	cmd.Flags().Uint64P(evmChainIDFlag, "z", 5, "Specify the evm chain id")

	cmd.Flags().StringP(celesGRPCFlag, "c", "localhost:9090", "Specify the grpc address")
	cmd.Flags().StringP(tendermintRPCFlag, "t", "http://localhost:26657", "Specify the rest rpc address")
	cmd.Flags().StringP(ethRPCFlag, "e", "http://localhost:8545", "Specify the ethereum rpc address")

	cmd.Flags().StringP(contractAddressFlag, "a", "", "Specify the contract at which the qgb is deployed")

	return cmd
}

type config struct {
	keyringBackend, keyringPath, keyringAccount string
	celestiaChainID                             string
	privateKey                                  *ecdsa.PrivateKey
	evmChainID                                  uint64
	qgbRPC, tendermintRPC, evmRPC               string
	contractAddr                                ethcmn.Address
}

func parseOrchestratorFlags(cmd *cobra.Command) (config, error) {
	keyringBackend, err := cmd.Flags().GetString(keyringBackendFlag)
	if err != nil {
		return config{}, err
	}
	keyringPath, err := cmd.Flags().GetString(keyringPathFlag)
	if err != nil {
		return config{}, err
	}
	keyringAccount, err := cmd.Flags().GetString(keyringAccountName)
	if err != nil {
		return config{}, err
	}
	rawPrivateKey, err := cmd.Flags().GetString(privateKeyFlag)
	if err != nil {
		return config{}, err
	}
	if rawPrivateKey == "" {
		return config{}, errors.New("private key flag required")
	}
	ethPrivKey, err := ethcrypto.HexToECDSA(rawPrivateKey)
	if err != nil {
		return config{}, fmt.Errorf("failed to hex-decode Ethereum ECDSA Private Key: %w", err)
	}
	chainID, err := cmd.Flags().GetString(chainIDFlag)
	if err != nil {
		return config{}, err
	}
	evmChainID, err := cmd.Flags().GetUint64(evmChainIDFlag)
	if err != nil {
		return config{}, err
	}
	tendermintRPC, err := cmd.Flags().GetString(tendermintRPCFlag)
	if err != nil {
		return config{}, err
	}
	qgbRPC, err := cmd.Flags().GetString(celesGRPCFlag)
	if err != nil {
		return config{}, err
	}
	contractAddr, err := cmd.Flags().GetString(contractAddressFlag)
	if err != nil {
		return config{}, err
	}
	if contractAddr == "" {
		return config{}, fmt.Errorf("contract address flag is required: %s", contractAddressFlag)
	}
	if !ethcmn.IsHexAddress(contractAddr) {
		return config{}, fmt.Errorf("valid contract address flag is required: %s", contractAddressFlag)
	}
	address := ethcmn.HexToAddress(contractAddr)
	ethRpc, err := cmd.Flags().GetString(ethRPCFlag)
	if err != nil {
		return config{}, err
	}

	return config{
		keyringBackend:  keyringBackend,
		keyringPath:     keyringPath,
		keyringAccount:  keyringAccount,
		privateKey:      ethPrivKey,
		celestiaChainID: chainID,
		evmChainID:      evmChainID,
		qgbRPC:          qgbRPC,
		tendermintRPC:   tendermintRPC,
		contractAddr:    address,
		evmRPC:          ethRpc,
	}, nil
}
