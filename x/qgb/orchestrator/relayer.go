package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcmn "github.com/ethereum/go-ethereum/common"
)

type relayer struct {
	// client
	querier Querier

	// relayer
	bridgeID      ethcmn.Hash
	evmClient     EVMClient
	relayerClient relayerClient
}

func NewRelayer(querier Querier, evmClient EVMClient, relayerClient relayerClient) (*relayer, error) {
	return &relayer{
		querier:       querier,
		bridgeID:      types.BridgeId,
		evmClient:     evmClient,
		relayerClient: relayerClient,
	}, nil
}

func (r *relayer) processEvents(ctx context.Context) error {
	for {
		lastContractNonce, err := r.relayerClient.evmClient.StateLastEventNonce(&bind.CallOpts{})
		if err != nil {
			r.relayerClient.logger.Error(err.Error())
			continue
		}
		latestNonce, err := r.relayerClient.querier.QueryLatestAttestationNonce(ctx)

		// If the contract has already the last version, no need to relay anything
		if lastContractNonce >= latestNonce {
			time.Sleep(10 * time.Second)
			continue
		}

		// we're incrementing by 1 since we still don't support heights
		// instead of nonce: https://github.com/celestiaorg/quantum-gravity-bridge/issues/104
		att1, err := r.relayerClient.querier.QueryAttestationByNonce(ctx, lastContractNonce+1)
		if err != nil {
			r.relayerClient.logger.Error(err.Error())
			continue
		}
		att := *att1
		if att.Type() == types.ValsetRequestType {
			vs, ok := att.(*types.Valset)
			if !ok {
				return types.ErrAttestationNotValsetRequest
			}
			confirms, err := r.querier.QueryTwoThirdsValsetConfirms(ctx, time.Minute*30, *vs)
			if err != nil {
				return err
			}

			// FIXME: arguments to be verified
			err = r.updateValidatorSet(ctx, *vs, vs.TwoThirdsThreshold(), confirms)
			if err != nil {
				return err
			}
		} else {
			dc, ok := att.(*types.DataCommitment)
			if !ok {
				return types.ErrAttestationNotDataCommitmentRequest
			}
			// todo: make times configurable
			confirms, err := r.querier.QueryTwoThirdsDataCommitmentConfirms(ctx, time.Minute*30, *dc)
			if err != nil {
				return err
			}

			// todo: make gas limit configurable
			valset, err := r.querier.QueryLastValsetBeforeNonce(ctx, dc.Nonce)
			if err != nil {
				return err
			}

			err = r.submitDataRootTupleRoot(ctx, *valset, confirms[0].Commitment, confirms)
			if err != nil {
				return err
			}
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
	var currentValset types.Valset
	if valset.Nonce == 1 {
		currentValset = valset
	} else {
		vs, err := r.querier.QueryLastValsetBeforeNonce(ctx, valset.Nonce-1)
		if err != nil {
			return err
		}
		currentValset = *vs
	}

	sigs, err := matchValsetConfirmSigs(confirms, currentValset)
	if err != nil {
		return err
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

	sigs, err := matchDataCommitmentConfirmSigs(confirms, currentValset)
	if err != nil {
		return err
	}

	// TODO: don't assume that the evm contracts are up to date with the latest nonce
	lastDataCommitmentNonce, err := r.evmClient.StateLastEventNonce(&bind.CallOpts{})
	if err != nil {
		return err
	}

	// increment the nonce before submitting the new tuple root
	newDataCommitmentNonce := lastDataCommitmentNonce + 1

	r.relayerClient.logger.Info(fmt.Sprintf(
		"relaying data commitment %d-%d...",
		confirms[0].BeginBlock, // because the nonce was already incremented
		confirms[0].EndBlock,
	))

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

//
//func matchSigsWithValidators(sigs []wrapper.Signature, valset types.Valset) ([]wrapper.Signature, error) {
//	for validator := range valset.Members {
//		validator
//	}
//}

func matchValsetConfirmSigs(
	confirms []types.MsgValsetConfirm,
	currentValset types.Valset,
) ([]wrapper.Signature, error) {
	confirmsMap := make(map[string]types.MsgValsetConfirm)
	// to fetch the signatures easilly by eth address
	for _, c := range confirms {
		confirmsMap[c.EthAddress] = c
	}

	sigs := make([]wrapper.Signature, len(confirms))
	// the QGB contract expects the signatures to be ordered by validators in valset
	for i, val := range currentValset.Members {
		v, r, s := SigToVRS(confirmsMap[val.EthereumAddress].Signature)

		sigs[i] = wrapper.Signature{
			V: v,
			R: r,
			S: s,
		}
	}

	return sigs, nil
}

// TODO kinda duplicate. refactor
func matchDataCommitmentConfirmSigs(confirms []types.MsgDataCommitmentConfirm, currentValset types.Valset) ([]wrapper.Signature, error) {
	//vals := make(map[string]string)
	//for _, v := range confirms {
	//	vals[v.EthAddress] = v.Signature
	//}

	// create a map to easily search for power
	vals2 := make(map[string]types.BridgeValidator)
	for _, val := range currentValset.Members {
		vals2[val.GetEthereumAddress()] = val
	}

	confirmsMap := make(map[string]types.MsgDataCommitmentConfirm)
	for _, c := range confirms {
		confirmsMap[c.EthAddress] = c
	}

	sigs := make([]wrapper.Signature, len(confirms))
	for i, val := range currentValset.Members {
		validatorEthAddr := val.EthereumAddress
		confirm, has := confirmsMap[validatorEthAddr]
		if !has {
			return nil, fmt.Errorf("missing orchestrator eth address: %s", validatorEthAddr)
		}
		v, r, s := SigToVRS(confirm.Signature)

		sigs[i] = wrapper.Signature{
			V: v,
			R: r,
			S: s,
		}
	}

	//for i, c := range confirms {
	//	sig, has := vals[c.EthAddress]
	//	if !has {
	//		return nil, fmt.Errorf("missing orchestrator eth address: %s", c.EthAddress)
	//	}
	//
	//	v, r, s := SigToVRS(sig)
	//
	//	sigs[i] = wrapper.Signature{
	//		V: v,
	//		R: r,
	//		S: s,
	//	}
	//}
	return sigs, nil
}
