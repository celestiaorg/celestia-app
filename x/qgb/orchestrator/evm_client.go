package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"

	gethcommon "github.com/ethereum/go-ethereum/common"
	coregethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	tmlog "github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

var _ EVMClient = &evmClient{}

type EVMClient interface {
	DeployQGBContract(
		ctx context.Context,
		contractInitValset types.Valset,
		contractInitNonce uint64,
		chainID uint64,
		waitToBeMined bool,
		initBridge bool,
	) (gethcommon.Address, *coregethtypes.Transaction, *wrapper.QuantumGravityBridge, error)
	UpdateValidatorSet(
		ctx context.Context,
		newNonce, newThreshHold uint64,
		currentValset, newValset types.Valset,
		sigs []wrapper.Signature,
		waitToBeMined bool,
	) error
	SubmitDataRootTupleRoot(
		ctx context.Context,
		tupleRoot gethcommon.Hash,
		lastDataCommitmentNonce uint64,
		currentValset types.Valset,
		sigs []wrapper.Signature,
		waitToBeMined bool,
	) error
	StateLastEventNonce(opts *bind.CallOpts) (uint64, error)
}

type evmClient struct {
	logger     tmlog.Logger
	wrapper    *wrapper.QuantumGravityBridge
	privateKey *ecdsa.PrivateKey
	evmRPC     string
}

// NewEvmClient Creates a new EVM Client that can be used to deploy the QGB contract and
// interact with it.
// The wrapper parameter can be nil when creating the client for contract deployment.
func NewEvmClient(
	logger tmlog.Logger,
	wrapper *wrapper.QuantumGravityBridge,
	privateKey *ecdsa.PrivateKey,
	evmRPC string,
) *evmClient {
	return &evmClient{
		logger:     logger,
		wrapper:    wrapper,
		privateKey: privateKey,
		evmRPC:     evmRPC,
	}
}

// DeployQGBContract Deploys the QGB contract and initializes it with the provided valset.
// The waitToBeMined, when set to true, will wait for the transaction to be included in a block,
// and log relevant information.
// The initBridge, when set to true, will assign the newly deployed bridge to the wrapper. This
// later can be used for further interactions with the newly contract.
func (ec *evmClient) DeployQGBContract(
	ctx context.Context,
	contractInitValset types.Valset,
	contractInitNonce uint64,
	chainID uint64,
	waitToBeMined bool,
	initBridge bool,
) (gethcommon.Address, *coregethtypes.Transaction, *wrapper.QuantumGravityBridge, error) {
	opts, err := bind.NewKeyedTransactorWithChainID(ec.privateKey, big.NewInt(int64(chainID)))
	if err != nil {
		return gethcommon.Address{}, nil, nil, err
	}

	ethClient, err := ethclient.Dial(ec.evmRPC)
	if err != nil {
		return gethcommon.Address{}, nil, nil, err
	}
	defer ethClient.Close()

	ethVsHash, err := contractInitValset.Hash()
	if err != nil {
		return gethcommon.Address{}, nil, nil, err
	}

	// deploy the QGB contract using the chain parameters
	addr, tx, bridge, err := wrapper.DeployQuantumGravityBridge(
		opts,
		ethClient,
		types.BridgeId,
		big.NewInt(int64(contractInitNonce)),
		big.NewInt(int64(contractInitValset.TwoThirdsThreshold())),
		ethVsHash,
	)
	if err != nil {
		return gethcommon.Address{}, nil, nil, err
	}
	if !waitToBeMined {
		return addr, tx, bridge, nil
	}

	if initBridge {
		// initializing the bridge
		ec.wrapper = bridge
	}

	receipt, err := ec.waitForTransaction(ctx, tx)
	if err == nil && receipt != nil && receipt.Status == 1 {
		ec.logger.Info("deployed QGB contract", "address", addr.Hex(), "hash", tx.Hash().String())
		return addr, tx, bridge, nil
	}
	ec.logger.Error("failed to delpoy QGB contract", "hash", tx.Hash().String())
	return addr, tx, bridge, err
}

func (ec *evmClient) UpdateValidatorSet(
	ctx context.Context,
	newNonce, newThreshHold uint64,
	currentValset, newValset types.Valset,
	sigs []wrapper.Signature,
	waitToBeMined bool,
) error {
	// TODO in addition to the nonce, log more interesting information
	ec.logger.Info("relaying valset", "nonce", newNonce)
	// TODO gasLimit ?
	opts, err := ec.NewTransactOpts(ctx, 1000000)
	if err != nil {
		return err
	}

	ethVals, err := ethValset(currentValset)
	if err != nil {
		return err
	}

	ethVsHash, err := newValset.Hash()
	if err != nil {
		return err
	}

	var currentNonce uint64
	if newValset.Nonce == 1 {
		currentNonce = 0
	} else {
		currentNonce = currentValset.Nonce
	}

	tx, err := ec.wrapper.UpdateValidatorSet(
		opts,
		big.NewInt(int64(newNonce)),
		big.NewInt(int64(currentNonce)),
		big.NewInt(int64(newThreshHold)),
		ethVsHash,
		ethVals,
		sigs,
	)
	if err != nil {
		return err
	}

	if !waitToBeMined {
		return nil
	}

	ec.logger.Debug("waiting for valset to be confirmed", "nonce", newNonce, "hash", tx.Hash().String())
	receipt, err := ec.waitForTransaction(ctx, tx)
	if err == nil && receipt != nil && receipt.Status == 1 {
		ec.logger.Info("relayed valset", "nonce", newNonce, "hash", tx.Hash().String())
		return nil
	}
	ec.logger.Error("failed to relay valset", "nonce", newNonce, "hash", tx.Hash().String())
	return err
}

func (ec *evmClient) SubmitDataRootTupleRoot(
	ctx context.Context,
	tupleRoot gethcommon.Hash,
	newNonce uint64,
	currentValset types.Valset,
	sigs []wrapper.Signature,
	waitToBeMined bool,
) error {
	opts, err := ec.NewTransactOpts(ctx, 1000000)
	if err != nil {
		return err
	}

	ethVals, err := ethValset(currentValset)
	if err != nil {
		return err
	}

	tx, err := ec.wrapper.SubmitDataRootTupleRoot(
		opts,
		big.NewInt(int64(newNonce)),
		big.NewInt(int64(currentValset.Nonce)),
		tupleRoot,
		ethVals,
		sigs,
	)
	if err != nil {
		return err
	}

	if !waitToBeMined {
		return nil
	}

	ec.logger.Debug("waiting for data commitment to be confirmed", "nonce", newNonce, "hash", tx.Hash().String())
	receipt, err := ec.waitForTransaction(ctx, tx)
	if err == nil && receipt != nil && receipt.Status == 1 {
		ec.logger.Info("relayed data commitment", "nonce", newNonce, "hash", tx.Hash().String())
		return nil
	}
	ec.logger.Error("failed to relay data commitment", "nonce", newNonce, "hash", tx.Hash().String())
	return err
}

func (ec *evmClient) NewTransactOpts(ctx context.Context, gasLim uint64) (*bind.TransactOpts, error) {
	builder := newTransactOptsBuilder(ec.privateKey)

	ethClient, err := ethclient.Dial(ec.evmRPC)
	if err != nil {
		return nil, err
	}

	opts, err := builder(ctx, ethClient, gasLim)
	if err != nil {
		return nil, err
	}
	return opts, nil
}

func (ec *evmClient) StateLastEventNonce(opts *bind.CallOpts) (uint64, error) {
	nonce, err := ec.wrapper.StateEventNonce(opts)
	if err != nil {
		return 0, err
	}
	return nonce.Uint64(), nil
}

func (ec *evmClient) waitForTransaction(
	ctx context.Context,
	tx *coregethtypes.Transaction,
) (*coregethtypes.Receipt, error) {
	ethClient, err := ethclient.Dial(ec.evmRPC)
	if err != nil {
		return nil, err
	}
	defer ethClient.Close()

	return bind.WaitMined(ctx, ethClient, tx)
}

func ethValset(valset types.Valset) ([]wrapper.Validator, error) {
	ethVals := make([]wrapper.Validator, len(valset.Members))
	for i, v := range valset.Members {
		if ok := gethcommon.IsHexAddress(v.EthereumAddress); !ok {
			return nil, errors.New("invalid ethereum address found in validator set")
		}
		addr := gethcommon.HexToAddress(v.EthereumAddress)
		ethVals[i] = wrapper.Validator{
			Addr:  addr,
			Power: big.NewInt(int64(v.Power)),
		}
	}
	return ethVals, nil
}
