package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	ethcmn "github.com/ethereum/go-ethereum/common"
	tmlog "github.com/tendermint/tendermint/libs/log"
)

type orchestrator struct {
	logger tmlog.Logger

	// client
	appClient AppClient

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
			return err
		}

		signature, err := crypto.Sign(signBytes.Bytes(), &oc.evmPrivateKey)
		if err != nil {
			return err
		}

		// create and send the valset hash
		msg := &types.MsgValsetConfirm{
			Orchestrator: oc.orchestratorAddress,
			EthAddress:   crypto.PubkeyToAddress(oc.evmPrivateKey.PublicKey).Hex(),
			Nonce:        valset.Nonce,
			Signature:    ethcmn.Bytes2Hex(signature),
		}

		err = oc.appClient.BroadcastTx(ctx, msg)
		if err != nil {
			return err
		}
	}
	return nil
}

func (oc *orchestrator) processDataCommitmentEvents(ctx context.Context, dataCommitmentChannel <-chan ExtendedDataCommitment) error {
	for dc := range dataCommitmentChannel {
		dataRootHash := types.DataCommitmentTupleRootSignBytes(oc.bridgeID, big.NewInt(int64(dc.Nonce)), dc.Commitment)
		dcSig, err := crypto.Sign(dataRootHash.Bytes(), &oc.evmPrivateKey)
		if err != nil {
			return err
		}

		msg := &types.MsgDataCommitmentConfirm{
			EthAddress:       crypto.PubkeyToAddress(oc.evmPrivateKey.PublicKey).Hex(),
			Commitment:       string(dc.Commitment),
			BeginBlock:       dc.Start,
			EndBlock:         dc.End,
			ValidatorAddress: oc.orchestratorAddress,
			Signature:        ethcmn.Bytes2Hex(dcSig),
		}

		err = oc.appClient.BroadcastTx(ctx, msg)
		if err != nil {
			return err
		}
	}
	return nil
}
