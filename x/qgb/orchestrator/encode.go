package orchestrator

import (
	"fmt"
	"math/big"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// EncodeDomainSeparatedDataCommitment takes the required input data and produces the required signature to confirm a validator
// set update on the Peggy Ethereum contract. This value will then be signed before being
// submitted to Cosmos, verified, and then relayed to Ethereum
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

// SigToVRS breaks apart a signature into its components to make it compatible with the contracts
func SigToVRS(sigHex string) (v uint8, r, s ethcmn.Hash) {
	signatureBytes := ethcmn.FromHex(sigHex)
	vParam := signatureBytes[64]
	if vParam == byte(0) {
		vParam = byte(27)
	} else if vParam == byte(1) {
		vParam = byte(28)
	}

	v = vParam
	r = ethcmn.BytesToHash(signatureBytes[0:32])
	s = ethcmn.BytesToHash(signatureBytes[32:64])

	return
}
