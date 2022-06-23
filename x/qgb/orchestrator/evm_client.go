package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	tmlog "github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

var _ EVMClient = &evmClient{}

type EVMClient interface {
	UpdateValidatorSet(
		ctx context.Context,
		newNonce, newThreshHold uint64,
		currentValset, newValset types.Valset,
		sigs []wrapper.Signature,
	) error
	SubmitDataRootTupleRoot(
		ctx context.Context,
		tupleRoot common.Hash,
		lastDataCommitmentNonce uint64,
		currentValset types.Valset,
		sigs []wrapper.Signature,
	) error
	StateLastEventNonce(opts *bind.CallOpts) (uint64, error)
}

type evmClient struct {
	logger     tmlog.Logger
	wrapper    wrapper.QuantumGravityBridge
	privateKey *ecdsa.PrivateKey
	evmRPC     string
}

func NewEvmClient(
	logger tmlog.Logger,
	wrapper wrapper.QuantumGravityBridge,
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

func (ec *evmClient) UpdateValidatorSet(
	ctx context.Context,
	newNonce, newThreshHold uint64,
	currentValset, newValset types.Valset,
	sigs []wrapper.Signature,
) error {
	// TODO in addition to the nonce, log more interesting information
	ec.logger.Info(fmt.Sprintf("relaying valset %d...", newNonce))
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

	// TODO put this in a separate function and listen for new EVM blocks instead of just sleeping
	for i := 0; i < 60; i++ {
		ec.logger.Debug(fmt.Sprintf("waiting for valset %d to be confirmed: %s", newNonce, tx.Hash().String()))
		lastNonce, err := ec.StateLastEventNonce(&bind.CallOpts{Context: ctx})
		if err != nil {
			return err
		}
		if lastNonce == newNonce {
			ec.logger.Info(fmt.Sprintf("relayed valset %d: %s", newNonce, tx.Hash().String()))
			return nil
		}
		time.Sleep(10 * time.Second)
	}

	ec.logger.Error(fmt.Sprintf("failed valset %d: %s", newNonce, tx.Hash().String()))
	return nil
}

func (ec *evmClient) SubmitDataRootTupleRoot(
	ctx context.Context,
	tupleRoot common.Hash,
	newNonce uint64,
	currentValset types.Valset,
	sigs []wrapper.Signature,
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

	// TODO put this in a separate function and listen for new EVM blocks instead of just sleeping
	for i := 0; i < 60; i++ {
		ec.logger.Debug(fmt.Sprintf(
			"waiting for data commitment to be confirmed: %s",
			tx.Hash().String(),
		))
		lastNonce, err := ec.StateLastEventNonce(&bind.CallOpts{Context: ctx})
		if err != nil {
			return err
		}
		if lastNonce == newNonce {
			ec.logger.Info(fmt.Sprintf(
				"relayed data commitment: %s",
				tx.Hash().String(),
			))
			return nil
		}
		time.Sleep(10 * time.Second)
	}
	ec.logger.Error(
		fmt.Sprintf(
			"failed to relay data commitment: %s",
			tx.Hash().String(),
		),
	)
	return nil
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

func ethValset(valset types.Valset) ([]wrapper.Validator, error) {
	ethVals := make([]wrapper.Validator, len(valset.Members))
	for i, v := range valset.Members {
		if ok := common.IsHexAddress(v.EthereumAddress); !ok {
			return nil, errors.New("invalid ethereum address found in validator set")
		}
		addr := common.HexToAddress(v.EthereumAddress)
		ethVals[i] = wrapper.Validator{
			Addr:  addr,
			Power: big.NewInt(int64(v.Power)),
		}
	}
	return ethVals, nil
}
