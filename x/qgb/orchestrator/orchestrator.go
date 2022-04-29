package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"github.com/tendermint/tendermint/libs/log"
	"math/big"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	ethcmn "github.com/ethereum/go-ethereum/common"
)

type orchestrator struct {
	logger log.Logger
	// TODO this will change once we have the worker pool pattern
	broadcaster Broadcaster

	// orchestrator signing
	evmPrivateKey ecdsa.PrivateKey
	bridgeID      ethcmn.Hash

	// celestia related signing
	orchestratorAddress string
}

func (oc *orchestrator) processValsetEvents(ctx context.Context, valsetChannel <-chan types.Valset) error {
	for valset := range valsetChannel {
		signBytes, err := valset.SignBytes(oc.bridgeID)
		if err != nil {
			oc.logger.Error(fmt.Sprintf("valset nonce %d: %s", valset.Nonce, err.Error()))
			continue
		}

		signature, err := types.NewEthereumSignature(signBytes.Bytes(), &oc.evmPrivateKey)
		if err != nil {
			oc.logger.Error(fmt.Sprintf("valset nonce %d: %s", valset.Nonce, err.Error()))
			continue
		}

		// create and send the valset hash
		msg := &types.MsgValsetConfirm{
			Orchestrator: oc.orchestratorAddress,
			EthAddress:   crypto.PubkeyToAddress(oc.evmPrivateKey.PublicKey).Hex(),
			Nonce:        valset.Nonce,
			Signature:    ethcmn.Bytes2Hex(signature),
		}

		hash, err := oc.broadcaster.BroadcastTx(ctx, msg)
		if err != nil {
			oc.logger.Error(fmt.Sprintf("valset nonce %d: %s", valset.Nonce, err.Error()))
			continue
		}
		oc.logger.Info(fmt.Sprintf("signed Valset %d : %s", msg.Nonce, hash))
	}
	return nil
}

func (oc *orchestrator) processDataCommitmentEvents(
	ctx context.Context,
	dataCommitmentChannel <-chan ExtendedDataCommitment,
) error {
	for dc := range dataCommitmentChannel {
		dataRootHash := types.DataCommitmentTupleRootSignBytes(oc.bridgeID, big.NewInt(int64(dc.Nonce)), dc.Commitment)
		dcSig, err := types.NewEthereumSignature(dataRootHash.Bytes(), &oc.evmPrivateKey)
		if err != nil {
			oc.logger.Error(fmt.Sprintf("data commitment range %d-%d: %s", dc.Start, dc.End, err.Error()))
			continue
		}

		msg := &types.MsgDataCommitmentConfirm{
			EthAddress:       crypto.PubkeyToAddress(oc.evmPrivateKey.PublicKey).Hex(),
			Commitment:       dc.Commitment.String(),
			BeginBlock:       dc.Start,
			EndBlock:         dc.End,
			ValidatorAddress: oc.orchestratorAddress,
			Signature:        ethcmn.Bytes2Hex(dcSig),
		}

		hash, err := oc.broadcaster.BroadcastTx(ctx, msg)
		if err != nil {
			oc.logger.Error(fmt.Sprintf("data commitment range %d-%d: %s", dc.Start, dc.End, err.Error()))
			continue
		}
		oc.logger.Info(fmt.Sprintf("signed commitment %d-%d: %s", msg.BeginBlock, msg.EndBlock, hash))
	}
	return nil
}
