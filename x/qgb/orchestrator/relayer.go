package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethcmn "github.com/ethereum/go-ethereum/common"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
	coretypes "github.com/tendermint/tendermint/types"
)

type relayer struct {
	*client
}

func (r *relayer) relayValsets(ctx context.Context) error {
	results, err := r.tendermintRPC.Subscribe(ctx, "valset-changes", "tm.event='Tx' AND message.module='qgb'")
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-results:
			err = r.processValsetEvents(ctx, ev)
			if err != nil {
				return err
			}
		}
	}
}

func (r *relayer) processValsetEvents(ctx context.Context, ev rpctypes.ResultEvent) error {
	attributes := ev.Events[types.EventTypeValsetRequest]
	for _, attr := range attributes {
		if attr != types.AttributeKeyNonce {
			continue
		}

		queryClient := types.NewQueryClient(r.qgbRPC)

		// query for the latest valset (sorted for us already)
		lastValsetResp, err := queryClient.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
		if err != nil {
			return err
		}

		// todo: double check that the first validator set is found
		if len(lastValsetResp.Valsets) < 1 {
			return errors.New("no validator sets found")
		}

		valset := lastValsetResp.Valsets[0]
		height := int64(valset.Height)

		// we need the validator set hash for this height.
		blockRes, err := r.tendermintRPC.Block(ctx, &height)
		if err != nil {
			return err
		}

		rawVSHash := blockRes.Block.Header.ValidatorsHash.Bytes()
		var ethVSHash ethcmn.Hash
		copy(ethVSHash[:], rawVSHash)

		confirms, err := r.queryTwoThirdsValsetConfirms(ctx, time.Minute*30, queryClient, valset)
		if err != nil {
			return err
		}

		opts, err := r.transactOpsBuilder(ctx, r.ethRPC, 1000000)
		if err != nil {
			return err
		}

		err = r.updateValidatorSet(
			ctx,
			opts,
			valset.Nonce,
			valset.TwoThirdsThreshold(),
			ethVSHash,
			valset,
			confirms,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *relayer) relayDataCommitments(ctx context.Context) error {
	queryClient := types.NewQueryClient(r.qgbRPC)

	resp, err := queryClient.Params(ctx, &types.QueryParamsRequest{})
	if err != nil {
		return err
	}

	params := resp.Params

	results, err := r.tendermintRPC.Subscribe(ctx, "height", coretypes.EventQueryNewBlockHeader.String())
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-results:
			r.processDataCommitmentEvents(ctx, queryClient, params.DataCommitmentWindow, ev)
		}
	}
}

func (r *relayer) processDataCommitmentEvents(
	ctx context.Context,
	client types.QueryClient,
	window uint64,
	msg rpctypes.ResultEvent,
) error {
	eventDataHeader := msg.Data.(coretypes.EventDataNewBlockHeader)
	height := eventDataHeader.Header.Height
	// todo: refactor to ensure that no ranges of blocks are missed if the
	// parameters are changed
	if height%int64(window) != 0 {
		return nil
	}

	// TODO: calculate start height some other way that can handle changes
	// in the data window param
	startHeight := height - int64(window)
	endHeight := height

	// create and send the data commitment
	dcResp, err := r.tendermintRPC.DataCommitment(
		ctx,
		fmt.Sprintf("block.height >= %d AND block.height <= %d",
			startHeight,
			endHeight,
		),
	)
	if err != nil {
		return err
	}

	// query for the latest valset (sorted for us already)
	lastValsetResp, err := client.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
	if err != nil {
		return err
	}

	if len(lastValsetResp.Valsets) < 1 {
		return errors.New("no validator sets found")
	}

	valset := lastValsetResp.Valsets[0]

	// todo, this assumes that the evm chain we are relaying to is update to data, which is not a good assumption
	nonce, err := r.wrapper.StateLastDataRootTupleRootNonce(&bind.CallOpts{})
	if err != nil {
		return err
	}

	nonce.Add(nonce, big.NewInt(1))

	dataRootHash := DataCommitmentTupleRootSignBytes(r.bridgeID, nonce, dcResp.DataCommitment)

	// todo: make times configurable
	confirms, err := r.queryTwoThirdsDataCommitmentConfirms(ctx, time.Minute*30, client, valset, dataRootHash.String())
	if err != nil {
		return err
	}

	opts, err := r.transactOpsBuilder(ctx, r.ethRPC, 1000000)
	if err != nil {
		return err
	}

	return r.submitDataRootTupleRoot(ctx, opts, dataRootHash, valset, confirms)
}

func (r *relayer) queryTwoThirdsDataCommitmentConfirms(ctx context.Context, timeout time.Duration, client types.QueryClient, valset types.Valset, commitment string) ([]types.MsgDataCommitmentConfirm, error) {
	// create a map to easily search for power
	vals := make(map[string]types.BridgeValidator)
	for _, val := range valset.Members {
		vals[val.GetEthereumAddress()] = val
	}

	majThreshHold := valset.TwoThirdsThreshold()

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(timeout):
			return nil, fmt.Errorf("failure to query for majority validator set confirms: timout %s", timeout)
		default:
			currThreshHold := uint64(0)
			confirmsResp, err := client.DataCommitmentConfirmsByCommitment(ctx, &types.QueryDataCommitmentConfirmsByCommitmentRequest{
				Commitment: commitment,
			})
			if err != nil {
				return nil, err
			}

			for _, dataCommitmentConfirm := range confirmsResp.Confirms {
				val, has := vals[dataCommitmentConfirm.EthAddress]
				if !has {
					return nil, fmt.Errorf("dataCommitmentConfirm signer not found in stored validator set: address %s nonce %d", val.EthereumAddress, valset.Nonce)
				}
				currThreshHold += val.Power
			}

			if currThreshHold >= majThreshHold {
				return confirmsResp.Confirms, nil
			}

			r.logger.Debug().Str("foundDataCommitmentConfirms", fmt.Sprintf("total power %d number of confirms %d", currThreshHold, len(confirmsResp.Confirms)))
		}
		time.Sleep(time.Second * 30)
	}
}

func (r *relayer) queryTwoThirdsValsetConfirms(ctx context.Context, timeout time.Duration, client types.QueryClient, valset types.Valset) ([]types.MsgValsetConfirm, error) {
	// create a map to easily search for power
	vals := make(map[string]types.BridgeValidator)
	for _, val := range valset.Members {
		vals[val.GetEthereumAddress()] = val
	}

	majThreshHold := valset.TwoThirdsThreshold()

	for {
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(timeout):
			return nil, fmt.Errorf("failure to query for majority validator set confirms: timout %s", timeout)
		default:
			currThreshHold := uint64(0)
			confirmsResp, err := client.ValsetConfirmsByNonce(ctx, &types.QueryValsetConfirmsByNonceRequest{
				Nonce: valset.Nonce,
			})
			if err != nil {
				return nil, err
			}

			for _, valsetConfirm := range confirmsResp.Confirms {
				val, has := vals[valsetConfirm.EthAddress]
				if !has {
					return nil, fmt.Errorf("valSetConfirm signer not found in stored validator set: address %s nonce %d", val.EthereumAddress, valset.Nonce)
				}
				currThreshHold += val.Power
			}

			if currThreshHold >= majThreshHold {
				return confirmsResp.Confirms, nil
			}

			r.logger.Debug().Str("foundValsetConfirms", fmt.Sprintf("total power %d number of confirms %d", currThreshHold, len(confirmsResp.Confirms)))
		}
		time.Sleep(time.Second * 30)
	}
}

func (r *relayer) updateValidatorSet(
	ctx context.Context,
	opts *bind.TransactOpts,
	nonce uint64,
	newThreshhold uint64,
	newValsetHash common.Hash,
	currentValset types.Valset,
	confirms []types.MsgValsetConfirm,
) error {

	sigs, err := matchValsetConfirmSigs(confirms, currentValset)

	ethVals, err := ethValset(currentValset)
	if err != nil {
		return err
	}

	tx, err := r.wrapper.UpdateValidatorSet(
		opts,
		big.NewInt(int64(currentValset.Nonce)),
		big.NewInt(int64(newThreshhold)),
		newValsetHash,
		ethVals,
		sigs,
	)
	if err != nil {
		return err
	}
	r.logger.Info().Str("ValSetUpdate", tx.Hash().String())
	return nil
}

func (r *relayer) submitDataRootTupleRoot(
	ctx context.Context,
	opts *bind.TransactOpts,
	tupleRoot common.Hash,
	currentValset types.Valset,
	confirms []types.MsgDataCommitmentConfirm,
) error {

	sigs, err := matchDataCommitmentConfirmSigs(confirms, currentValset)
	if err != nil {
		return err
	}

	ethVals, err := ethValset(currentValset)
	if err != nil {
		return err
	}

	lastDataCommitmentNonce, err := r.wrapper.StateLastDataRootTupleRootNonce(&bind.CallOpts{})
	if err != nil {
		return err
	}

	// increment the nonce before submitting the new tuple root
	lastDataCommitmentNonce.Add(lastDataCommitmentNonce, big.NewInt(1))

	tx, err := r.wrapper.SubmitDataRootTupleRoot(
		opts,
		lastDataCommitmentNonce,
		tupleRoot,
		ethVals,
		sigs,
	)
	if err != nil {
		return err
	}
	r.logger.Info().Str("DataRootTupleRootUpdated", tx.Hash().String())
	return nil
}

func matchValsetConfirmSigs(confirms []types.MsgValsetConfirm, valset types.Valset) ([]wrapper.Signature, error) {
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

func matchDataCommitmentConfirmSigs(confirms []types.MsgDataCommitmentConfirm, valset types.Valset) ([]wrapper.Signature, error) {
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
