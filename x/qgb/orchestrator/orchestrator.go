package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/tendermint/tendermint/libs/log"
	"math/big"

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
	orchestratorAddress sdk.AccAddress
	orchEthAddress      stakingtypes.EthAddress
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
		msg := types.NewMsgValsetConfirm(
			valset.Nonce,
			oc.orchEthAddress,
			oc.orchestratorAddress,
			ethcmn.Bytes2Hex(signature),
		)

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
		dataRootHash := types.DataCommitmentTupleRootSignBytes(oc.bridgeID, big.NewInt(int64(dc.Data.Nonce)), dc.Commitment)
		dcSig, err := types.NewEthereumSignature(dataRootHash.Bytes(), &oc.evmPrivateKey)
		if err != nil {
			oc.logger.Error(fmt.Sprintf("data commitment range %d-%d: %s", dc.Data.BeginBlock, dc.Data.EndBlock, err.Error()))
			continue
		}

		msg := types.NewMsgDataCommitmentConfirm(
			dc.Commitment.String(),
			ethcmn.Bytes2Hex(dcSig),
			oc.orchestratorAddress,
			oc.orchEthAddress,
			dc.Data.BeginBlock,
			dc.Data.EndBlock,
			dc.Data.Nonce,
		)

		hash, err := oc.broadcaster.BroadcastTx(ctx, msg)
		if err != nil {
			oc.logger.Error(fmt.Sprintf("data commitment range %d-%d: %s", dc.Data.BeginBlock, dc.Data.EndBlock, err.Error()))
			continue
		}
		oc.logger.Info(fmt.Sprintf(
			"signed commitment %d-%d: %s tx hash: %s",
			msg.BeginBlock,
			msg.EndBlock,
			dc.Commitment.String(),
			hash,
		))
	}
	return nil
}
