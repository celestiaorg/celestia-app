package app

import (
	"testing"

	"cosmossdk.io/collections"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// The fast path's raw keys must match the collections the keepers use.
func TestFastRecheckKeysMatchCollections(t *testing.T) {
	addr := sdk.AccAddress([]byte("fast-recheck-address"))
	acc, err := collections.EncodeKeyWithPrefix(authtypes.AddressStoreKeyPrefix, sdk.AccAddressKey, addr)
	require.NoError(t, err)
	require.Equal(t, acc, accountKey(addr))
	num, err := collections.EncodeKeyWithPrefix(authtypes.AccountNumberStoreKeyPrefix, collections.Uint64Key, 42)
	require.NoError(t, err)
	require.Equal(t, num, accountNumberKey(42))
	bal, err := collections.EncodeKeyWithPrefix(banktypes.BalancesPrefix,
		collections.PairKeyCodec(sdk.AccAddressKey, collections.StringKey), collections.Join(addr, appconsts.BondDenom))
	require.NoError(t, err)
	require.Equal(t, bal, balanceKey(addr, appconsts.BondDenom))
	require.Equal(t, []byte(addr), balanceOwner(bal))
}

// eligible must not panic on txs whose accessors do (no fee, a bad granter).
func TestFastRecheckEligibleMalformed(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	addr := sdk.AccAddress([]byte("fast-recheck-address")).String()
	msgAny, err := codectypes.NewAnyWithValue(banktypes.NewMsgSend(sdk.MustAccAddressFromBech32(addr), sdk.MustAccAddressFromBech32(addr), nil))
	require.NoError(t, err)
	single := &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Single_{Single: &txtypes.ModeInfo_Single{Mode: signing.SignMode_SIGN_MODE_DIRECT}}}
	for name, fee := range map[string]*txtypes.Fee{"no fee": nil, "bad granter": {Granter: "garbage"}} {
		raw, err := (&txtypes.Tx{
			Body:       &txtypes.TxBody{Messages: []*codectypes.Any{msgAny}},
			AuthInfo:   &txtypes.AuthInfo{SignerInfos: []*txtypes.SignerInfo{{ModeInfo: single}}, Fee: fee},
			Signatures: [][]byte{make([]byte, 64)},
		}).Marshal()
		require.NoError(t, err, name)
		sdkTx, err := enc.TxConfig.TxDecoder()(raw)
		require.NoError(t, err, name)
		require.Nil(t, (&fastRecheck{}).eligible(sdkTx), name)
	}
}
