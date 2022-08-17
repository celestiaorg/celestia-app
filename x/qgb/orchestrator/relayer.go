package orchestrator

import (
	"context"
	"fmt"
	"time"

	"github.com/cosmos/cosmos-sdk/types/errors"
	tmlog "github.com/tendermint/tendermint/libs/log"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcmn "github.com/ethereum/go-ethereum/common"
)

type Relayer struct {
	querier   Querier
	evmClient EVMClient
	bridgeID  ethcmn.Hash
	logger    tmlog.Logger
}

func NewRelayer(querier Querier, evmClient EVMClient, logger tmlog.Logger) (*Relayer, error) {
	return &Relayer{
		querier:   querier,
		bridgeID:  types.BridgeID,
		evmClient: evmClient,
		logger:    logger,
	}, nil
}

func (r *Relayer) processEvents(ctx context.Context) error {
	for {
		lastContractNonce, err := r.evmClient.StateLastEventNonce(&bind.CallOpts{})
		if err != nil {
			r.logger.Error(err.Error())
			continue
		}

		latestNonce, err := r.querier.QueryLatestAttestationNonce(ctx)
		if err != nil {
			r.logger.Error(err.Error())
			continue
		}

		// If the contract has already the last version, no need to relay anything
		if lastContractNonce >= latestNonce {
			time.Sleep(10 * time.Second)
			continue
		}

		att, err := r.querier.QueryAttestationByNonce(ctx, lastContractNonce+1)
		if err != nil {
			r.logger.Error(err.Error())
			continue
		}
		if att == nil {
			r.logger.Error(types.ErrAttestationNotFound.Error())
			continue
		}
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
}

func (r *Relayer) updateValidatorSet(
	ctx context.Context,
	valset types.Valset,
	newThreshhold uint64,
	confirms []types.MsgValsetConfirm,
) error {
	var currentValset types.Valset
	if valset.Nonce == 1 {
		currentValset = valset
	} else {
		vs, err := r.querier.QueryLastValsetBeforeNonce(ctx, valset.Nonce)
		if err != nil {
			return err
		}
		currentValset = *vs
	}

	sigsMap := make(map[string]string)
	// to fetch the signatures easilly by eth address
	for _, c := range confirms {
		sigsMap[c.EthAddress] = c.Signature
	}

	sigs, err := matchAttestationConfirmSigs(sigsMap, currentValset)
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
		true,
	)
	if err != nil {
		return err
	}
	return nil
}

func (r *Relayer) submitDataRootTupleRoot(
	ctx context.Context,
	currentValset types.Valset,
	commitment string,
	confirms []types.MsgDataCommitmentConfirm,
) error {
	sigsMap := make(map[string]string)
	// to fetch the signatures easilly by eth address
	for _, c := range confirms {
		sigsMap[c.EthAddress] = c.Signature
	}

	sigs, err := matchAttestationConfirmSigs(sigsMap, currentValset)
	if err != nil {
		return err
	}

	// the confirm carries the correct nonce to be submitted
	newDataCommitmentNonce := confirms[0].Nonce

	r.logger.Info(fmt.Sprintf(
		"relaying data commitment %d-%d...",
		confirms[0].BeginBlock,
		confirms[0].EndBlock,
	))

	err = r.evmClient.SubmitDataRootTupleRoot(
		ctx,
		ethcmn.HexToHash(commitment),
		newDataCommitmentNonce,
		currentValset,
		sigs,
		true,
	)
	if err != nil {
		return err
	}
	return nil
}

// matchAttestationConfirmSigs matches and sorts the confirm signatures with the valset
// members as expected by the QGB contract.
func matchAttestationConfirmSigs(
	signatures map[string]string,
	currentValset types.Valset,
) ([]wrapper.Signature, error) {
	sigs := make([]wrapper.Signature, len(signatures))
	// the QGB contract expects the signatures to be ordered by validators in valset
	for i, val := range currentValset.Members {
		sig, has := signatures[val.EthereumAddress]
		if !has {
			return nil, errors.Wrap(
				ErrConfirmSignatureNotFound,
				fmt.Sprintf("missing signature for orchestrator eth address: %s", val.EthereumAddress),
			)
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
