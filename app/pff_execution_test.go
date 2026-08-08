package app

import (
	"errors"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// stubMsgRouter returns one handler for every message; nil simulates an
// unregistered message.
type stubMsgRouter struct {
	handler baseapp.MsgServiceHandler
}

func (r stubMsgRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler         { return r.handler }
func (r stubMsgRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return r.handler }

func TestExecuteTxMsgs(t *testing.T) {
	storeKey := storetypes.NewKVStoreKey("test")
	writtenKey := []byte("written")

	newCtx := func(t *testing.T) sdk.Context {
		db := dbm.NewMemDB()
		cms := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
		cms.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
		require.NoError(t, cms.LoadLatestVersion())
		return sdk.NewContext(cms, cmtproto.Header{}, false, log.NewNopLogger())
	}

	// A two-message tx so failures on the second message can prove the first
	// message's writes are rolled back.
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	addr := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address())
	sendMsg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin("utia", 1)))
	builder := txConfig.NewTxBuilder()
	require.NoError(t, builder.SetMsgs(sendMsg, sendMsg))
	sdkTx := builder.GetTx()

	exec := func(ctx sdk.Context, handler baseapp.MsgServiceHandler) error {
		return executeTxMsgs(ctx, sdkTx, stubMsgRouter{handler: handler})
	}
	write := func(ctx sdk.Context) {
		ctx.KVStore(storeKey).Set(writtenKey, []byte{1})
	}
	// hasWrite reads with a fresh meter so an exhausted tx meter can't panic the assertion.
	hasWrite := func(ctx sdk.Context) bool {
		return ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).KVStore(storeKey).Has(writtenKey)
	}

	t.Run("nil router skips execution", func(t *testing.T) {
		require.NoError(t, executeTxMsgs(newCtx(t), sdkTx, nil))
	})

	t.Run("commits writes when every message succeeds", func(t *testing.T) {
		calls := 0
		ctx := newCtx(t)
		err := exec(ctx, func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			calls++
			write(ctx)
			return &sdk.Result{}, nil
		})
		require.NoError(t, err)
		require.Equal(t, 2, calls, "expected both messages to execute")
		require.True(t, hasWrite(ctx))
	})

	t.Run("discards writes for tx when a later message fails, all msgs must succeed", func(t *testing.T) {
		calls := 0
		ctx := newCtx(t)
		err := exec(ctx, func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			if calls++; calls == 2 {
				return nil, errors.New("boom")
			}
			write(ctx)
			return &sdk.Result{}, nil
		})
		require.ErrorContains(t, err, "boom")
		require.False(t, hasWrite(ctx), "first message's writes must be rolled back")
	})

	t.Run("recovers out-of-gas panic as an error and discards writes", func(t *testing.T) {
		// Enough gas to complete the write, not enough for the follow-up charge.
		ctx := newCtx(t).WithGasMeter(storetypes.NewGasMeter(50_000))
		err := exec(ctx, func(ctx sdk.Context, _ sdk.Msg) (*sdk.Result, error) {
			write(ctx)
			ctx.GasMeter().ConsumeGas(1_000_000, "settlement")
			return &sdk.Result{}, nil
		})
		require.ErrorContains(t, err, "recovered panic")
		// The out-of-gas panic payload carries the gas descriptor, proving the
		// recovered panic was the gas meter's and not some other failure.
		require.ErrorContains(t, err, "settlement")
		require.False(t, hasWrite(ctx), "write before the panic must be rolled back")
	})

	t.Run("errors when no handler is registered", func(t *testing.T) {
		require.ErrorContains(t, exec(newCtx(t), nil), "no message handler found")
	})
}
