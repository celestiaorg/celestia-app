package ante_test

import (
	"encoding/hex"
	"strings"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	hyperlanetypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/types"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/ante"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/authz"
	"github.com/stretchr/testify/require"
)

// maxFuzzISMSignatures bounds structured metadata so each input stays cheap.
const maxFuzzISMSignatures = 512

// recoveredISMSignatures returns the most signatures either hyperlane multisig
// ISM would recover from metadata: the mailbox hex-decodes it with
// util.DecodeEthHex and the ISM then parses the bytes with its own layout. It
// returns 0 when no layout decodes.
func recoveredISMSignatures(metadata string) int {
	raw, err := util.DecodeEthHex(metadata)
	if err != nil {
		return 0
	}
	recovered := 0
	if m, err := ismtypes.NewMessageIdMultisigMetadata(raw); err == nil {
		recovered = max(recovered, len(m.Signatures))
	}
	if m, err := ismtypes.NewMerkleRootMultisigMetadata(raw); err == nil {
		recovered = max(recovered, len(m.Signatures))
	}
	return recovered
}

// ismFuzzMetadata builds hex metadata from fuzz choices: a multisig layout
// header with sigs signatures followed by extra bytes, or extra alone, under
// one of several prefixes and letter cases.
func ismFuzzMetadata(layout uint8, sigs uint16, extra []byte) string {
	var header int
	switch layout % 3 {
	case 0:
		header = ante.MessageIDMultisigHeaderLen
	case 1:
		header = ante.MerkleRootMultisigHeaderLen
	}
	body := make([]byte, header+int(sigs%maxFuzzISMSignatures)*ante.ISMSignatureLen, header+int(sigs%maxFuzzISMSignatures)*ante.ISMSignatureLen+len(extra))
	for i := range body {
		body[i] = byte(i)
	}
	encoded := hex.EncodeToString(append(body, extra...))
	if layout&4 != 0 {
		encoded = strings.ToUpper(encoded)
	}
	switch (layout >> 3) % 5 {
	case 0:
		return "0x" + encoded
	case 1:
		return encoded
	case 2:
		return "0X" + encoded
	case 3:
		return "0x0x" + encoded
	default:
		return encoded + "0"
	}
}

// FuzzISMSignatureWeight checks that the ante charge for multisig ISM
// signatures never undercounts what hyperlane verifies. The per-signature
// charge and the per-block MaxISMSignatures budget rely on
// ProcessMessageSignatures being an upper bound on the signatures the mailbox
// passes to signature recovery. Structured metadata covers both multisig
// layouts, partial signatures, prefixes and letter case; arbitrary strings
// cover everything else. The decorator must charge at least the recovered
// count for direct and one-level authz-wrapped MsgProcessMessage after a
// codec round trip.
func FuzzISMSignatureWeight(f *testing.F) {
	for layout := range uint8(40) {
		for _, sigs := range []uint16{0, 1, 2, 7} {
			f.Add("", layout, sigs, []byte{})
		}
		f.Add("", layout, uint16(3), make([]byte, ante.ISMSignatureLen-1))
		f.Add("", layout, uint16(1), make([]byte, ante.MerkleRootMultisigHeaderLen-ante.MessageIDMultisigHeaderLen))
	}
	f.Add("0x"+strings.Repeat("ab", ante.MessageIDMultisigHeaderLen+ante.ISMSignatureLen), uint8(0), uint16(0), []byte{})
	f.Add("0xzz", uint8(0), uint16(0), []byte{})
	f.Add("0x", uint8(0), uint16(0), []byte{})

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	decorator := ante.NewISMSignatureGasDecorator()
	f.Fuzz(func(t *testing.T, arbitrary string, layout uint8, sigs uint16, extra []byte) {
		if len(arbitrary) > 1<<17 || len(extra) > 1<<12 {
			return
		}
		structured := ismFuzzMetadata(layout, sigs, extra)
		total := 0
		for _, metadata := range []string{arbitrary, structured} {
			counted := ante.ProcessMessageSignatures(metadata)
			recovered := recoveredISMSignatures(metadata)
			require.GreaterOrEqualf(t, counted, recovered, "ante counts fewer signatures than hyperlane recovers (metadata length %d)", len(metadata))
			require.GreaterOrEqual(t, counted, 0)
			total += recovered
		}

		// One direct and one authz-wrapped message, decoded from tx bytes.
		exec := authz.NewMsgExec(sdk.AccAddress(make([]byte, 20)), []sdk.Msg{&hyperlanetypes.MsgProcessMessage{Metadata: structured}})
		builder := enc.TxConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(&hyperlanetypes.MsgProcessMessage{Metadata: arbitrary}, &exec))
		txBytes, err := enc.TxConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		decoded, err := enc.TxConfig.TxDecoder()(txBytes)
		if err != nil {
			// Arbitrary strings that are not valid UTF-8 do not survive decoding;
			// the mailbox never sees them.
			return
		}
		ctx := sdk.Context{}.WithGasMeter(storetypes.NewInfiniteGasMeter())
		gotCtx, err := decorator.AnteHandle(ctx, decoded, false, func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
			return ctx, nil
		})
		require.NoError(t, err)
		require.GreaterOrEqualf(t, gotCtx.GasMeter().GasConsumed(), uint64(total)*appconsts.GasPerISMSignature,
			"decorator charges less than %d recovered signatures", total)
	})
}
