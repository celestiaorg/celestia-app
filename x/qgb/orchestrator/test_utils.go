package orchestrator

import (
	"errors"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"strings"
)

func verifyOrchestratorValsetSignatures(broadCasted []sdk.Msg, valsets []*types.Valset, bridgeID common.Hash) error {
	for i := 0; i < len(broadCasted); i++ {
		msg := broadCasted[i].(*types.MsgValsetConfirm)
		if msg == nil {
			return errors.New("couldn't cast sdk.Msg to *types.MsgValsetConfirm")
		}
		hash, err := valsets[i].SignBytes(bridgeID)
		if err != nil {
			return err
		}
		sigPublicKeyECDSA, err := crypto.SigToPub(hash.Bytes(), common.Hex2Bytes(msg.Signature))
		if err != nil {
			return err
		}
		ethAddress := crypto.PubkeyToAddress(*sigPublicKeyECDSA).Hex()
		if strings.Compare(msg.EthAddress, ethAddress) != 0 {
			return errors.New("wrong signature for valset")
		}
	}
	return nil
}

func generateValset(nonce int, ethAddress string) (*types.Valset, error) {
	validators, err := populateValidators(ethAddress)
	if err != nil {
		return nil, err
	}
	valset, err := types.NewValset(
		uint64(nonce),
		uint64(nonce*10),
		validators,
	)
	if err != nil {
		return nil, err
	}
	return valset, err
}

func generateValsets(count int, ethAddress string) ([]*types.Valset, error) {
	valsets := make([]*types.Valset, count)
	for i := 0; i < count; i++ {
		valset, err := generateValset(i, ethAddress)
		if err != nil {
			return nil, err
		}
		valsets[i] = valset
	}
	return valsets, nil
}

func populateValsetChan(valsetChannel chan types.Valset, valsets []*types.Valset) {
	for i := 0; i < len(valsets); i++ {
		valsetChannel <- *valsets[i]
	}
}

func populateValidators(ethAddress string) (types.InternalBridgeValidators, error) {
	validators := make(types.InternalBridgeValidators, 1)
	validator, err := types.NewInternalBridgeValidator(
		types.BridgeValidator{
			Power:           80,
			EthereumAddress: ethAddress,
		})
	if err != nil {
		return nil, err
	}
	validators[0] = validator
	return validators, nil
}

func generateDataCommitments(count int) ([]ExtendedDataCommitment, error) {
	dcs := make([]ExtendedDataCommitment, count)
	for i := 0; i < count; i++ {
		dc, err := generateDc(i)
		if err != nil {
			return nil, err
		}
		dcs[i] = dc
	}
	return dcs, nil
}

func generateDc(nonce int) (ExtendedDataCommitment, error) {
	dc := ExtendedDataCommitment{
		[]byte("test_commitment"),
		1,
		30,
		uint64(nonce),
	}
	return dc, nil
}

func populateDcChan(dcChannel chan ExtendedDataCommitment, dcs []ExtendedDataCommitment) {
	for i := 0; i < len(dcs); i++ {
		dcChannel <- dcs[i]
	}
}

func verifyOrchestratorDcSignatures(broadCasted []sdk.Msg, dcs []ExtendedDataCommitment, bridgeID common.Hash) error {
	for i := 0; i < len(broadCasted); i++ {
		msg := broadCasted[i].(*types.MsgDataCommitmentConfirm)
		if msg == nil {
			return errors.New("couldn't cast sdk.Msg to *types.MsgDataCommitmentConfirm")
		}
		dataRootHash := types.DataCommitmentTupleRootSignBytes(
			bridgeID,
			big.NewInt(int64(dcs[i].Nonce)),
			dcs[i].Commitment,
		)
		sigPublicKeyECDSA, err := crypto.SigToPub(dataRootHash.Bytes(), common.Hex2Bytes(msg.Signature))
		if err != nil {
			return err
		}
		ethAddress := crypto.PubkeyToAddress(*sigPublicKeyECDSA).Hex()
		if strings.Compare(ethAddress, msg.EthAddress) != 0 {
			return errors.New("wrong signature for data commitment")
		}
	}
	return nil
}
