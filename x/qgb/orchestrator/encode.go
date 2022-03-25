package orchestrator

import (
	"fmt"
	"log"
	"math/big"
	"strings"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

const (
	// ethSignPrefix is used to mimic the expected encoding. see
	// https://github.com/OpenZeppelin/openzeppelin-contracts/blob/c9f328ef66251db7fac7c704dd6c5523fc53b0ab/contracts/cryptography/ECDSA.sol#L69-L82 //nolint:lll
	ethSignPrefix = "\x19Ethereum Signed Message:\n32"

	// internalQGBABIJSON is the json encoded abi for private functions in the
	// qgb contract. This is needed to encode data that is signed over in a way
	// that the contracts can easily verify.
	internalQGBABIJSON = `[
			{
			"inputs": [
				{
				"components": [
					{
					"internalType": "address",
					"name": "addr",
					"type": "address"
					},
					{
					"internalType": "uint256",
					"name": "power",
					"type": "uint256"
					}
				],
				"internalType": "struct Validator[]",
				"name": "_validators",
				"type": "tuple[]"
				}
			],
			"name": "computeValidatorSetHash",
			"outputs": [
				{
				"internalType": "bytes32",
				"name": "",
				"type": "bytes32"
				}
			],
			"stateMutability": "pure",
			"type": "function"
			},
			{
			"inputs": [
				{
				"internalType": "bytes32",
				"name": "_bridge_id",
				"type": "bytes32"
				},
				{
				"internalType": "bytes32",
				"name": "_separator",
				"type": "bytes32"
				},
				{
				"internalType": "uint256",
				"name": "_nonce",
				"type": "uint256"
				},
				{
				"internalType": "bytes32",
				"name": "_dataRootTupleRoot",
				"type": "bytes32"
				}
			],
			"name": "domainSeparateDataRootTupleRoot",
			"outputs": [
				{
				"internalType": "bytes32",
				"name": "",
				"type": "bytes32"
				}
			],
			"stateMutability": "pure",
			"type": "function"
			},
			{
			"inputs": [
				{
				"internalType": "bytes32",
				"name": "_bridge_id",
				"type": "bytes32"
				},
				{
				"internalType": "bytes32",
				"name": "_separator",
				"type": "bytes32"
				},
				{
				"internalType": "uint256",
				"name": "_nonce",
				"type": "uint256"
				},
				{
				"internalType": "uint256",
				"name": "_powerThreshold",
				"type": "uint256"
				},
				{
				"internalType": "bytes32",
				"name": "_validatorSetHash",
				"type": "bytes32"
				}
			],
			"name": "domainSeparateValidatorSetHash",
			"outputs": [
				{
				"internalType": "bytes32",
				"name": "",
				"type": "bytes32"
				}
			],
			"stateMutability": "pure",
			"type": "function"
			}
		]`

	// Domain separator constants copied directly from the contracts
	ValidatorSetDomainSeparator   = "0x636865636b706f696e7400000000000000000000000000000000000000000000"
	DataCommitmentDomainSeparator = "0x7472616e73616374696f6e426174636800000000000000000000000000000000"
)

var (
	qgbContractABI abi.ABI
	internalQGBABI abi.ABI
	validatorArgs  abi.Arguments

	VsDomainSeparator ethcmn.Hash
	DcDomainSeparator ethcmn.Hash
)

func init() {
	contractAbi, err := abi.JSON(strings.NewReader(wrapper.QuantumGravityBridgeMetaData.ABI))
	if err != nil {
		log.Fatalln("bad ABI constant", err)
	}
	qgbContractABI = contractAbi

	internalABI, err := abi.JSON(strings.NewReader(internalQGBABIJSON))
	if err != nil {
		log.Fatalln("bad internal ABI constant", err)
	}
	internalQGBABI = internalABI

	solValidatorType, err := abi.NewType("tuple", "validator", []abi.ArgumentMarshaling{
		{Name: "Addr", Type: "address"},
		{Name: "Power", Type: "uint256"},
	})
	if err != nil {
		panic(err)
	}

	validatorArgs = abi.Arguments{
		{Type: solValidatorType, Name: "Validator"},
	}

	// create the domain separator for valset hashes
	VsDomainSeparator = ethcmn.HexToHash(ValidatorSetDomainSeparator)
	DcDomainSeparator = ethcmn.HexToHash(DataCommitmentDomainSeparator)

}

// ValsetSignBytes takes the required input data and produces the required signature to confirm a validator
// set update on the Peggy Ethereum contract. This value will then be signed before being
// submitted to Cosmos, verified, and then relayed to Ethereum
func ValsetSignBytes(bridgeID common.Hash, valset types.Valset) (common.Hash, error) {
	vsHash, err := ComputeValSetHash(valset)
	if err != nil {
		return ethcmn.Hash{}, err
	}

	// the word 'checkpoint' needs to be the same as the 'name' above in the checkpointAbiJson
	// but other than that it's a constant that has no impact on the output. This is because
	// it gets encoded as a function name which we must then discard.
	bytes, err := internalQGBABI.Pack(
		"domainSeparateValidatorSetHash",
		bridgeID,
		VsDomainSeparator,
		big.NewInt(int64(valset.Nonce)),
		big.NewInt(int64(valset.TwoThirdsThreshold())),
		vsHash,
	)
	// this should never happen outside of test since any case that could crash on encoding
	// should be filtered above.
	if err != nil {
		panic(fmt.Sprintf("Error packing checkpoint! %s/n", err))
	}

	hash := crypto.Keccak256Hash(bytes[4:])
	return hash, nil
}

// EncodeDomainSeparatedDataCommitment takes the required input data and produces the required signature to confirm a validator
// set update on the Peggy Ethereum contract. This value will then be signed before being
// submitted to Cosmos, verified, and then relayed to Ethereum
func DataCommitmentTupleRootSignBytes(bridgeID common.Hash, nonce *big.Int, commitment []byte) common.Hash {
	var dataCommitment [32]uint8
	copy(dataCommitment[:], commitment)

	// the word 'transactionBatch' needs to be the same as the 'name' above in the DataCommitmentConfirmABIJSON
	// but other than that it's a constant that has no impact on the output. This is because
	// it gets encoded as a function name which we must then discard.
	bytes, err := internalQGBABI.Pack(
		"domainSeparateDataRootTupleRoot",
		bridgeID,
		DcDomainSeparator,
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

func ComputeValSetHash(vals types.Valset) (ethcmn.Hash, error) {
	ethVals := make([]wrapper.Validator, len(vals.Members))
	for i, val := range vals.Members {
		ethVals[i] = wrapper.Validator{
			Addr:  ethcmn.HexToAddress(val.EthereumAddress),
			Power: big.NewInt(int64(val.Power)),
		}
	}

	encodedVals, err := internalQGBABI.Pack("computeValidatorSetHash", ethVals)
	if err != nil {
		return ethcmn.Hash{}, err
	}

	return crypto.Keccak256Hash(encodedVals[4:]), nil
}

func ethSignBytes(hash ethcmn.Hash) ethcmn.Hash {
	packedABIEncoding := []byte(ethSignPrefix)
	packedABIEncoding = append(packedABIEncoding, hash[:]...)
	return crypto.Keccak256Hash(packedABIEncoding)
}

// SigToVRS breaks apart a signature into its components to make it compatible with the contracts
func SigToVRS(sigHex string) (v uint8, r, s common.Hash) {
	signatureBytes := common.FromHex(sigHex)
	vParam := signatureBytes[64]
	if vParam == byte(0) {
		vParam = byte(27)
	} else if vParam == byte(1) {
		vParam = byte(28)
	}

	v = vParam
	r = common.BytesToHash(signatureBytes[0:32])
	s = common.BytesToHash(signatureBytes[32:64])

	return
}
