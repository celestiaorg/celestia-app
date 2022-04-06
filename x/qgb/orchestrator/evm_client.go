package orchestrator

import (
	"context"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
)

type EVMClient interface {
	UpdateValidatorSet(ctx context.Context, nonce, threshhold uint64, valset types.Valset, sigs []wrapper.Signature) error
	SubmitDataRootTupleRoot(ctx context.Context, nonce uint64, valset types.Valset, sigs []wrapper.Signature) error
	NewTransactOpts(ctx context.Context, gasLim uint64) (*bind.TransactOpts, error)
	StateLastDataRootTupleRootNonce(opts *bind.CallOpts) (uint64, error)
}

type evmClient struct {
	wrapper wrapper.QuantumGravityBridge
}

func NewEvmClient(wrapper wrapper.QuantumGravityBridge) EVMClient {
	return &evmClient{
		wrapper: wrapper,
	}
}

func (ec *evmClient) UpdateValidatorSet(ctx context.Context, nonce, threshhold uint64, valset types.Valset, sigs []wrapper.Signature) error {
	return nil
}

func (ec *evmClient) SubmitDataRootTupleRoot(ctx context.Context, nonce uint64, valset types.Valset, sigs []wrapper.Signature) error {
	return nil
}

func (ec *evmClient) NewTransactOpts(gasLim uint64) (*bind.TransactOpts, error) {
	return nil, nil
}

func (ec *evmClient) StateLastDataRootTupleRootNonce(opts *bind.CallOpts) (uint64, error) {
	return 0, nil
}
