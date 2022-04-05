package orchestrator

import (
	"context"
	"github.com/rs/zerolog"
	"math/big"

	paytypes "github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	ethcmn "github.com/ethereum/go-ethereum/common"
)

type orchestrator struct {
	logger zerolog.Logger

	// client
	appClient AppClient

	// orchestrator signing
	singerFn           bind.SignerFn
	personalSignerFn   PersonalSignFn
	transactOpsBuilder transactOpsBuilder
	evmAddress         ethcmn.Address
	bridgeID           ethcmn.Hash

	// celestia related signing
	signer *paytypes.KeyringSigner
}

func (oc *orchestrator) processValsetEvents(ctx context.Context, valSetChannel <-chan types.Valset) error {
	for range valSetChannel {
		valset := <-valSetChannel

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

func (oc *orchestrator) processDataCommitmentEvents(ctx context.Context, dataCommitmentChannel <-chan ExtendedDataCommitment) error {
	for range dataCommitmentChannel {
		dc := <-dataCommitmentChannel

		nonce, err := oc.appClient.GetNonce()
		if err != nil {
			return err
		}

		nonce.Add(nonce, big.NewInt(1))

		dataRootHash := types.DataCommitmentTupleRootSignBytes(oc.bridgeID, nonce, dc.Commitment)
		dcSig, err := oc.personalSignerFn(oc.evmAddress, dataRootHash.Bytes())
		if err != nil {
			return err
		}

		msg := &types.MsgDataCommitmentConfirm{
			EthAddress:       oc.evmAddress.String(),
			Commitment:       string(dc.Commitment),
			BeginBlock:       dc.Start,
			EndBlock:         dc.End,
			ValidatorAddress: oc.signer.GetSignerInfo().GetAddress().String(),
			Signature:        ethcmn.Bytes2Hex(dcSig),
		}

		err = oc.broadcastTx(ctx, msg)
		if err != nil {
			return err
		}
	}
	return nil
}
