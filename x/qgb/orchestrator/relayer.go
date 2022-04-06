package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"github.com/rs/zerolog"
	"math/big"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethcmn "github.com/ethereum/go-ethereum/common"
)

type relayer struct {
	logger zerolog.Logger

	// client
	appClient AppClient

	// relayer
	bridgeID  ethcmn.Hash
	evmClient EVMClient
}

func (r *relayer) processValsetEvents(ctx context.Context, valSetChannel <-chan types.Valset) error {
	for range valSetChannel {
		valset := <-valSetChannel

		confirms, err := r.appClient.QueryTwoThirdsValsetConfirms(ctx, time.Minute*30, valset)
		if err != nil {
			return err
		}

		// FIXME: arguments to be verified
		err = r.updateValidatorSet(ctx, valset, valset.TwoThirdsThreshold(), valset, confirms)
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
	for range dataCommitmentChannel {
		dc := <-dataCommitmentChannel

		nonce := dc.Nonce + 1
		dataRootHash := types.DataCommitmentTupleRootSignBytes(r.bridgeID, big.NewInt(int64(nonce)), dc.Commitment)
		// todo: make times configurable
		confirms, err := r.appClient.QueryTwoThirdsDataCommitmentConfirms(ctx, time.Minute*30, dataRootHash.String())
		if err != nil {
			return err
		}

		// todo: make gas limit configurable
		valset, err := r.appClient.QueryLatestValset(ctx)
		if err != nil {
			return err
		}

		return r.submitDataRootTupleRoot(ctx, valset, confirms)
	}
	return nil
}

func (r *relayer) updateValidatorSet(
	ctx context.Context,
	valset types.Valset,
	newThreshhold uint64,
	currentValset types.Valset,
	confirms []types.MsgValsetConfirm,
) error {

	sigs, err := matchValsetConfirmSigs(confirms)
	if err != nil {
		return err
	}

	err = r.evmClient.UpdateValidatorSet(
		ctx,
		currentValset.Nonce,
		newThreshhold,
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
	confirms []types.MsgDataCommitmentConfirm,
) error {

	sigs, err := matchDataCommitmentConfirmSigs(confirms)
	if err != nil {
		return err
	}

	lastDataCommitmentNonce, err := r.evmClient.StateLastDataRootTupleRootNonce(&bind.CallOpts{})
	if err != nil {
		return err
	}

	// increment the nonce before submitting the new tuple root
	newDataCommitmentNonce := lastDataCommitmentNonce + 1

	err = r.evmClient.SubmitDataRootTupleRoot(
		ctx,
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
