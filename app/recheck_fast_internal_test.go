package app

import (
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

func TestAnteTrace(t *testing.T) {
	bank, other := storetypes.NewKVStoreKey("bank"), storetypes.NewKVStoreKey("other")
	payer, fc := []byte("payer"), []byte("fc")
	newTrace := func() *anteTrace { return newAnteTrace(bank, payer, fc) }

	t.Run("a key read twice is recorded once", func(t *testing.T) {
		tr := newTrace()
		tr.onRead(other, []byte("k"), []byte("v"), true)
		tr.onRead(other, []byte("k"), []byte("v"), false)
		require.Equal(t, []kvOp{{store: other, key: []byte("k"), value: []byte("v")}}, tr.reads)
	})
	t.Run("a read of a key the pass wrote is not recorded", func(t *testing.T) {
		tr := newTrace()
		tr.onWrite(other, []byte("k"), []byte("v"))
		tr.onRead(other, []byte("k"), []byte("v"), false)
		require.Empty(t, tr.reads)
		require.Len(t, tr.writes, 1)
		require.False(t, tr.failed)
	})
	t.Run("balances must see one Get then one Set", func(t *testing.T) {
		for name, ops := range map[string]func(tr *anteTrace){
			"set first": func(tr *anteTrace) { tr.onWrite(bank, payer, []byte("1")) },
			"two gets":  func(tr *anteTrace) { tr.onRead(bank, fc, []byte("1"), false); tr.onRead(bank, fc, []byte("1"), false) },
			"has":       func(tr *anteTrace) { tr.onRead(bank, fc, []byte{}, true) },
			"delete":    func(tr *anteTrace) { tr.onRead(bank, payer, []byte("1"), false); tr.onWrite(bank, payer, nil) },
			"two writes": func(tr *anteTrace) {
				tr.onRead(bank, payer, []byte("2"), false)
				tr.onWrite(bank, payer, []byte("1"))
				tr.onWrite(bank, payer, []byte("0"))
			},
		} {
			tr := newTrace()
			ops(tr)
			require.True(t, tr.failed, name)
		}
		tr := newTrace()
		tr.onRead(bank, payer, []byte("2"), false)
		tr.onWrite(bank, payer, []byte("1"))
		require.False(t, tr.failed)
		require.Empty(t, tr.reads)
	})
	t.Run("paused reads are skipped and paused writes fail", func(t *testing.T) {
		tr := newTrace()
		tr.paused = true
		tr.onRead(other, []byte("k"), []byte("v"), false)
		require.Empty(t, tr.reads)
		tr.onWrite(other, []byte("k"), []byte("v"))
		require.True(t, tr.failed)
	})
}

// utiaFeePayer must not panic on txs whose accessors do (no fee, a bad granter).
func TestUtiaFeePayerMalformed(t *testing.T) {
	enc := encoding.MakeConfig(ModuleEncodingRegisters...)
	addr := sdk.AccAddress([]byte("fast-recheck-address"))
	msgAny, err := codectypes.NewAnyWithValue(banktypes.NewMsgSend(addr, addr, nil))
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
		require.Nil(t, utiaFeePayer(sdkTx), name)
	}
}
