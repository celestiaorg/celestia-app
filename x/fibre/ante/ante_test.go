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
	keeper := &fakeFibreSignatureKeeper{}
	cache := &fakeSigCache{
		t:                 t,
		failOnCacheLookup: true,
		failOnCacheWrite:  true,
	}
	decorator := FibreSignatureVerificationDecorator{
		k:           keeper,
		pffSigCache: cache,
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01})

	gotCtx, err := decorator.AnteHandle(ctx, tx, true, nextNoop)

	require.NoError(t, err)
	require.Zero(t, keeper.calls)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorFinalizeModeSkipsVerification(t *testing.T) {
	tx := mockTx{msgs: []sdk.Msg{newPayForFibreMsgWithSignatures(1)}}
	keeper := &fakeFibreSignatureKeeper{err: errors.New("verification must not run in finalize mode")}
	cache := &fakeSigCache{
		t:                 t,
		failOnCacheLookup: true,
		failOnCacheWrite:  true,
	}
	decorator := FibreSignatureVerificationDecorator{
		k:           keeper,
		pffSigCache: cache,
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01}).
		WithExecMode(sdk.ExecModeFinalize)

	gotCtx, err := decorator.AnteHandle(ctx, tx, false, nextNoop)

	require.NoError(t, err)
	require.Zero(t, keeper.calls)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorCacheHitSkipsVerification(t *testing.T) {
	msg := newPayForFibreMsgWithSignatures(1)
	tx := mockTx{msgs: []sdk.Msg{msg}}
	keeper := &fakeFibreSignatureKeeper{}
	cache := &fakeSigCache{
		t:                t,
		cacheHit:         true,
		wantKey:          mustPffSigCacheKey(t, msg),
		failOnCacheWrite: true,
	}
	decorator := FibreSignatureVerificationDecorator{
		k:           keeper,
		pffSigCache: cache,
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes([]byte{0x01})

	gotCtx, err := decorator.AnteHandle(ctx, tx, false, nextNoop)

	require.NoError(t, err)
	require.Equal(t, 1, cache.cacheLookups)
	require.Zero(t, keeper.calls)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorVerifiesCacheMissWithInfiniteGas(t *testing.T) {
	txBytes := []byte{0x01, 0x02, 0x03}
	msg := newPayForFibreMsgWithSignatures(2)
	tx := mockTx{msgs: []sdk.Msg{msg}}
	keeper := &fakeFibreSignatureKeeper{
		gasToConsume: 1_000_000,
	}
	cache := &fakeSigCache{
		t:       t,
		wantKey: mustPffSigCacheKey(t, msg),
	}
	decorator := FibreSignatureVerificationDecorator{
		k:           keeper,
		pffSigCache: cache,
	}
	ctx := sdk.Context{}.
		WithGasMeter(storetypes.NewGasMeter(1)).
		WithTxBytes(txBytes)

	gotCtx, err := decorator.AnteHandle(ctx, tx, false, nextNoop)

	require.NoError(t, err)
	require.Equal(t, 1, keeper.calls)
	require.Equal(t, 1, keeper.infiniteGasCalls)
	require.Equal(t, 1, cache.cachedTxs)
	require.Zero(t, gotCtx.GasMeter().GasConsumed())
}

func TestFibreSignatureVerificationDecoratorDoesNotCacheFailures(t *testing.T) {
	expectedErr := errors.New("invalid signature")
	keeper := &fakeFibreSignatureKeeper{err: expectedErr}
	cache := &fakeSigCache{t: t}
	nextCalled := false
	decorator := FibreSignatureVerificationDecorator{
		k:           keeper,
		pffSigCache: cache,
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
	require.Zero(t, cache.cachedTxs)
}

func TestPffSigCacheKey(t *testing.T) {
	tests := map[string]struct {
		mutate      func(*fibretypes.MsgPayForFibre)
		wantSameKey bool
	}{
		"outer signer is not part of the key": {
			mutate:      func(msg *fibretypes.MsgPayForFibre) { msg.Signer = "another-relayer" },
			wantSameKey: true,
		},
		"payment promise changes the key": {
			mutate:      func(msg *fibretypes.MsgPayForFibre) { msg.PaymentPromise.BlobSize++ },
			wantSameKey: false,
		},
		"payment promise signature changes the key": {
			mutate: func(msg *fibretypes.MsgPayForFibre) {
				msg.PaymentPromise.Signature = []byte("different-promise-signature")
			},
			wantSameKey: false,
		},
		"validator signature changes the key": {
			mutate: func(msg *fibretypes.MsgPayForFibre) {
				msg.ValidatorSignatures[0] = []byte("different-validator-signature")
			},
			wantSameKey: false,
		},
		"validator signature order changes the key": {
			mutate: func(msg *fibretypes.MsgPayForFibre) {
				msg.ValidatorSignatures[0], msg.ValidatorSignatures[1] = msg.ValidatorSignatures[1], msg.ValidatorSignatures[0]
			},
			wantSameKey: false,
		},
		"concatenated signatures with identical bytes change the key": {
			// Signatures {1}, {2} must not collide with the single signature {1, 2}.
			mutate: func(msg *fibretypes.MsgPayForFibre) {
				msg.ValidatorSignatures = [][]byte{{1, 2}}
			},
			wantSameKey: false,
		},
	}

	baseKey := mustPffSigCacheKey(t, keyTestMsg())
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := keyTestMsg()
			tc.mutate(candidate)
			if tc.wantSameKey {
				require.Equal(t, baseKey, mustPffSigCacheKey(t, candidate))
			} else {
				require.NotEqual(t, baseKey, mustPffSigCacheKey(t, candidate))
			}
		})
	}
}

func TestFibreSignatureVerificationDecoratorVerifiesOncePerCertificate(t *testing.T) {
	// Fee bumps, memo edits, new sequence numbers, and re-signed envelopes all
	// surface as different raw tx bytes, so every case submits the second tx
	// with different tx bytes than the first.
	tests := map[string]struct {
		mutate    func(*fibretypes.MsgPayForFibre)
		wantCalls int
	}{
		"identical certificate resubmitted verifies once": {
			mutate:    func(*fibretypes.MsgPayForFibre) {},
			wantCalls: 1,
		},
		"same certificate from a different message signer verifies once": {
			mutate:    func(msg *fibretypes.MsgPayForFibre) { msg.Signer = "another-relayer" },
			wantCalls: 1,
		},
		"changed certificate is verified again": {
			mutate:    func(msg *fibretypes.MsgPayForFibre) { msg.ValidatorSignatures[0] = []byte("different") },
			wantCalls: 2,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			keeper := &fakeFibreSignatureKeeper{}
			decorator := FibreSignatureVerificationDecorator{k: keeper, pffSigCache: newMemorySigCache()}
			second := keyTestMsg()
			tc.mutate(second)

			for i, msg := range []*fibretypes.MsgPayForFibre{keyTestMsg(), second} {
				ctx := sdk.Context{}.WithTxBytes([]byte{byte(i)})
				_, err := decorator.AnteHandle(ctx, mockTx{msgs: []sdk.Msg{msg}}, false, nextNoop)
				require.NoError(t, err)
			}

			require.Equal(t, tc.wantCalls, keeper.calls)
		})
	}
}

type fakeFibreSignatureKeeper struct {
	calls            int
	infiniteGasCalls int
	gasToConsume     uint64
	err              error
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

type fakeSigCache struct {
	t                 *testing.T
	cacheLookups      int
	cachedTxs         int
	cacheHit          bool
	wantKey           PffSigCacheKey
	failOnCacheLookup bool
	failOnCacheWrite  bool
}

type memorySigCache struct {
	entries map[PffSigCacheKey]struct{}
}

func newMemorySigCache() *memorySigCache {
	return &memorySigCache{entries: make(map[PffSigCacheKey]struct{})}
}

func (c *memorySigCache) IsCached(key PffSigCacheKey) bool {
	_, ok := c.entries[key]
	return ok
}

func (c *memorySigCache) Cache(key PffSigCacheKey) {
	c.entries[key] = struct{}{}
}

func (f *fakeSigCache) IsCached(key PffSigCacheKey) bool {
	if f.failOnCacheLookup {
		f.t.Fatal("cache lookup should be skipped")
	}
	f.cacheLookups++
	f.requireKey(key)
	return f.cacheHit
}

func (f *fakeSigCache) Cache(key PffSigCacheKey) {
	if f.failOnCacheWrite {
		f.t.Fatal("cache write should be skipped")
	}
	f.cachedTxs++
	f.requireKey(key)
}

func (f *fakeSigCache) requireKey(key PffSigCacheKey) {
	if f.t != nil && f.wantKey != (PffSigCacheKey{}) {
		require.Equal(f.t, f.wantKey, key)
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

func keyTestMsg() *fibretypes.MsgPayForFibre {
	msg := newPayForFibreMsgWithSignatures(2)
	msg.Signer = "relayer"
	msg.PaymentPromise.Signature = []byte("promise-signature")
	return msg
}

func mustPffSigCacheKey(t *testing.T, msg *fibretypes.MsgPayForFibre) PffSigCacheKey {
	t.Helper()
	key, err := NewPffSigCacheKey(msg)
	require.NoError(t, err)
	return key
}

func nextNoop(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}
