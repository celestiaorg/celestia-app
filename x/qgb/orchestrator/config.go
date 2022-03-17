package orchestrator

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	ethcmn "github.com/ethereum/go-ethereum/common"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

const (
	// cosmos-sdk keyring flags
	keyringBackendFlag = "keyring-backend"
	keyringPathFlag    = "keyring-path"
	keyringAccountName = "keyring-account"
	chainIDFlag        = "celes-chain-id"

	// ethereum signing
	privateKeyFlag = "eth-priv-key"
	bridgeID       = "bridge-id"
	evnChainIDFlag = "evm-chain-id"

	// rpc
	celesGRPCFlag     = "celes-grpc"
	tendermintRPCFlag = "celes-http-rpc"
	ethRPCFlag        = "eth-rpc"

	contractAddressFlag = "contract"
)

func addOrchestratorFlags(cmd *cobra.Command) *cobra.Command {
	cmd.Flags().StringP(keyringBackendFlag, "back", "test", "Select keyring's backend (os|file|kwallet|pass|test)")
	cmd.Flags().StringP(keyringPathFlag, "path", filepath.Join(HomeDir, ".celestia-app"), "Specify the path to the keyring keys")
	cmd.Flags().StringP(keyringAccountName, "name", "user", "Specify the account name used with the keyring")
	cmd.Flags().StringP(privateKeyFlag, "priv", "", "Provide the private key used to sign relayed evm transactions or to sign orchestrator commitments")
	cmd.Flags().StringP(chainIDFlag, "cid", "user", "Specify the celestia chain id")
	cmd.Flags().StringP(bridgeID, "bid", "universal", "Specify the bridge id")
	cmd.Flags().Uint64P(evnChainIDFlag, "eid", 5, "Specify the evm chain id")

	cmd.Flags().StringP(celesGRPCFlag, "cRPC", "tcp://localhost:9090", "Specify the grpc address")
	cmd.Flags().StringP(tendermintRPCFlag, "tRPC", "http://localhost:26657", "Specify the rest rpc address")
	cmd.Flags().StringP(ethRPCFlag, "eRPC", "http://localhost:8545", "Specify the ethereum rpc address")

	cmd.Flags().StringP(contractAddressFlag, "c", "", "Specify the contract at which the qgb is deployed")

	return cmd
}

type config struct {
	keyringBackend, keyringPath, keyringAccount string
	celestiaChainID                             string
	privateKey                                  *ecdsa.PrivateKey
	bridgeID                                    ethcmn.Hash
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
	rawBridgeID, err := cmd.Flags().GetString(bridgeID)
	if err != nil {
		return config{}, err
	}
	evmChainID, err := cmd.Flags().GetUint64(evnChainIDFlag)
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

	return config{
		keyringBackend:  keyringBackend,
		keyringPath:     keyringPath,
		keyringAccount:  keyringAccount,
		privateKey:      ethPrivKey,
		celestiaChainID: chainID,
		bridgeID:        ethcmn.HexToHash(rawBridgeID),
		evmChainID:      evmChainID,
		qgbRPC:          qgbRPC,
		tendermintRPC:   tendermintRPC,
		contractAddr:    address,
	}, nil
}
