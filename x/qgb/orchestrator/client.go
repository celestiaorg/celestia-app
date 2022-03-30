package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strings"
	"syscall"

	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/rs/zerolog"
	"github.com/tendermint/tendermint/rpc/client/http"
	"golang.org/x/term"
	"google.golang.org/grpc"
)

type client struct {
	logger zerolog.Logger

	// RPC
	tendermintRPC *http.HTTP
	qgbRPC        *grpc.ClientConn
	ethRPC        *ethclient.Client
	wrapper       *wrapper.QuantumGravityBridge

	// orchestrator signing
	singerFn           bind.SignerFn
	personalSignerFn   PersonalSignFn
	transactOpsBuilder transactOpsBuilder
	evmAddress         ethcmn.Address
	bridgeID           ethcmn.Hash
}

func newClient(logger zerolog.Logger, cfg config) (*client, error) {
	trpc, err := http.New(cfg.tendermintRPC, "/websocket")
	if err != nil {
		return nil, err
	}

	qgbGRPC, err := grpc.Dial(cfg.qgbRPC, grpc.WithInsecure())
	if err != nil {
		return nil, err
	}

	ethclient, err := ethclient.Dial(cfg.evmRPC)
	if err != nil {
		return nil, err
	}

	qgbWrapper, err := wrapper.NewQuantumGravityBridge(cfg.contractAddr, ethclient)
	if err != nil {
		return nil, err
	}

	orchAddr, sfn, psfn, err := initEthSigners(cfg.evmChainID, cfg.privateKey)
	if err != nil {
		return nil, err
	}

	transactOpsBuilder := newTransactOptsBuilder(cfg.privateKey)

	return &client{
		tendermintRPC:      trpc,
		singerFn:           sfn,
		personalSignerFn:   psfn,
		transactOpsBuilder: transactOpsBuilder,
		logger:             logger,
		qgbRPC:             qgbGRPC,
		evmAddress:         orchAddr,
		bridgeID:           cfg.bridgeID,
		wrapper:            qgbWrapper,
	}, nil
}

func (oc *client) stop() {
	err := oc.tendermintRPC.Stop()
	if err != nil {
		panic(err)
	}

	err = oc.qgbRPC.Close()
	if err != nil {
		panic(err)
	}

	oc.ethRPC.Close()
}

// TODO: make gas price configurable
type transactOpsBuilder func(ctx context.Context, client *ethclient.Client, gasLim uint64) (*bind.TransactOpts, error)

func newTransactOptsBuilder(privKey *ecdsa.PrivateKey) transactOpsBuilder {
	publicKey := privKey.Public()
	publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
	if !ok {
		panic(fmt.Errorf("invalid public key; expected: %T, got: %T", &ecdsa.PublicKey{}, publicKey))
	}

	evmAddress := ethcrypto.PubkeyToAddress(*publicKeyECDSA)
	return func(ctx context.Context, client *ethclient.Client, gasLim uint64) (*bind.TransactOpts, error) {
		nonce, err := client.PendingNonceAt(ctx, evmAddress)
		if err != nil {
			return nil, err
		}

		ethChainID, err := client.ChainID(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get Ethereum chain ID: %w", err)
		}

		auth, err := bind.NewKeyedTransactorWithChainID(privKey, ethChainID)
		if err != nil {
			return nil, fmt.Errorf("failed to create Ethereum transactor: %w", err)
		}

		bigGasPrice, err := client.SuggestGasPrice(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to get Ethereum gas estimate: %w", err)
		}

		auth.Nonce = new(big.Int).SetUint64(nonce)
		auth.Value = big.NewInt(0) // in wei
		auth.GasLimit = gasLim     // in units
		auth.GasPrice = bigGasPrice

		return auth, nil
	}
}

type PersonalSignFn func(account ethcmn.Address, data []byte) (sig []byte, err error)

func PrivateKeyPersonalSignFn(privKey *ecdsa.PrivateKey) (PersonalSignFn, error) {
	keyAddress := crypto.PubkeyToAddress(privKey.PublicKey)

	signFn := func(from common.Address, data []byte) (sig []byte, err error) {
		if from != keyAddress {
			return nil, errors.New("from address mismatch")
		}

		protectedHash := accounts.TextHash(data)
		return crypto.Sign(protectedHash, privKey)
	}

	return signFn, nil
}

func initEthSigners(
	ethChainID uint64,
	ethPrivKey *ecdsa.PrivateKey,
) (
	ethcmn.Address,
	bind.SignerFn,
	PersonalSignFn,
	error,
) {

	addr := ethcrypto.PubkeyToAddress(ethPrivKey.PublicKey)

	txOpts, err := bind.NewKeyedTransactorWithChainID(ethPrivKey, new(big.Int).SetUint64(ethChainID))
	if err != nil {
		return ethcmn.Address{}, nil, nil, fmt.Errorf("failed to init NewKeyedTransactorWithChainID: %w", err)
	}

	personalSignFn, err := PrivateKeyPersonalSignFn(ethPrivKey)
	if err != nil {
		return ethcmn.Address{}, nil, nil, fmt.Errorf("failed to init PrivateKeyPersonalSignFn: %w", err)
	}

	return addr, txOpts.Signer, personalSignFn, nil
}

func ethPassFromStdin() (string, error) {
	fmt.Fprintln(os.Stderr, "Passphrase for Ethereum account: ")
	bytePassword, err := term.ReadPassword(syscall.Stdin)
	if err != nil {
		return "", fmt.Errorf("failed to read password from STDIN: %w", err)
	}

	password := string(bytePassword)
	return strings.TrimSpace(password), nil
}
