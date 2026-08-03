package ante

import (
	"errors"
	"fmt"
	"testing"

	storetypes "cosmossdk.io/store/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
)

func TestFibreSignatureGasDecoratorConsumesGasOnAllPaths(t *testing.T) {
	decorator := NewFibreSignatureGasDecorator()
	tx := mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(3)}}
	expectedGas := fibretypes.EstimateGasForPayForFibreSignatureVerification(3)

	for _, simulate := range []bool{false, true} {
		t.Run(fmt.Sprintf("simulate %t", simulate), func(t *testing.T) {
			ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(expectedGas))
			nextCalled := false

			gotCtx, err := decorator.AnteHandle(ctx, tx, simulate, func(ctx sdk.Context, _ sdk.Tx, gotSimulate bool) (sdk.Context, error) {
				nextCalled = true
				require.Equal(t, simulate, gotSimulate)
				return ctx, nil
			})

			require.NoError(t, err)
			require.True(t, nextCalled)
			require.Equal(t, expectedGas, gotCtx.GasMeter().GasConsumed())
		})
	}
}

func TestFibreSignatureGasDecoratorSkipsNonSinglePFFTx(t *testing.T) {
	decorator := NewFibreSignatureGasDecorator()
	tx := mockTx{msgs: []sdk.Msg{
		newPayForFibreMsgWithSignatures(1),
		newPayForFibreMsgWithSignatures(1),
	}}
	ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(1))

	gotCtx, err := decorator.AnteHandle(ctx, tx, false, nextNoop)

	require.NoError(t, err)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorSimulationSkipsVerification(t *testing.T) {
	tx := mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(1)}}
	keeper := &fakeFibreSignatureKeeper{
		t:                 t,
		failOnCacheLookup: true,
		failOnCacheWrite:  true,
	}
	decorator := FibreSignatureVerificationDecorator{
		k: keeper,
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01})

	gotCtx, err := decorator.AnteHandle(ctx, tx, true, nextNoop)

	require.NoError(t, err)
	require.Zero(t, keeper.calls)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorCacheHitSkipsVerification(t *testing.T) {
	tx := mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(1)}}
	keeper := &fakeFibreSignatureKeeper{
		t:                t,
		cacheHit:         true,
		wantTx:           []byte{0x01},
		failOnCacheWrite: true,
	}
	decorator := FibreSignatureVerificationDecorator{
		k: keeper,
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01})

	gotCtx, err := decorator.AnteHandle(ctx, tx, false, nextNoop)

	require.NoError(t, err)
	require.Equal(t, 1, keeper.cacheLookups)
	require.Zero(t, keeper.calls)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorVerifiesCacheMissWithInfiniteGas(t *testing.T) {
	txBytes := []byte{0x01, 0x02, 0x03}
	tx := mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(2)}}
	keeper := &fakeFibreSignatureKeeper{
		t:            t,
		wantTx:       txBytes,
		gasToConsume: 1_000_000,
	}
	decorator := FibreSignatureVerificationDecorator{
		k: keeper,
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes(txBytes)

	gotCtx, err := decorator.AnteHandle(ctx, tx, false, nextNoop)

	require.NoError(t, err)
	require.Equal(t, 1, keeper.calls)
	require.Equal(t, 1, keeper.infiniteGasCalls)
	require.Equal(t, 1, keeper.cachedTxs)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorDoesNotCacheFailures(t *testing.T) {
	expectedErr := errors.New("invalid signature")
	keeper := &fakeFibreSignatureKeeper{err: expectedErr}
	nextCalled := false
	decorator := FibreSignatureVerificationDecorator{
		k: keeper,
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01})

	_, err := decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(1)}}, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})

	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, 1, keeper.calls)
	require.False(t, nextCalled)
	require.Zero(t, keeper.cachedTxs)
}

type fakeFibreSignatureKeeper struct {
	t                 *testing.T
	calls             int
	infiniteGasCalls  int
	cacheLookups      int
	cachedTxs         int
	gasToConsume      uint64
	cacheHit          bool
	wantTx            []byte
	failOnCacheLookup bool
	failOnCacheWrite  bool
	err               error
}

func (f *fakeFibreSignatureKeeper) ValidatePayForFibreSignatures(ctx sdk.Context, _ *fibretypes.MsgPayForFibre) error {
	f.calls++
	if ctx.GasMeter().Limit() == ^uint64(0) {
		f.infiniteGasCalls++
	}
	if f.gasToConsume > 0 {
		ctx.GasMeter().ConsumeGas(f.gasToConsume, "fake verification")
	}
	return f.err
}

func (f *fakeFibreSignatureKeeper) IsPffSigVerificationCached(tx []byte) bool {
	if f.failOnCacheLookup {
		f.t.Fatal("cache lookup should be skipped")
	}
	f.cacheLookups++
	f.requireTx(tx)
	return f.cacheHit
}

func (f *fakeFibreSignatureKeeper) CachePffSigVerification(tx []byte) {
	if f.failOnCacheWrite {
		f.t.Fatal("cache write should be skipped")
	}
	f.cachedTxs++
	f.requireTx(tx)
}

func (f *fakeFibreSignatureKeeper) requireTx(tx []byte) {
	if f.t != nil && f.wantTx != nil {
		require.Equal(f.t, f.wantTx, tx)
	}
}

type mockTx struct {
	msgs []sdk.Msg
}

func (m mockTx) GetMsgs() []sdk.Msg {
	return m.msgs
}

func (m mockTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func newPayForFibreMsgWithSignatures(count int) *fibretypes.MsgPayForFibre {
	signatures := make([][]byte, count)
	for i := range signatures {
		signatures[i] = []byte{byte(i + 1)}
	}
	return &fibretypes.MsgPayForFibre{ValidatorSignatures: signatures}
}

func nextNoop(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}
