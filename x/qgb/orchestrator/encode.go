package orchestrator

import (
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	"github.com/celestiaorg/quantum-gravity-bridge/orchestrator/ethereum/peggy"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

// EncodeValsetConfirm takes the required input data and produces the required signature to confirm a validator
// set update on the Peggy Ethereum contract. This value will then be signed before being
// submitted to Cosmos, verified, and then relayed to Ethereum
func EncodeValsetConfirm(valset *types.Valset) common.Hash {
	// error case here should not occur outside of testing since the above is a constant
	// todo: update the abi used
	contractAbi, abiErr := abi.JSON(strings.NewReader(peggy.ValsetCheckpointABIJSON))
	if abiErr != nil {
		log.Fatalln("bad ABI constant")
	}

	checkpointBytes := []uint8("checkpoint")
	var checkpoint [32]uint8
	copy(checkpoint[:], checkpointBytes)

	memberAddresses := make([]common.Address, len(valset.Members))
	convertedPowers := make([]*big.Int, len(valset.Members))
	for i, m := range valset.Members {
		memberAddresses[i] = common.HexToAddress(m.EthereumAddress)
		convertedPowers[i] = big.NewInt(int64(m.Power))
	}

	// the word 'checkpoint' needs to be the same as the 'name' above in the checkpointAbiJson
	// but other than that it's a constant that has no impact on the output. This is because
	// it gets encoded as a function name which we must then discard.
	bytes, packErr := contractAbi.Pack(
		"checkpoint",
		checkpoint,
		big.NewInt(int64(valset.Nonce)),
		memberAddresses,
		convertedPowers,
	)
	// this should never happen outside of test since any case that could crash on encoding
	// should be filtered above.
	if packErr != nil {
		panic(fmt.Sprintf("Error packing checkpoint! %s/n", packErr))
	}

	// we hash the resulting encoded bytes discarding the first 4 bytes these 4 bytes are the constant
	// method name 'checkpoint'. If you where to replace the checkpoint constant in this code you would
	// then need to adjust how many bytes you truncate off the front to get the output of abi.encode()
	hash := crypto.Keccak256Hash(bytes[4:])
	return hash
}
