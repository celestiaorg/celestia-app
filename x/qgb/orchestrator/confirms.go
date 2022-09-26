package orchestrator

import (
	"fmt"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
)

// NewMsgValsetConfirm returns a new msgValSetConfirm.
func NewMsgValsetConfirm(
	nonce uint64,
	ethAddress ethcmn.Address,
	validator sdk.AccAddress,
	signature string,
) *types.MsgValsetConfirm {
	return &types.MsgValsetConfirm{
		Nonce:        nonce,
		Orchestrator: validator.String(),
		EthAddress:   ethAddress.Hex(),
		Signature:    signature,
	}
}

// NewMsgDataCommitmentConfirm creates a new NewMsgDataCommitmentConfirm.
func NewMsgDataCommitmentConfirm(
	commitment string,
	signature string,
	validatorAddress sdk.AccAddress,
	ethAddress ethcmn.Address,
	beginBlock uint64,
	endBlock uint64,
	nonce uint64,
) *types.MsgDataCommitmentConfirm {
	return &types.MsgDataCommitmentConfirm{
		Commitment:       commitment,
		Signature:        signature,
		ValidatorAddress: validatorAddress.String(),
		EthAddress:       ethAddress.Hex(),
		BeginBlock:       beginBlock,
		EndBlock:         endBlock,
		Nonce:            nonce,
	}
}

// DataCommitmentTupleRootSignBytes EncodeDomainSeparatedDataCommitment takes the required input data and
// produces the required signature to confirm a validator set update on the QGB Ethereum contract.
// This value will then be signed before being submitted to Cosmos, verified, and then relayed to Ethereum.
func DataCommitmentTupleRootSignBytes(bridgeID ethcmn.Hash, nonce *big.Int, commitment []byte) ethcmn.Hash {
	var dataCommitment [32]uint8
	copy(dataCommitment[:], commitment)

	// the word 'transactionBatch' needs to be the same as the 'name' above in the DataCommitmentConfirmABIJSON
	// but other than that it's a constant that has no impact on the output. This is because
	// it gets encoded as a function name which we must then discard.
	bytes, err := types.InternalQGBabi.Pack(
		"domainSeparateDataRootTupleRoot",
		bridgeID,
		types.DcDomainSeparator,
		nonce,
		dataCommitment,
	)
	// this should never happen outside of test since any case that could crash on encoding
	// should be filtered above.
	if err != nil {
		panic(fmt.Sprintf("Error packing checkpoint! %s/n", err))
	}

	// we hash the resulting encoded bytes discarding the first 4 bytes these 4 bytes are the constant
	// method name 'checkpoint'. If you where to replace the checkpoint constant in this code you would
	// then need to adjust how many bytes you truncate off the front to get the output of abi.encode()
	hash := crypto.Keccak256Hash(bytes[4:])
	return hash
}
