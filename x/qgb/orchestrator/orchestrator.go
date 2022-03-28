package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcmn "github.com/ethereum/go-ethereum/common"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
	coretypes "github.com/tendermint/tendermint/types"
)

type orchestrator struct {
	*client
}

func (oc *orchestrator) orchestrateValsets(ctx context.Context) error {
	results, err := oc.tendermintRPC.Subscribe(ctx, "valset-changes", "tm.event='Tx' AND message.module='qgb'")
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-results:
			err = oc.processValsetEvents(ctx, ev)
			if err != nil {
				return err
			}
		}
	}
}

func (oc *orchestrator) processValsetEvents(ctx context.Context, ev rpctypes.ResultEvent) error {
	attributes := ev.Events[types.EventTypeValsetRequest]
	for _, attr := range attributes {
		if attr != types.AttributeKeyNonce {
			continue
		}

		queryClient := types.NewQueryClient(oc.qgbRPC)

		lastValsetResp, err := queryClient.LastValsetRequests(ctx, &types.QueryLastValsetRequestsRequest{})
		if err != nil {
			return err
		}

		// todo: double check that the first validator set is found
		if len(lastValsetResp.Valsets) < 1 {
			return errors.New("no validator sets found")
		}

		valset := lastValsetResp.Valsets[0]

		signBytes, err := valset.SignBytes(oc.bridgeID)
		if err != nil {
			return err
		}

		signature, err := oc.personalSignerFn(oc.evmAddress, signBytes.Bytes())
		if err != nil {
			return err
		}

		// create and send the valset hash
		msg := &types.MsgValsetConfirm{
			Orchestrator: oc.signer.GetSignerInfo().GetAddress().String(),
			EthAddress:   oc.evmAddress.Hex(),
			Nonce:        valset.Nonce,
			Signature:    ethcmn.Bytes2Hex(signature),
		}

		err = oc.broadcastTx(ctx, msg)
		if err != nil {
			return err
		}
	}
	return nil
}

func (oc *orchestrator) orchestrateDataCommitments(ctx context.Context) error {
	queryClient := types.NewQueryClient(oc.qgbRPC)

	resp, err := queryClient.Params(ctx, &types.QueryParamsRequest{})
	if err != nil {
		return err
	}

	params := resp.Params

	results, err := oc.tendermintRPC.Subscribe(ctx, "height", coretypes.EventQueryNewBlockHeader.String())
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev := <-results:
			oc.processDataCommitmentEvents(ctx, params.DataCommitmentWindow, ev)
		}
	}
}

func (oc *orchestrator) processDataCommitmentEvents(ctx context.Context, window uint64, ev rpctypes.ResultEvent) error {
	eventDataHeader := ev.Data.(coretypes.EventDataNewBlockHeader)
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
	dcResp, err := oc.tendermintRPC.DataCommitment(
		ctx,
		fmt.Sprintf("block.height >= %d AND block.height <= %d",
			startHeight,
			endHeight,
		),
	)
	if err != nil {
		return err
	}

	nonce, err := oc.wrapper.StateLastDataRootTupleRootNonce(&bind.CallOpts{})
	if err != nil {
		return err
	}

	nonce.Add(nonce, big.NewInt(1))

	dataRootHash := DataCommitmentTupleRootSignBytes(oc.bridgeID, nonce, dcResp.DataCommitment)

	dcSig, err := oc.personalSignerFn(oc.evmAddress, dataRootHash.Bytes())
	if err != nil {
		return err
	}

	msg := &types.MsgDataCommitmentConfirm{
		EthAddress:       oc.evmAddress.String(),
		Commitment:       dcResp.DataCommitment.String(),
		BeginBlock:       startHeight,
		EndBlock:         endHeight,
		ValidatorAddress: oc.signer.GetSignerInfo().GetAddress().String(),
		Signature:        ethcmn.Bytes2Hex(dcSig),
	}

	return oc.broadcastTx(ctx, msg)
}
