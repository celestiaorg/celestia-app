package ethereum

import (
	"context"
	"math/big"
	"testing"

	basev1beta1 "cosmossdk.io/api/cosmos/base/v1beta1"
	txv1beta1api "cosmossdk.io/api/cosmos/tx/v1beta1"
	sdkmath "cosmossdk.io/math"
	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"
)

func TestSignModeHandlerVerifiesEthereumTxSignature(t *testing.T) {
	rawTx, signature, pubKey, signerData, txData, _ := buildEthereumTxData(t)

	ext, err := NewExtensionOptions(SchemaVersion, DevEthereumChainID, rawTx)
	require.NoError(t, err)
	decoded, err := DecodeExtensionOption(ext)
	require.NoError(t, err)
	require.Equal(t, rawTx, decoded.RawTransaction)

	err = (SignModeHandler{}).VerifySignature(context.Background(), pubKey, signerData, signature, txData)
	require.NoError(t, err)
}

func TestValidateValueTransferRejectsMismatchedRecipient(t *testing.T) {
	_, signature, _, signerData, txData, resolver := buildEthereumTxData(t)
	resolver.mappings[common.HexToAddress("0x1111111111111111111111111111111111111111")] = sdk.AccAddress("different-recipient")

	err := ValidateValueTransfer(sdk.Context{}, resolver, signerData, txData, signature)
	require.ErrorContains(t, err, "does not match resolved Ethereum recipient")
}

func buildEthereumTxData(t *testing.T) ([]byte, []byte, *secp256k1.PubKey, txsigning.SignerData, txsigning.TxData, testResolver) {
	t.Helper()

	key, err := gethcrypto.GenerateKey()
	require.NoError(t, err)
	pubKey := &secp256k1.PubKey{Key: gethcrypto.CompressPubkey(&key.PublicKey)}
	fromCelestia := sdk.AccAddress(pubKey.Address())
	toEth := common.HexToAddress("0x1111111111111111111111111111111111111111")
	toCelestia := sdk.AccAddress("recipient-celestia-acct")

	tx := gethtypes.NewTx(&gethtypes.DynamicFeeTx{
		ChainID:   big.NewInt(int64(DevEthereumChainID)),
		Nonce:     7,
		GasTipCap: big.NewInt(1_000_000_000),
		GasFeeCap: big.NewInt(2_000_000_000),
		Gas:       21000,
		To:        &toEth,
		Value:     big.NewInt(1_000_000_000_000_000_000),
	})
	signedTx, err := gethtypes.SignTx(tx, gethtypes.LatestSignerForChainID(big.NewInt(int64(DevEthereumChainID))), key)
	require.NoError(t, err)
	rawTx, err := signedTx.MarshalBinary()
	require.NoError(t, err)
	signature, err := SignatureFromTx(signedTx)
	require.NoError(t, err)

	ext, err := NewExtensionOptions(SchemaVersion, DevEthereumChainID, rawTx)
	require.NoError(t, err)
	msg := banktypes.NewMsgSend(
		fromCelestia,
		toCelestia,
		sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(1_000_000))),
	)
	msgBytes, err := msg.Marshal()
	require.NoError(t, err)

	txData := txsigning.TxData{
		Body: &txv1beta1api.TxBody{
			Messages: []*anypb.Any{
				{TypeUrl: "/cosmos.bank.v1beta1.MsgSend", Value: msgBytes},
			},
			ExtensionOptions: []*anypb.Any{
				{TypeUrl: ext.TypeUrl, Value: ext.Value},
			},
		},
		AuthInfo: &txv1beta1api.AuthInfo{
			Fee: &txv1beta1api.Fee{
				Amount: []*basev1beta1.Coin{
					{Denom: appconsts.BondDenom, Amount: "42"},
				},
				GasLimit: 21000,
			},
		},
	}
	signerData := txsigning.SignerData{
		Address:       fromCelestia.String(),
		ChainID:       "celestiadev",
		AccountNumber: 1,
		Sequence:      7,
	}
	resolver := testResolver{
		mappings: map[common.Address]sdk.AccAddress{
			gethcrypto.PubkeyToAddress(key.PublicKey): fromCelestia,
			toEth: toCelestia,
		},
	}

	return rawTx, signature, pubKey, signerData, txData, resolver
}

type testResolver struct {
	mappings map[common.Address]sdk.AccAddress
}

func (r testResolver) Resolve(_ sdk.Context, ethAddr []byte) (sdk.AccAddress, bool) {
	addr, found := r.mappings[common.BytesToAddress(ethAddr)]
	return addr, found
}
