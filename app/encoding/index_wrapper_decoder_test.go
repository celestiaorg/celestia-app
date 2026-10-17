package encoding

import (
	"testing"

	apperr "github.com/celestiaorg/celestia-app/v10/app/errors"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestIndexWrapperDecoder(t *testing.T) {
	registry := codectypes.NewInterfaceRegistry()
	std.RegisterInterfaces(registry)
	banktypes.RegisterInterfaces(registry)
	blobtypes.RegisterInterfaces(registry)
	cdc := codec.NewProtoCodec(registry)
	txConfig := authtx.NewTxConfig(cdc, authtx.DefaultSignModes)
	decode := indexWrapperDecoder(authtx.DefaultTxDecoder(cdc))

	addr := sdk.AccAddress("addr").String()
	pfb := &blobtypes.MsgPayForBlobs{Signer: addr}
	send := &banktypes.MsgSend{FromAddress: addr, ToAddress: addr}

	encodeTx := func(msgs ...sdk.Msg) []byte {
		builder := txConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msgs...))
		bz, err := txConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		return bz
	}
	wrap := func(tx []byte) []byte {
		bz, err := coretypes.MarshalIndexWrapper(tx, 0)
		require.NoError(t, err)
		return bz
	}

	testCases := []struct {
		name    string
		txBytes []byte
		wantErr error
		wantMsg sdk.Msg
	}{
		{"ordinary tx passes through", encodeTx(send), nil, send},
		{"ordinary malformed bytes pass through", []byte{0xff, 0xff}, sdkerrors.ErrTxDecode, nil},
		{"malformed inner tx", wrap([]byte{0xff, 0xff}), sdkerrors.ErrTxDecode, nil},
		{"zero messages", wrap(encodeTx()), apperr.ErrMultiMsgIndexWrapper, nil},
		{"multiple messages", wrap(encodeTx(pfb, pfb)), apperr.ErrMultiMsgIndexWrapper, nil},
		{"wrong message type", wrap(encodeTx(send)), apperr.ErrNonPFBIndexWrapper, nil},
		{"valid PFB", wrap(encodeTx(pfb)), nil, pfb},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tx, err := decode(tc.txBytes)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				require.Nil(t, tx)
				return
			}
			require.NoError(t, err)
			require.Len(t, tx.GetMsgs(), 1)
			require.Equal(t, tc.wantMsg, tx.GetMsgs()[0])
		})
	}
}
