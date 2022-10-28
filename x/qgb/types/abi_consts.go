package types

import (
	"log"
	"strings"

	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcmn "github.com/ethereum/go-ethereum/common"
)

const (
	// InternalQGBabiJSON is the json encoded abi for private functions in the
	// qgb contract. This is needed to encode data that is signed over in a way
	// that the contracts can easily verify.
	InternalQGBabiJSON = `[
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

	// Domain separator constants copied directly from the contracts.
	ValidatorSetDomainSeparator   = "0x636865636b706f696e7400000000000000000000000000000000000000000000"
	DataCommitmentDomainSeparator = "0x7472616e73616374696f6e426174636800000000000000000000000000000000"
)

var (
	ExternalQGBabi     abi.ABI
	InternalQGBabi     abi.ABI
	BridgeValidatorAbi abi.Arguments

	VsDomainSeparator ethcmn.Hash
	DcDomainSeparator ethcmn.Hash
	BridgeID          = ethcmn.HexToHash("Evm_Celestia_Bridge") //  TODO to be removed afterwards
)

func init() {
	contractAbi, err := abi.JSON(strings.NewReader(wrapper.QuantumGravityBridgeMetaData.ABI))
	if err != nil {
		log.Fatalln("bad ABI constant", err)
	}
	ExternalQGBabi = contractAbi

	internalABI, err := abi.JSON(strings.NewReader(InternalQGBabiJSON))
	if err != nil {
		log.Fatalln("bad internal ABI constant", err)
	}
	InternalQGBabi = internalABI

	solValidatorType, err := abi.NewType("tuple", "validator", []abi.ArgumentMarshaling{
		{Name: "Addr", Type: "address"},
		{Name: "Power", Type: "uint256"},
	})
	if err != nil {
		panic(err)
	}

	BridgeValidatorAbi = abi.Arguments{
		{Type: solValidatorType, Name: "Validator"},
	}

	// create the domain separator for valset hashes
	VsDomainSeparator = ethcmn.HexToHash(ValidatorSetDomainSeparator)
	DcDomainSeparator = ethcmn.HexToHash(DataCommitmentDomainSeparator)
}
