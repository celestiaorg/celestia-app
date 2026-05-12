package eip712

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	txv1beta1 "cosmossdk.io/api/cosmos/tx/v1beta1"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestEIP712BuildTypedDataAndDigest(t *testing.T) {
	privBytes, err := hex.DecodeString("4c0883a69102937d6231471b5dbb6204fe512961708279b727a63ca9b9a4b4f3")
	require.NoError(t, err)
	priv, err := gethcrypto.ToECDSA(privBytes)
	require.NoError(t, err)
	pubKey := &secp256k1.PubKey{Key: gethcrypto.CompressPubkey(&priv.PublicKey)}
	signer := sdk.AccAddress(pubKey.Address()).String()

	signerData := txsigning.SignerData{
		Address:       signer,
		ChainID:       "celestia-testnet-1",
		AccountNumber: 7,
		Sequence:      9,
	}
	txData := testTxData(t)

	digest, err := Digest(signerData, txData)
	require.NoError(t, err)
	require.Equal(t, "62d8494c8544842f90be1830f79ecfcf8125bebcce0d3e62a9b6ab6a8513cc24", hex.EncodeToString(digest[:]))

	typedData, err := BuildTypedData(signerData, txData)
	require.NoError(t, err)
	typedDataJSON, err := json.Marshal(typedData)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"types": {
			"CelestiaTx": [
				{"name":"celestiaChainId","type":"string"},
				{"name":"ethChainId","type":"uint256"},
				{"name":"accountNumber","type":"uint64"},
				{"name":"sequence","type":"uint64"},
				{"name":"signer","type":"string"},
				{"name":"feePayer","type":"string"},
				{"name":"feeGranter","type":"string"},
				{"name":"gasLimit","type":"uint64"},
				{"name":"feeAmount","type":"string"},
				{"name":"bodyBytesHash","type":"bytes32"},
				{"name":"authInfoBytesHash","type":"bytes32"},
				{"name":"schemaVersion","type":"uint32"}
			],
			"EIP712Domain": [
				{"name":"name","type":"string"},
				{"name":"version","type":"string"},
				{"name":"chainId","type":"uint256"}
			]
		},
		"primaryType": "CelestiaTx",
		"domain": {"name":"Celestia","version":"1","chainId":"12345"},
		"message": {
			"celestiaChainId":"celestia-testnet-1",
			"ethChainId":"12345",
			"accountNumber":"7",
			"sequence":"9",
			"signer":"cosmos1swva2dz7pr973aqmqyn4jesf8xhu2vdzc4a3kt",
			"feePayer":"",
			"feeGranter":"",
			"gasLimit":"80000",
			"feeAmount":"1000utia",
			"bodyBytesHash":"0x123e5e68bc2e332cb587886d20991124410bf726af8b702cb23b5e6b61f90154",
			"authInfoBytesHash":"0xc937728ae0e48be9d809cc471fc34a08d9d83fb989dac69e1c407eacfb37a01e",
			"schemaVersion":"1"
		}
	}`, string(typedDataJSON))
}

func TestBuildTypedDataRequiresExtensionOption(t *testing.T) {
	txData := testTxData(t)
	txData.Body.ExtensionOptions = nil
	_, err := BuildTypedData(txsigning.SignerData{ChainID: "celestia-testnet-1"}, txData)
	require.ErrorContains(t, err, "missing EIP-712 extension option")
}

func testTxData(t *testing.T) txsigning.TxData {
	t.Helper()
	ext, err := NewExtensionOptions(SchemaVersion, 12345)
	require.NoError(t, err)
	bodyBytes := []byte("deterministic-body-bytes")
	authInfoBytes := []byte("deterministic-auth-info-bytes")
	return txsigning.TxData{
		Body: &txv1beta1.TxBody{
			ExtensionOptions: []*anypb.Any{{TypeUrl: ext.TypeUrl, Value: ext.Value}},
		},
		AuthInfo: &txv1beta1.AuthInfo{
			Fee: &txv1beta1.Fee{
				Amount:   []*basev1beta1.Coin{{Denom: "utia", Amount: "1000"}},
				GasLimit: 80000,
			},
		},
		BodyBytes:     bodyBytes,
		AuthInfoBytes: authInfoBytes,
	}
}
