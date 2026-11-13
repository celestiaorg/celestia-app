package frozen_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/app/frozen"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedList(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	r := frozen.NewRules(enc.Codec)
	require.True(t, r.Has([]byte("frozen_placeholder_1")))
	require.True(t, r.Has([]byte("frozen_placeholder_3")))
	require.False(t, r.Has(sdk.MustAccAddressFromBech32(frozen.Whitehat)))
}

func TestAnteHandle(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	listedKey := secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)
	listed := sdk.AccAddress(listedKey.Address())
	other := sdk.AccAddress([]byte("unlisted_address____"))
	r := frozen.NewRules(enc.Codec)
	r.SetList(listed)

	run := func(version uint64, fee sdk.Coins, msgs ...sdk.Msg) error {
		b := enc.TxConfig.NewTxBuilder()
		require.NoError(t, b.SetMsgs(msgs...))
		b.SetFeeAmount(fee)
		ctx := sdk.Context{}.WithConsensusParams(cmtproto.ConsensusParams{Version: &cmtproto.VersionParams{App: version}})
		_, err := r.AnteHandle(ctx, b.GetTx(), false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) { return ctx, nil })
		return err
	}
	fee := sdk.NewCoins(sdk.NewInt64Coin("utia", 10_000))
	send := banktypes.NewMsgSend(listed, other, fee)
	pff := &fibretypes.MsgPayForFibre{Signer: other.String(), PaymentPromise: fibretypes.PaymentPromise{SignerPublicKey: *listedKey}}
	timeout := &fibretypes.MsgPaymentPromiseTimeout{Signer: other.String(), PaymentPromise: fibretypes.PaymentPromise{SignerPublicKey: *listedKey}}

	require.NoError(t, run(frozen.ActivationVersion, fee, send))
	require.ErrorIs(t, run(frozen.ActivationVersion, fee.Add(sdk.NewInt64Coin("ibc/ABC", 1)), send), frozen.ErrFrozen)
	require.ErrorIs(t, run(frozen.ActivationVersion, fee, pff), frozen.ErrFrozen)
	require.ErrorIs(t, run(frozen.ActivationVersion, fee, timeout), frozen.ErrFrozen)
	require.NoError(t, run(frozen.ActivationVersion-1, fee, pff))
}
