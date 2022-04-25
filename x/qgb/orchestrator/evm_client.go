package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	tmlog "github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

type EVMClient interface {
	UpdateValidatorSet(
		ctx context.Context,
		nonce, threshHold uint64,
		valset types.Valset,
		sigs []wrapper.Signature,
	) error
	SubmitDataRootTupleRoot(
		ctx context.Context,
		tupleRoot common.Hash,
		lastDataCommitmentNonce uint64,
		currentValset types.Valset,
		sigs []wrapper.Signature,
	) error
	StateLastDataRootTupleRootNonce(opts *bind.CallOpts) (uint64, error)
	StateLastValsetNonce(opts *bind.CallOpts) (uint64, error)
}

type evmClient struct {
	logger     tmlog.Logger
	wrapper    wrapper.QuantumGravityBridge
	privateKey *ecdsa.PrivateKey
	evmRpc     string
}

func NewEvmClient(
	logger tmlog.Logger,
	wrapper wrapper.QuantumGravityBridge,
	privateKey *ecdsa.PrivateKey,
	evmRpc string,
) EVMClient {
	return &evmClient{
		logger:     logger,
		wrapper:    wrapper,
		privateKey: privateKey,
		evmRpc:     evmRpc,
	}
}

func (ec *evmClient) UpdateValidatorSet(
	ctx context.Context,
	nonce, newThreshHold uint64,
	valset types.Valset,
	sigs []wrapper.Signature,
) error {
	opts, err := ec.NewTransactOpts(ctx, 1000000)
	if err != nil {
		return err
	}

	ethVals, err := ethValset(valset)
	if err != nil {
		return err
	}

	ethVsHash, err := valset.Hash()
	if err != nil {
		return err
	}

	tx, err := ec.wrapper.UpdateValidatorSet(
		opts,
		big.NewInt(int64(nonce)),
		big.NewInt(int64(newThreshHold)),
		ethVsHash,
		ethVals,
		sigs,
	)
	if err != nil {
		return err
	}
	ec.logger.Info("ValSetUpdate", tx.Hash().String())
	return nil
}

func (ec *evmClient) SubmitDataRootTupleRoot(
	ctx context.Context,
	tupleRoot common.Hash,
	lastDataCommitmentNonce uint64,
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

	// todo: why are we using the last nonce here? shouldn't we just use the new nonce?
	tx, err := ec.wrapper.SubmitDataRootTupleRoot(
		opts,
		big.NewInt(int64(lastDataCommitmentNonce)),
		tupleRoot,
		ethVals,
		sigs,
	)
	if err != nil {
		return err
	}
	ec.logger.Info("DataRootTupleRootUpdated", tx.Hash().String())
	return nil
}

func (ec *evmClient) NewTransactOpts(ctx context.Context, gasLim uint64) (*bind.TransactOpts, error) {
	builder := newTransactOptsBuilder(ec.privateKey)

	ethClient, err := ethclient.Dial(ec.evmRpc)
	if err != nil {
		return nil, err
	}

	opts, err := builder(ctx, ethClient, gasLim)
	if err != nil {
		return nil, err
	}
	return opts, nil
}

func (ec *evmClient) StateLastDataRootTupleRootNonce(opts *bind.CallOpts) (uint64, error) {
	nonce, err := ec.wrapper.StateLastDataRootTupleRootNonce(opts)
	if err != nil {
		return 0, err
	}
	return nonce.Uint64(), nil
}

func (ec *evmClient) StateLastValsetNonce(opts *bind.CallOpts) (uint64, error) {
	nonce, err := ec.wrapper.StateLastValidatorSetNonce(opts)
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
