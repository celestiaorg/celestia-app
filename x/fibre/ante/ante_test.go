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
	verifier := new(fakeFibreSignatureVerifier)
	decorator := FibreSignatureVerificationDecorator{
		verifySignatures: verifier.ValidatePayForFibreSignatures,
		isVerificationCached: func([]byte) bool {
			t.Fatal("cache lookup should be skipped in simulation")
			return false
		},
		cacheVerification: func([]byte) {
			t.Fatal("cache write should be skipped in simulation")
		},
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01})

	gotCtx, err := decorator.AnteHandle(ctx, tx, true, nextNoop)

	require.NoError(t, err)
	require.Zero(t, verifier.calls)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorCacheHitSkipsVerification(t *testing.T) {
	tx := mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(1)}}
	verifier := new(fakeFibreSignatureVerifier)
	cacheLookups := 0
	decorator := FibreSignatureVerificationDecorator{
		verifySignatures: verifier.ValidatePayForFibreSignatures,
		isVerificationCached: func(tx []byte) bool {
			cacheLookups++
			require.Equal(t, []byte{0x01}, tx)
			return true
		},
		cacheVerification: func([]byte) {
			t.Fatal("cache write should be skipped on cache hit")
		},
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01})

	gotCtx, err := decorator.AnteHandle(ctx, tx, false, nextNoop)

	require.NoError(t, err)
	require.Equal(t, 1, cacheLookups)
	require.Zero(t, verifier.calls)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorVerifiesCacheMissWithInfiniteGas(t *testing.T) {
	txBytes := []byte{0x01, 0x02, 0x03}
	tx := mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(2)}}
	verifier := &fakeFibreSignatureVerifier{gasToConsume: 1_000_000}
	cachedTxs := 0
	decorator := FibreSignatureVerificationDecorator{
		verifySignatures: verifier.ValidatePayForFibreSignatures,
		isVerificationCached: func(tx []byte) bool {
			require.Equal(t, txBytes, tx)
			return false
		},
		cacheVerification: func(tx []byte) {
			cachedTxs++
			require.Equal(t, txBytes, tx)
		},
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes(txBytes)

	gotCtx, err := decorator.AnteHandle(ctx, tx, false, nextNoop)

	require.NoError(t, err)
	require.Equal(t, 1, verifier.calls)
	require.Equal(t, 1, verifier.infiniteGasCalls)
	require.Equal(t, 1, cachedTxs)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorDoesNotCacheFailures(t *testing.T) {
	expectedErr := errors.New("invalid signature")
	verifier := &fakeFibreSignatureVerifier{err: expectedErr}
	nextCalled := false
	cachedTxs := 0
	decorator := FibreSignatureVerificationDecorator{
		verifySignatures: verifier.ValidatePayForFibreSignatures,
		isVerificationCached: func([]byte) bool {
			return false
		},
		cacheVerification: func([]byte) {
			cachedTxs++
		},
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01})

	_, err := decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(1)}}, false, func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		nextCalled = true
		return ctx, nil
	})

	require.ErrorIs(t, err, expectedErr)
	require.Equal(t, 1, verifier.calls)
	require.False(t, nextCalled)
	require.Zero(t, cachedTxs)
}

type fakeFibreSignatureVerifier struct {
	calls            int
	infiniteGasCalls int
	gasToConsume     uint64
	err              error
}

func (f *fakeFibreSignatureVerifier) ValidatePayForFibreSignatures(ctx sdk.Context, _ *fibretypes.MsgPayForFibre) error {
	f.calls++
	if ctx.GasMeter().Limit() == ^uint64(0) {
		f.infiniteGasCalls++
	}
	if f.gasToConsume > 0 {
		ctx.GasMeter().ConsumeGas(f.gasToConsume, "fake verification")
	}
	return f.err
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
