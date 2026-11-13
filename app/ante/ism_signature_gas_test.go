package ante_test

import (
	"strings"
	"testing"

	storetypes "cosmossdk.io/store/types"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// ismMetadataHex returns hex metadata in the message-id multisig layout that
// carries sigs signatures.
func ismMetadataHex(sigs int) string {
	return "0x" + strings.Repeat("00", ante.MessageIDMultisigHeaderLen+sigs*ante.ISMSignatureLen)
}

func TestProcessMessageSignatures(t *testing.T) {
	tests := []struct {
		name     string
		metadata string
		want     int
	}{
		{name: "empty", metadata: "", want: 0},
		{name: "prefix only", metadata: "0x", want: 0},
		{name: "short header", metadata: "0x" + strings.Repeat("00", ante.MessageIDMultisigHeaderLen-1), want: 0},
		{name: "message-id header only", metadata: ismMetadataHex(0), want: 0},
		{name: "message-id one signature", metadata: ismMetadataHex(1), want: 1},
		{name: "message-id two signatures", metadata: ismMetadataHex(2), want: 2},
		{name: "message-id partial signature fits no layout", metadata: ismMetadataHex(1) + strings.Repeat("00", ante.ISMSignatureLen-1), want: 0},
		{name: "merkle-root header only", metadata: "0x" + strings.Repeat("00", ante.MerkleRootMultisigHeaderLen), want: 0},
		{name: "merkle-root two signatures", metadata: "0x" + strings.Repeat("00", ante.MerkleRootMultisigHeaderLen+2*ante.ISMSignatureLen), want: 2},
		{name: "merkle-root partial signature fits no layout", metadata: "0x" + strings.Repeat("00", ante.MerkleRootMultisigHeaderLen+1), want: 0},
		{name: "zkism merkle proof weighs nothing", metadata: "0x" + strings.Repeat("00", zkismtypes.MerkleProofMetadataBytes), want: 0},
		{name: "no prefix", metadata: strings.Repeat("00", ante.MessageIDMultisigHeaderLen+ante.ISMSignatureLen), want: 1},
		{name: "odd hex length never decodes", metadata: ismMetadataHex(1) + "0", want: 0},
		{name: "invalid hex counts by length", metadata: "0x" + strings.Repeat("zz", ante.MessageIDMultisigHeaderLen+ante.ISMSignatureLen), want: 1},
		{name: "64,000 signatures", metadata: ismMetadataHex(64_000), want: 64_000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ante.ProcessMessageSignatures(tc.metadata))
		})
	}
}

func TestISMSignatureGasDecorator(t *testing.T) {
	process := func(sigs int) sdk.Msg { return &hyperlanetypes.MsgProcessMessage{Metadata: ismMetadataHex(sigs)} }
	execWith := func(msgs ...sdk.Msg) sdk.Msg {
		exec := authz.NewMsgExec(sdk.AccAddress{}, msgs)
		return &exec
	}
	rate := appconsts.GasPerISMSignature

	tests := []struct {
		name    string
		msgs    []sdk.Msg
		wantGas uint64
	}{
		{name: "no hyperlane messages", msgs: []sdk.Msg{&banktypes.MsgSend{}}, wantGas: 0},
		{name: "no signatures", msgs: []sdk.Msg{process(0)}, wantGas: 0},
		{name: "one signature", msgs: []sdk.Msg{process(1)}, wantGas: rate},
		{name: "several messages", msgs: []sdk.Msg{process(3), &banktypes.MsgSend{}, process(4)}, wantGas: 7 * rate},
		{name: "authz", msgs: []sdk.Msg{execWith(process(3), &banktypes.MsgSend{}, process(4))}, wantGas: 7 * rate},
		{name: "authz skips nested exec", msgs: []sdk.Msg{execWith(execWith(process(5)), process(2))}, wantGas: 2 * rate},
		{name: "full block budget", msgs: []sdk.Msg{process(appconsts.MaxISMSignatures)}, wantGas: uint64(appconsts.MaxISMSignatures) * rate},
	}

	decorator := ante.NewISMSignatureGasDecorator()
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			txBuilder := cdc.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(tc.msgs...))
			ctx := sdk.Context{}.WithGasMeter(storetypes.NewInfiniteGasMeter())
			nextCalled := false

			// The charge applies in simulation too, so gas estimates cover it.
			gotCtx, err := decorator.AnteHandle(ctx, txBuilder.GetTx(), true, func(ctx sdk.Context, _ sdk.Tx, simulate bool) (sdk.Context, error) {
				nextCalled = true
				require.True(t, simulate)
				return ctx, nil
			})

			require.NoError(t, err)
			require.True(t, nextCalled)
			require.Equal(t, tc.wantGas, gotCtx.GasMeter().GasConsumed())
		})
	}
}
