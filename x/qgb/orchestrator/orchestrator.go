package orchestrator

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/crypto"

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

		signature, err := types.NewEthereumSignature(signBytes.Bytes(), &oc.evmPrivateKey)
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

		hash, err := oc.appClient.BroadcastTx(ctx, msg)
		if err != nil {
			return err
		}
		fmt.Printf("\nsigned Valset %d : %s\n", msg.Nonce, hash)
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
			return err
		}

		msg := &types.MsgDataCommitmentConfirm{
			EthAddress:       crypto.PubkeyToAddress(oc.evmPrivateKey.PublicKey).Hex(),
			Commitment:       dc.Commitment.String(),
			BeginBlock:       dc.Start,
			EndBlock:         dc.End,
			ValidatorAddress: oc.orchestratorAddress,
			Signature:        ethcmn.Bytes2Hex(dcSig),
		}

		hash, err := oc.appClient.BroadcastTx(ctx, msg)
		if err != nil {
			return err
		}
		fmt.Printf("\nsigned commitment %d-%d: %s\n", msg.BeginBlock, msg.EndBlock, hash)
	}
	return nil
}
