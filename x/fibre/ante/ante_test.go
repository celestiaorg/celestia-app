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

func TestFibreSignatureVerificationDecoratorCacheHitSkipsVerification(t *testing.T) {
	msg := newPayForFibreMsgWithSignatures(1)
	tx := mockTx{msgs: []sdk.Msg{msg}}
	wantKey, err := pffSigCacheKey(msg)
	require.NoError(t, err)
	keeper := &fakeFibreSignatureKeeper{}
	cache := &fakeSigCache{
		t:                t,
		cacheHit:         true,
		wantKey:          wantKey,
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
	wantKey, err := pffSigCacheKey(msg)
	require.NoError(t, err)
	keeper := &fakeFibreSignatureKeeper{
		gasToConsume: 1_000_000,
	}
	cache := &fakeSigCache{
		t:       t,
		wantKey: wantKey,
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

func TestPffSigCacheKeyIgnoresOuterSigner(t *testing.T) {
	first := newPayForFibreMsgWithSignatures(2)
	first.Signer = "relayer-one"
	second := newPayForFibreMsgWithSignatures(2)
	second.Signer = "relayer-two"

	firstKey, err := pffSigCacheKey(first)
	require.NoError(t, err)
	secondKey, err := pffSigCacheKey(second)
	require.NoError(t, err)

	require.Equal(t, firstKey, secondKey)
}

func TestPffSigCacheKeyIncludesCompleteCertificate(t *testing.T) {
	tests := map[string]func(*fibretypes.MsgPayForFibre){
		"payment promise": func(msg *fibretypes.MsgPayForFibre) {
			msg.PaymentPromise.BlobSize++
		},
		"payment promise signature": func(msg *fibretypes.MsgPayForFibre) {
			msg.PaymentPromise.Signature = []byte("different-promise-signature")
		},
		"validator signature": func(msg *fibretypes.MsgPayForFibre) {
			msg.ValidatorSignatures[0] = []byte("different-validator-signature")
		},
		"validator signature order": func(msg *fibretypes.MsgPayForFibre) {
			msg.ValidatorSignatures[0], msg.ValidatorSignatures[1] = msg.ValidatorSignatures[1], msg.ValidatorSignatures[0]
		},
	}

	base := newPayForFibreMsgWithSignatures(2)
	base.PaymentPromise.Signature = []byte("promise-signature")
	baseKey, err := pffSigCacheKey(base)
	require.NoError(t, err)

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			candidate := newPayForFibreMsgWithSignatures(2)
			candidate.PaymentPromise.Signature = []byte("promise-signature")
			mutate(candidate)
			candidateKey, err := pffSigCacheKey(candidate)
			require.NoError(t, err)
			require.NotEqual(t, baseKey, candidateKey)
		})
	}
}

func TestPffSigCacheKeyLengthFramesSignatures(t *testing.T) {
	first := &fibretypes.MsgPayForFibre{ValidatorSignatures: [][]byte{{1}, {2}}}
	second := &fibretypes.MsgPayForFibre{ValidatorSignatures: [][]byte{{1, 2}}}

	firstKey, err := pffSigCacheKey(first)
	require.NoError(t, err)
	secondKey, err := pffSigCacheKey(second)
	require.NoError(t, err)

	require.NotEqual(t, firstKey, secondKey)
}

func TestFibreSignatureVerificationDecoratorCoalescesOuterTxVariants(t *testing.T) {
	keeper := &fakeFibreSignatureKeeper{}
	cache := newMemorySigCache()
	decorator := FibreSignatureVerificationDecorator{k: keeper, pffSigCache: cache}
	first := newPayForFibreMsgWithSignatures(2)
	first.Signer = "relayer-one"
	second := newPayForFibreMsgWithSignatures(2)
	second.Signer = "relayer-two"

	_, err := decorator.AnteHandle(
		sdk.Context{}.WithTxBytes([]byte("first-outer-tx")),
		mockTx{msgs: []sdk.Msg{first}},
		false,
		nextNoop,
	)
	require.NoError(t, err)
	_, err = decorator.AnteHandle(
		sdk.Context{}.WithTxBytes([]byte("second-outer-tx")),
		mockTx{msgs: []sdk.Msg{second}},
		false,
		nextNoop,
	)
	require.NoError(t, err)

	require.Equal(t, 1, keeper.calls)
}

func TestFibreSignatureVerificationDecoratorRechecksChangedCertificate(t *testing.T) {
	keeper := &fakeFibreSignatureKeeper{}
	cache := newMemorySigCache()
	decorator := FibreSignatureVerificationDecorator{k: keeper, pffSigCache: cache}
	first := newPayForFibreMsgWithSignatures(2)
	second := newPayForFibreMsgWithSignatures(2)
	second.ValidatorSignatures[0] = []byte("different")

	_, err := decorator.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{first}}, false, nextNoop)
	require.NoError(t, err)
	_, err = decorator.AnteHandle(sdk.Context{}, mockTx{msgs: []sdk.Msg{second}}, false, nextNoop)
	require.NoError(t, err)

	require.Equal(t, 2, keeper.calls)
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

func nextNoop(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
	return ctx, nil
}
