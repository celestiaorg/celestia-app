package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// benchmarkValidators is the mainnet validator set size. Its 2/3 quorum takes
// quorumPrefixSignatures equal validators, all in one verification batch.
const (
	benchmarkValidators    = 100
	quorumPrefixSignatures = 67
)

// rotatingMessages is how many distinct transactions a benchmark cycles
// through, against a signature cache far smaller, so a message's entries are
// always evicted before it comes round again and every iteration pays in full.
const (
	rotatingMessages       = 32
	benchmarkCacheCapacity = 4
)

// withCacheCapacity returns a view of the same chain state and message with a
// cache of the given size.
func (f *preverifyFixture) withCacheCapacity(capacity int) *preverifyFixture {
	fresh := *f
	fresh.cache = sigcache.New(capacity)
	fresh.keeper = keeper.NewKeeper(codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), f.storeKey,
		&MockBankKeeper{}, f.stakingKeeper, authtypes.NewModuleAddress("gov").String(), false, fresh.cache)
	return &fresh
}

// forgeValidatorSignature replaces one validator signature with that validator's
// signature over different bytes. That is the expensive shape: random bytes are
// rejected on canonicity, before any curve arithmetic.
func (f *preverifyFixture) forgeValidatorSignature(t testing.TB, msg *types.MsgPayForFibre, index int) {
	t.Helper()

	signature, err := f.valPrivKeys[index].Sign([]byte("sign bytes of another promise"))
	require.NoError(t, err)
	msg.ValidatorSignatures[index] = signature
}

// forgePromiseSignature replaces the promise signature with the signer's
// signature over different bytes.
func (f *preverifyFixture) forgePromiseSignature(t testing.TB, msg *types.MsgPayForFibre) {
	t.Helper()

	signature, err := f.signerPrivKey.Sign([]byte("sign bytes of another promise"))
	require.NoError(t, err)
	msg.PaymentPromise.Signature = signature
}

// BenchmarkPayForFibreSignatureVerification measures what one transaction's
// signature verification costs the node that admits or rejects it, with and
// without the pre-pass. The rejected classes are the ones to read: a tx that
// fails the ante handler is never charged, so its bytes replay for free.
func BenchmarkPayForFibreSignatureVerification(b *testing.B) {
	f := newPreverifyFixtureWithValidators(b, benchmarkValidators)

	tests := []struct {
		name    string
		corrupt func(testing.TB, *preverifyFixture, *types.MsgPayForFibre)
		accept  bool
	}{
		{name: "valid", accept: true},
		{
			name: "forged promise signature",
			corrupt: func(t testing.TB, f *preverifyFixture, msg *types.MsgPayForFibre) {
				f.forgePromiseSignature(t, msg)
			},
		},
		{
			name: "forged validator signature at the first index",
			corrupt: func(t testing.TB, f *preverifyFixture, msg *types.MsgPayForFibre) {
				f.forgeValidatorSignature(t, msg, 0)
			},
		},
		{
			// The sequential verifier's own worst case: it verifies the whole
			// quorum prefix before it reaches the forged signature.
			name: "forged validator signature at the last quorum index",
			corrupt: func(t testing.TB, f *preverifyFixture, msg *types.MsgPayForFibre) {
				f.forgeValidatorSignature(t, msg, quorumPrefixSignatures-1)
			},
		},
	}

	for _, tc := range tests {
		msgs := make([]*types.MsgPayForFibre, rotatingMessages)
		certKeys := make([]sigcache.Key, rotatingMessages)
		for i := range msgs {
			msgs[i] = f.newMessage(b, uint32(i+1))
			if tc.corrupt != nil {
				tc.corrupt(b, f, msgs[i])
			}
			certKey, err := msgs[i].SigCacheKey()
			require.NoError(b, err)
			certKeys[i] = certKey
		}

		// Assert the verdict once, so no class can go vacuous.
		verdict := f.withCacheCapacity(benchmarkCacheCapacity)
		require.Equal(b, tc.accept,
			verdict.keeper.ValidatePayForFibreSignatures(verdict.ctx, msgs[0]) == nil, tc.name)

		for _, prepass := range []bool{false, true} {
			name := tc.name + "/sequential"
			if prepass {
				name = tc.name + "/prepass"
			}
			b.Run(name, func(b *testing.B) {
				fixture := f.withCacheCapacity(benchmarkCacheCapacity)
				// Warm the per-height validator set conversion, which a node
				// serving a stream of promises at recent heights has warm.
				fixture.keeper.PreverifyPayForFibreMsg(fixture.ctx, msgs[0], certKeys[0])

				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					// The ante decorator's sequence on a cache miss.
					index := i % rotatingMessages
					if prepass {
						fixture.keeper.PreverifyPayForFibreMsg(fixture.ctx, msgs[index], certKeys[index])
						if fixture.cache.Has(certKeys[index]) {
							continue
						}
					}
					if fixture.keeper.ValidatePayForFibreSignatures(fixture.ctx, msgs[index]) == nil {
						fixture.cache.Add(certKeys[index])
					}
				}
			})
		}
	}
}
