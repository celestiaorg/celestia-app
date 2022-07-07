package test

import (
	"errors"
	"math/big"

	"github.com/celestiaorg/celestia-app/x/qgb/orchestrator"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/tendermint/tendermint/crypto/tmhash"
)

func verifyOrchestratorValsetSignature(broadcasted sdk.Msg, valset *types.Valset) error {
	msg := broadcasted.(*types.MsgValsetConfirm)
	if msg == nil {
		return errors.New("couldn't cast sdk.Msg to *types.MsgValsetConfirm")
	}
	hash, err := valset.SignBytes(types.BridgeId)
	if err != nil {
		return err
	}
	ethAddress := common.HexToAddress(msg.EthAddress)
	err = types.ValidateEthereumSignature(
		hash.Bytes(),
		common.Hex2Bytes(msg.Signature),
		ethAddress,
	)
	if err != nil {
		return err
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

func generateDc(nonce int) (types.DataCommitment, error) {
	dc := *types.NewDataCommitment(uint64(nonce), 1, 30)
	return dc, nil
}

func verifyOrchestratorDcSignature(broadcasted sdk.Msg, dc types.DataCommitment) error {
	msg := broadcasted.(*types.MsgDataCommitmentConfirm)
	if msg == nil {
		return errors.New("couldn't cast sdk.Msg to *types.MsgDataCommitmentConfirm")
	}

	dataRootHash := types.DataCommitmentTupleRootSignBytes(
		types.BridgeId,
		big.NewInt(int64(dc.Nonce)),
		commitmentFromQuery(orchestrator.CommitmentQueryByRange(dc.BeginBlock, dc.EndBlock)),
	)
	ethAddress := common.HexToAddress(msg.EthAddress)
	err := types.ValidateEthereumSignature(
		dataRootHash.Bytes(),
		common.Hex2Bytes(msg.Signature),
		ethAddress,
	)
	if err != nil {
		return err
	}
	return nil
}

func commitmentFromQuery(query string) []byte {
	return tmhash.Sum([]byte(query))
}
