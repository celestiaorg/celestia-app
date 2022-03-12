package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/celestiaorg/quantum-gravity-bridge/orchestrator/ethereum/committer"
	"github.com/celestiaorg/quantum-gravity-bridge/orchestrator/ethereum/provider"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/rs/zerolog"
	coretypes "github.com/tendermint/tendermint/types"
)

type relayer struct {
	orchClient

	logger zerolog.Logger
	committer.EVMCommitter

	ethProvider provider.EVMProvider
	address     common.Address
	wrapper     *wrapper.QuantumGravityBridge
}

func (r *relayer) relayValsets(opts *bind.TransactOpts) error {
	r.wg.Add(1)
	defer r.wg.Done()
	results, err := r.tendermintRPC.Subscribe(r.ctx, "valset-changes", "tm.event='Tx' AND message.module='qgb'")
	if err != nil {
		return err
	}
	for ev := range results {
		attributes := ev.Events[types.EventTypeValsetRequest]
		for _, attr := range attributes {
			if attr != types.AttributeKeyNonce {
				continue
			}

			queryClient := types.NewQueryClient(r.qgbRPC)

			// query for the latest valset (sorted for us already)
			lastValsetResp, err := queryClient.LastValsetRequests(r.ctx, &types.QueryLastValsetRequestsRequest{})
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
			blockRes, err := r.tendermintRPC.Block(r.ctx, &height)
			if err != nil {
				return err
			}

			rawVSHash := blockRes.Block.Header.ValidatorsHash.Bytes()
			var ethVSHash ethcmn.Hash
			copy(ethVSHash[:], rawVSHash)

			confirms, err := r.queryTwoThirdsValsetConfirms(r.ctx, time.Minute*30, queryClient, valset)
			if err != nil {
				return err
			}

			err = r.updateValidatorSet(
				r.ctx,
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
	}
	return nil
}

func (r *relayer) relayDataCommitments() error {
	r.wg.Add(1)
	defer r.wg.Done()

	queryClient := types.NewQueryClient(r.qgbRPC)

	resp, err := queryClient.Params(r.ctx, &types.QueryParamsRequest{})
	if err != nil {
		return err
	}

	params := resp.Params

	results, err := r.tendermintRPC.Subscribe(r.ctx, "height", coretypes.EventQueryNewBlockHeader.String())
	if err != nil {
		return err
	}
	for msg := range results {
		eventDataHeader := msg.Data.(coretypes.EventDataNewBlockHeader)
		height := eventDataHeader.Header.Height
		// todo: refactor to ensure that no ranges of blocks are missed if the
		// parameters are changed
		if height%int64(params.DataCommitmentWindow) != 0 {
			continue
		}

		// TODO: calculate start height some other way that can handle changes
		// in the data window param
		startHeight := height - int64(params.DataCommitmentWindow)
		endHeight := height

		// create and send the data commitment
		dcResp, err := r.tendermintRPC.DataCommitment(
			r.ctx,
			fmt.Sprintf("block.height >= %d AND block.height <= %d",
				startHeight,
				endHeight,
			),
		)
		if err != nil {
			return err
		}

		// query for the latest valset (sorted for us already)
		lastValsetResp, err := queryClient.LastValsetRequests(r.ctx, &types.QueryLastValsetRequestsRequest{})
		if err != nil {
			return err
		}

		// todo: double check that the first validator set is found
		if len(lastValsetResp.Valsets) < 1 {
			return errors.New("no validator sets found")
		}

		valset := lastValsetResp.Valsets[0]

		dataRootHash := EncodeDataCommitmentConfirm(r.bridgeID, valset.Nonce, dcResp.DataCommitment)

		r.queryTwoThirdsDataCommitmentConfirms(r.ctx, time.Minute*30, queryClient, valset, dataRootHash.String())

	}
	return nil
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
			confirmsResp, err := client.DataCommitmentConfirmsByCommitment(r.ctx, &types.QueryDataCommitmentConfirmsByCommitmentRequest{
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
			confirmsResp, err := client.ValsetConfirmsByNonce(r.ctx, &types.QueryValsetConfirmsByNonceRequest{
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
	nonce uint64,
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

	tx, err := r.wrapper.SubmitMessageTupleRoot(
		opts,
		big.NewInt(int64(currentValset.Nonce)), //TODO: actually use the correct nonce here!!!
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

		v, r, s := sigToVRS(sig)

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

		v, r, s := sigToVRS(sig)

		sigs[i] = wrapper.Signature{
			V: v,
			R: r,
			S: s,
		}
	}
	return sigs, nil
}

func sigToVRS(sigHex string) (v uint8, r, s common.Hash) {
	signatureBytes := common.FromHex(sigHex)
	vParam := signatureBytes[64]
	if vParam == byte(0) {
		vParam = byte(27)
	} else if vParam == byte(1) {
		vParam = byte(28)
	}

	v = vParam
	r = common.BytesToHash(signatureBytes[0:32])
	s = common.BytesToHash(signatureBytes[32:64])

	return
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

// Gets the latest validator set nonce
func (r *relayer) GetValsetNonce(
	ctx context.Context,
	callerAddress common.Address,
) (*big.Int, error) {

	nonce, err := r.wrapper.StateLastValidatorSetNonce(&bind.CallOpts{
		From:    callerAddress,
		Context: ctx,
	})

	if err != nil {
		err = fmt.Errorf("StateLastValsetNonce call failed: %w", err)
		return nil, err
	}

	return nonce, nil
}

// Gets the bridgeID
func (r *relayer) GetBridgeID(
	ctx context.Context,
	callerAddress common.Address,
) (common.Hash, error) {

	qgbID, err := r.wrapper.BRIDGEID(&bind.CallOpts{
		From:    callerAddress,
		Context: ctx,
	})

	if err != nil {
		err = fmt.Errorf("BRIDGEID call failed: %w", err)
		return common.Hash{}, err
	}

	return qgbID, nil
}
