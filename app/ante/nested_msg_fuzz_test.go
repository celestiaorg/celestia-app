package ante_test

import (
	"bytes"
	"testing"

	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	blobtypes "github.com/celestiaorg/celestia-app/v11/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

// FuzzNestedMsgDecorator varies wrapper, message order, count and simulation
// mode. Round-tripping through TxDecoder exercises unpacked protobuf Any values
// rather than the in-memory Any cache populated by SetMsgs. The guard must block
// forbidden messages at any position before the next ante handler can run.
func FuzzNestedMsgDecorator(f *testing.F) {
	for _, proposal := range []bool{false, true} {
		f.Add(proposal, false, []byte{})
		for kind := range byte(13) {
			f.Add(proposal, false, []byte{kind})
			f.Add(proposal, true, []byte{0, kind, 0})
		}
	}
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	addr := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	decorator := ante.NewNestedMsgDecorator()
	f.Fuzz(func(t *testing.T, proposal, simulate bool, kinds []byte) {
		// Bound packing and unpacking work independently of fuzz input length.
		if len(kinds) > 32 {
			return
		}
		msgs := make([]sdk.Msg, 0, len(kinds))
		wantReject := false
		for _, kind := range kinds {
			var msg sdk.Msg
			switch kind % 13 {
			case 0:
				msg = banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
			case 1:
				msg = &blobtypes.MsgPayForBlobs{}
			case 2:
				msg = &fibretypes.MsgPayForFibre{}
			case 3:
				msg = &fibretypes.MsgPaymentPromiseTimeout{}
			case 4:
				msg = &fibretypes.MsgDepositToEscrow{}
			case 5:
				msg = &fibretypes.MsgRequestWithdrawal{}
			case 6:
				exec := authz.NewMsgExec(addr, []sdk.Msg{})
				msg = &exec
			case 7:
				msg = &govv1.MsgSubmitProposal{}
			case 8:
				msg = &zkismtypes.MsgCreateInterchainSecurityModule{}
			case 9:
				msg = &zkismtypes.MsgUpdateInterchainSecurityModule{}
			case 10:
				msg = &zkismtypes.MsgSubmitMessages{}
			case 11:
				msg = &zkismtypes.MsgUpdateVerifyingKeys{}
			case 12:
				msg = &hyperlanetypes.MsgProcessMessage{}
			}
			// PFB/fibre/container messages are forbidden in both wrappers;
			// deferred ZK/ISM work is forbidden only in governance proposals.
			wantReject = wantReject || kind%13 >= 1 && (kind%13 <= 7 || proposal)
			msgs = append(msgs, msg)
		}
		var wrapper sdk.Msg
		if proposal {
			prop := &govv1.MsgSubmitProposal{Proposer: addr.String()}
			require.NoError(t, prop.SetMsgs(msgs))
			wrapper = prop
		} else {
			exec := authz.NewMsgExec(addr, msgs)
			wrapper = &exec
		}
		builder := enc.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(wrapper))
		wire, err := enc.TxConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		tx, err := enc.TxConfig.TxDecoder()(wire)
		require.NoError(t, err)

		called := false
		_, err = decorator.AnteHandle(sdk.Context{}, tx, simulate, func(ctx sdk.Context, _ sdk.Tx, gotSimulate bool) (sdk.Context, error) {
			called = true
			require.Equal(t, simulate, gotSimulate)
			return ctx, nil
		})
		if wantReject {
			require.ErrorIs(t, err, sdkerrors.ErrNotSupported)
			require.False(t, called, "forbidden nested message reached the next handler")
		} else {
			require.NoError(t, err)
			require.True(t, called)
		}
	})
}
