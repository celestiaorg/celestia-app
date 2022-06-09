package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcmn "github.com/ethereum/go-ethereum/common"
)

type relayer struct {
	// client
	querier Querier

	// relayer
	bridgeID  ethcmn.Hash
	evmClient EVMClient
}

func NewRelayer(querier Querier, evmClient EVMClient) (*relayer, error) {
	return &relayer{
		querier:   querier,
		bridgeID:  types.BridgeId,
		evmClient: evmClient,
	}, nil
}

func (r *relayer) processValsetEvents(ctx context.Context, valSetChannel <-chan types.Valset) error {
	for valset := range valSetChannel {

		confirms, err := r.querier.QueryTwoThirdsValsetConfirms(ctx, time.Minute*30, valset)
		if err != nil {
			return err
		}

		// FIXME: arguments to be verified
		err = r.updateValidatorSet(ctx, valset, valset.TwoThirdsThreshold(), confirms)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *relayer) processDataCommitmentEvents(
	ctx context.Context,
	dataCommitmentChannel <-chan ExtendedDataCommitment,
) error {
	for dc := range dataCommitmentChannel {

		// todo: make times configurable
		confirms, err := r.querier.QueryTwoThirdsDataCommitmentConfirms(ctx, time.Minute*30, dc)
		if err != nil {
			return err
		}

		// todo: make gas limit configurable
		valset, err := r.querier.QueryLastValsetBeforeHeight(ctx, dc.Data.EndBlock)
		if err != nil {
			return err
		}

		err = r.submitDataRootTupleRoot(ctx, *valset, dc.Commitment.String(), confirms)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *relayer) updateValidatorSet(
	ctx context.Context,
	valset types.Valset,
	newThreshhold uint64,
	confirms []types.MsgValsetConfirm,
) error {
	sigs, err := matchValsetConfirmSigs(confirms)
	if err != nil {
		return err
	}

	var currentValset types.Valset
	if valset.Nonce == 1 {
		currentValset = valset
	} else {
		vs, err := r.querier.QueryValsetByNonce(ctx, valset.Nonce-1)
		if err != nil {
			return err
		}
		currentValset = *vs
	}

	err = r.evmClient.UpdateValidatorSet(
		ctx,
		valset.Nonce,
		newThreshhold,
		currentValset,
		valset,
		sigs,
	)
	if err != nil {
		return err
	}
	return nil
}

func (r *relayer) submitDataRootTupleRoot(
	ctx context.Context,
	currentValset types.Valset,
	commitment string,
	confirms []types.MsgDataCommitmentConfirm,
) error {

	sigs, err := matchDataCommitmentConfirmSigs(confirms)
	if err != nil {
		return err
	}

	// TODO: don't assume that the evm contracts are up to date with the latest nonce
	lastDataCommitmentNonce, err := r.evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{})
	if err != nil {
		return err
	}

	// increment the nonce before submitting the new tuple root
	newDataCommitmentNonce := lastDataCommitmentNonce + 1

	err = r.evmClient.SubmitDataRootTupleRoot(
		ctx,
		ethcmn.HexToHash(commitment),
		newDataCommitmentNonce,
		currentValset,
		sigs,
	)
	if err != nil {
		return err
	}
	return nil
}

func matchValsetConfirmSigs(confirms []types.MsgValsetConfirm) ([]wrapper.Signature, error) {
	vals := make(map[string]string)
	for _, v := range confirms {
		vals[v.EthAddress] = v.Signature
	}

	sigs := make([]wrapper.Signature, len(confirms))
	for i, c := range confirms {
		sig, has := vals[c.EthAddress]
		if !has {
			return nil, fmt.Errorf("missing orchestrator eth address: %s", c.EthAddress)
		}

		v, r, s := SigToVRS(sig)

		sigs[i] = wrapper.Signature{
			V: v,
			R: r,
			S: s,
		}
	}
	return sigs, nil
}

func matchDataCommitmentConfirmSigs(confirms []types.MsgDataCommitmentConfirm) ([]wrapper.Signature, error) {
	vals := make(map[string]string)
	for _, v := range confirms {
		vals[v.EthAddress] = v.Signature
	}

	sigs := make([]wrapper.Signature, len(confirms))
	for i, c := range confirms {
		sig, has := vals[c.EthAddress]
		if !has {
			return nil, fmt.Errorf("missing orchestrator eth address: %s", c.EthAddress)
		}

		v, r, s := SigToVRS(sig)

		sigs[i] = wrapper.Signature{
			V: v,
			R: r,
			S: s,
		}
	}
	return sigs, nil
}
