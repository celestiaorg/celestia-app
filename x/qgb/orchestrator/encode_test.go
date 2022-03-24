package orchestrator

import (
	"math/big"
	"testing"

	wrapper "github.com/celestiaorg/quantum-gravity-bridge/ethereum/solidity/wrappers/QuantumGravityBridge.sol"
	ethcmn "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	firstValset = []wrapper.Validator{
		{
			Addr:  ethcmn.HexToAddress("0xb33FDD9C00076A15b599F4ab0D29d59720a94E6a"),
			Power: big.NewInt(5000),
		},
	}

	secondValset = []wrapper.Validator{
		{
			Addr:  ethcmn.HexToAddress("0xb33FDD9C00076A15b599F4ab0D29d59720a94E6a"),
			Power: big.NewInt(5000),
		},
		{
			Addr:  ethcmn.HexToAddress("0x83319570b67638aa16F6eDa4d2C2AdBa305c9610"),
			Power: big.NewInt(5000),
		},
	}
)

// TestValsetHashABIEncode is a sanity check is ensure that the abi encoding is working as expected
func TestValsetHashABIEncode(t *testing.T) {
	const (
		firstExpectedData  = "00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000001000000000000000000000000b33fdd9c00076a15b599f4ab0d29d59720a94e6a0000000000000000000000000000000000000000000000000000000000001388" // nolint:lll
		firstExpectedHash  = "0x3c704bc9ea79d3f8c0191d4c6d38516f4bbcd645308b83185ca9fb48aff0eff6"
		secondExpectedData = "00000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000002000000000000000000000000b33fdd9c00076a15b599f4ab0d29d59720a94e6a000000000000000000000000000000000000000000000000000000000000138800000000000000000000000083319570b67638aa16f6eda4d2c2adba305c96100000000000000000000000000000000000000000000000000000000000001388" // nolint:lll
		secondExpectedHash = "0xa8a26d87698033282b716b314e18c49c646d5298b664a33928540ffe996cce35"
	)
	var (
		firstExpected  = ethcmn.Hex2Bytes(firstExpectedData)
		firstHash      = ethcmn.HexToHash(firstExpectedHash)
		secondExpected = ethcmn.Hex2Bytes(secondExpectedData)
		secondHash     = ethcmn.HexToHash(secondExpectedHash)
	)

	encodedVals, err := internalQGBABI.Pack("computeValidatorSetHash", firstValset)
	require.NoError(t, err)
	assert.Equal(t, firstExpected, encodedVals[4:])
	assert.Equal(t, firstHash[:], crypto.Keccak256(firstExpected))

	encodedVals, err = internalQGBABI.Pack("computeValidatorSetHash", secondValset)
	require.NoError(t, err)
	assert.Equal(t, secondExpected, encodedVals[4:])
	assert.Equal(t, secondHash[:], crypto.Keccak256(secondExpected))
}

func TestValsetConfirmEncodeABIEncode(t *testing.T) {
	const (
		firstExpectedData = "636865636b706f696e7400000000000000000000000000000000000000000000636865636b706f696e740000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000002710636865636b706f696e7400000000000000000000000000000000000000000000" // nolint:lll
	)
	var (
		firstExpected = ethcmn.Hex2Bytes(firstExpectedData)
	)

	bytes, err := internalQGBABI.Pack(
		"domainSeparateValidatorSetHash",
		VsDomainSeparator,
		VsDomainSeparator,
		big.NewInt(int64(1)),
		big.NewInt(int64(10000)),
		VsDomainSeparator,
	)
	require.NoError(t, err)

	assert.Equal(t, firstExpected, bytes[4:])

}

func TestSignatureABIEncode(t *testing.T) {
	const (
		firstExpectedData = "19457468657265756d205369676e6564204d6573736167653a0a3332636865636b706f696e7400000000000000000000000000000000000000000000" // nolint:lll
	)
	var (
		firstExpected = ethcmn.Hex2Bytes(firstExpectedData)
	)

	i := []byte(ethSignPrefix)
	i = append(i, VsDomainSeparator[:]...)

	assert.Equal(t, firstExpected, i)
}
