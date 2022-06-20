package orchestrator

import (
	"math/big"
	"testing"

	"github.com/celestiaorg/celestia-app/x/qgb/keeper/keystore"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	wrapper "github.com/celestiaorg/quantum-gravity-bridge/wrappers/QuantumGravityBridge.sol"
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

// NOTE: These tests are more documentation than actual tests. All vaules used
// where derived using the contracts and the evm.

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

	encodedVals, err := types.InternalQGBabi.Pack("computeValidatorSetHash", firstValset)
	require.NoError(t, err)
	assert.Equal(t, firstExpected, encodedVals[4:])
	assert.Equal(t, firstHash[:], crypto.Keccak256(firstExpected))

	encodedVals, err = types.InternalQGBabi.Pack("computeValidatorSetHash", secondValset)
	require.NoError(t, err)
	assert.Equal(t, secondExpected, encodedVals[4:])
	assert.Equal(t, secondHash[:], crypto.Keccak256(secondExpected))
}

func TestValsetConfirmEncodeABI(t *testing.T) {
	const (
		firstExpectedData = "636865636b706f696e7400000000000000000000000000000000000000000000636865636b706f696e740000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000002710636865636b706f696e7400000000000000000000000000000000000000000000" // nolint:lll
	)
	var (
		firstExpected = ethcmn.Hex2Bytes(firstExpectedData)
	)

	bytes, err := types.InternalQGBabi.Pack(
		"domainSeparateValidatorSetHash",
		types.VsDomainSeparator,
		types.VsDomainSeparator,
		big.NewInt(int64(1)),
		big.NewInt(int64(10000)),
		types.VsDomainSeparator,
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

	i := []byte(types.EthSignPrefix)
	i = append(i, types.VsDomainSeparator[:]...)

	assert.Equal(t, firstExpected, i)
}

// Here, we can generate the signatures needed to test the smart contracts
const (
	testAddr  = "0x9c2B12b5a07FC6D719Ed7646e5041A7E85758329"
	testPriv  = "64a1d6f0e760a8d62b4afdde4096f16f51b401eaaecc915740f71770ea76a8ad"
	testAddr2 = "0xe650B084f05C6194f6e552e3b9f08718Bc8a9d56"
	testPriv2 = "6e8bdfa979ab645b41c4d17cb1329b2a44684c82b61b1b060ea9b6e1c927a4f4"
)

func Test_genValSetSignBytes(t *testing.T) {
	vs := types.Valset{
		Members: []types.BridgeValidator{
			{
				EthereumAddress: testAddr,
				Power:           5000,
			},
			{
				EthereumAddress: testAddr2,
				Power:           5000,
			},
		},
		Nonce: 0,
	}
	bID := ethcmn.HexToHash("0x636865636b706f696e7400000000000000000000000000000000000000000000")
	s, err := vs.SignBytes(bID)
	require.NoError(t, err)

	key, err := crypto.HexToECDSA(testPriv)
	require.NoError(t, err)

	personalSignFn, err := keystore.PrivateKeyPersonalSignFn(key)
	require.NoError(t, err)
	sig, err := personalSignFn(ethcmn.HexToAddress(testAddr), s[:])
	require.NoError(t, err)
	_, _, s = SigToVRS(ethcmn.Bytes2Hex(sig))
	// this test doesn't test anything meanfully, but can be used to generate
	// signatures for testing the smart contracts
}

func Test_genTupleRootSignBytes(t *testing.T) {
	bID := ethcmn.HexToHash("0x636865636b706f696e7400000000000000000000000000000000000000000000")
	tupleRoot := ethcmn.HexToHash("0x636865636b706f696e7400000000000000000000000000000000000000000000")
	s := types.DataCommitmentTupleRootSignBytes(bID, big.NewInt(1), tupleRoot[:])

	key, err := crypto.HexToECDSA(testPriv2)
	require.NoError(t, err)

	personalSignFn, err := keystore.PrivateKeyPersonalSignFn(key)
	require.NoError(t, err)
	sig, err := personalSignFn(ethcmn.HexToAddress(testAddr2), s[:])
	require.NoError(t, err)
	_, _, s = SigToVRS(ethcmn.Bytes2Hex(sig))
	// this test doesn't test anything meanfully, but can be used to generate
	// signatures for testing the smart contracts
}
