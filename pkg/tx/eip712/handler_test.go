package eip712

import (
	"context"
	"encoding/hex"
	"math/big"
	"testing"

	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestEIP712RecoverAndVerify(t *testing.T) {
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

	ethSig, err := gethcrypto.Sign(digest[:], priv)
	require.NoError(t, err)
	require.Equal(t, "56c57989545285cc4a7485c0f614d180cae5b1ce7e27ba3fea097c7ffae3141f138761dffb597dfd7d0645f4938bd4c42c6f785debea8e5e795868e75316467b01", hex.EncodeToString(ethSig))
	require.Equal(t, "023b02129ffc8d057746fb5d84b62f2063d919eeea80e51a2697a7d702e169c432", hex.EncodeToString(pubKey.Bytes()))
	require.Equal(t, "cosmos1swva2dz7pr973aqmqyn4jesf8xhu2vdzc4a3kt", signer)

	typedDataJSON, err := (SignModeHandler{}).GetSignBytes(context.Background(), signerData, txData)
	require.NoError(t, err)
	require.NotEmpty(t, typedDataJSON)

	recovered, err := RecoverPubKey(signerData, txData, ethSig)
	require.NoError(t, err)
	require.Equal(t, pubKey.Bytes(), recovered.Bytes())

	ethAddr, err := EthereumAddress(recovered)
	require.NoError(t, err)
	require.Equal(t, "0x8d8687a749cbf56c9a063dd51506f705a1f99fd4", "0x"+hex.EncodeToString(ethAddr[:]))

	err = (SignModeHandler{}).VerifySignature(context.Background(), pubKey, signerData, ethSig, txData)
	require.NoError(t, err)
}

func TestEIP712RejectsReplayFieldMismatches(t *testing.T) {
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
	ethSig, err := gethcrypto.Sign(digest[:], priv)
	require.NoError(t, err)

	tests := []struct {
		name             string
		mutateSignerData func(txsigning.SignerData) txsigning.SignerData
		mutateTxData     func(txsigning.TxData) txsigning.TxData
	}{
		{
			name: "chain id",
			mutateSignerData: func(data txsigning.SignerData) txsigning.SignerData {
				data.ChainID = "celestia-testnet-2"
				return data
			},
		},
		{
			name: "account number",
			mutateSignerData: func(data txsigning.SignerData) txsigning.SignerData {
				data.AccountNumber++
				return data
			},
		},
		{
			name: "sequence",
			mutateSignerData: func(data txsigning.SignerData) txsigning.SignerData {
				data.Sequence++
				return data
			},
		},
		{
			name: "signer",
			mutateSignerData: func(data txsigning.SignerData) txsigning.SignerData {
				data.Address = sdk.AccAddress(make([]byte, len(pubKey.Address()))).String()
				return data
			},
		},
		{
			name: "body bytes",
			mutateTxData: func(data txsigning.TxData) txsigning.TxData {
				data.BodyBytes = []byte("tampered-body-bytes")
				return data
			},
		},
		{
			name: "auth info bytes",
			mutateTxData: func(data txsigning.TxData) txsigning.TxData {
				data.AuthInfoBytes = []byte("tampered-auth-info-bytes")
				return data
			},
		},
		{
			name: "fee amount",
			mutateTxData: func(data txsigning.TxData) txsigning.TxData {
				data.AuthInfo.Fee.Amount[0].Amount = "1001"
				return data
			},
		},
		{
			name: "gas limit",
			mutateTxData: func(data txsigning.TxData) txsigning.TxData {
				data.AuthInfo.Fee.GasLimit++
				return data
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mutatedSignerData := signerData
			if tt.mutateSignerData != nil {
				mutatedSignerData = tt.mutateSignerData(mutatedSignerData)
			}
			mutatedTxData := testTxData(t)
			if tt.mutateTxData != nil {
				mutatedTxData = tt.mutateTxData(mutatedTxData)
			}
			err := (SignModeHandler{}).VerifySignature(context.Background(), pubKey, mutatedSignerData, ethSig, mutatedTxData)
			require.Error(t, err)
		})
	}
}

func TestEIP712RejectsHighS(t *testing.T) {
	sig := make([]byte, 65)
	sig[31] = 1
	halfN := new(big.Int).Rsh(new(big.Int).Set(gethcrypto.S256().Params().N), 1)
	new(big.Int).Add(halfN, big.NewInt(1)).FillBytes(sig[32:64])
	sig[64] = 27
	_, err := normalizeSignature(sig)
	require.ErrorContains(t, err, "invalid values")
}
