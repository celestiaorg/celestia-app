package keeper

import (
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	cosmostx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPreverificationDeclaredGas(t *testing.T) {
	msg := &types.MsgPayForFibre{ValidatorSignatures: make([][]byte, 100)}
	required := types.EstimateGasForPayForFibreSignatureVerification(100)
	for _, gas := range []uint64{0, required - 1, required, required + 1, ^uint64(0)} {
		auth, err := (&cosmostx.AuthInfo{Fee: &cosmostx.Fee{GasLimit: gas}}).Marshal()
		require.NoError(t, err)
		raw, err := (&cosmostx.TxRaw{AuthInfoBytes: auth}).Marshal()
		require.NoError(t, err)
		require.Equal(t, gas >= required, canPayForPreverification(&types.DecodedPayForFibre{Raw: raw, Msg: msg}))
	}
	for _, raw := range [][]byte{nil, {0xff}, {0x12, 0x01, 0xff}} {
		require.False(t, canPayForPreverification(&types.DecodedPayForFibre{Raw: raw, Msg: msg}))
	}
}
