package keeper

import (
	"bytes"
	"encoding/hex"
	"maps"
	"testing"
	"time"

	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

const (
	cacheFuzzOpWidth = 8
	cacheFuzzSigners = 3
	cacheFuzzPool    = 8
)

var cacheFuzzKeys = func() []*secp256k1.PrivKey {
	keys := make([]*secp256k1.PrivKey, cacheFuzzSigners)
	for i := range keys {
		keys[i] = secp256k1.GenPrivKeyFromSecret([]byte{'p', 'c', byte(i)})
	}
	return keys
}()

var cacheFuzzSizes = []uint32{1, 4096, 65536, 1 << 20}

// sweepEvent is one sweep of a signer's budget and the AvailableBalance it
// reconciled against.
type sweepEvent struct {
	signer    string
	available math.Int
}

// sweepRecorder is the keeper as the cache's state reader, recording sweeps.
// The cache reads the escrow on every reservation but reads the params only
// in a sweep, so a params read marks a sweep of the signer last read.
type sweepRecorder struct {
	k             *Keeper
	sweeps        []sweepEvent
	lastSigner    string
	lastAvailable math.Int
}

func (r *sweepRecorder) GetEscrowAccount(ctx sdk.Context, signer string) (types.EscrowAccount, bool) {
	acc, found := r.k.GetEscrowAccount(ctx, signer)
	r.lastSigner, r.lastAvailable = signer, math.ZeroInt()
	if found {
		r.lastAvailable = acc.AvailableBalance.Amount
	}
	return acc, found
}

func (r *sweepRecorder) IsPaymentProcessedByHash(ctx sdk.Context, hash []byte) bool {
	return r.k.IsPaymentProcessedByHash(ctx, hash)
}

func (r *sweepRecorder) GetParams(ctx sdk.Context) types.Params {
	r.sweeps = append(r.sweeps, sweepEvent{r.lastSigner, r.lastAvailable})
	return r.k.GetParams(ctx)
}

type cacheSnapshot struct {
	remaining map[string]string
	hashes    map[string]map[string]struct{}
	pending   map[string]pendingPromise
}

func snapshotCache(c *LocalPromiseCache) cacheSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := cacheSnapshot{remaining: map[string]string{}, hashes: map[string]map[string]struct{}{}, pending: maps.Clone(c.pending)}
	for signer, b := range c.budgets {
		s.remaining[signer] = b.remaining.String()
		s.hashes[signer] = maps.Clone(b.hashes)
	}
	return s
}

// FuzzPromiseCacheReservations drives the ValidatePaymentPromise query with
// its validator-local promise cache through signed, forged and replayed
// promises, escrow balance changes, on-chain settlements and block advances.
// A forged promise must be rejected without touching the cache, and a promise
// whose hash is already reserved leaves the cache unchanged. Per signer, the
// amount reserved since the last sweep never exceeds the AvailableBalance that
// sweep read, and a swept signer keeps no settled or no longer settleable
// reservation. A reservation that reads a lower AvailableBalance than the
// last sweep sweeps first; a higher one needs no sweep.
func FuzzPromiseCacheReservations(f *testing.F) {
	promise := func(signer, size, commit byte, minutes int8, forge byte) []byte {
		return []byte{0, signer, size, commit, byte(minutes), 0, forge}
	}
	replay := func(idx byte) []byte { return []byte{1, idx} }
	balance := func(signer, units, availThirds byte) []byte { return []byte{2, signer, units, availThirds} }
	settle := func(idx byte) []byte { return []byte{3, idx} }
	advance := func(minutes, hours byte) []byte { return []byte{4, minutes, hours} }
	f.Add([]byte{})
	// Reserve up to the budget, replay, fail, settle one and retry next block.
	f.Add(fuzzutil.Records(cacheFuzzOpWidth,
		balance(0, 3, 3), promise(0, 1, 1, -1, 0), replay(0), promise(0, 1, 2, -1, 0), promise(0, 1, 3, -1, 0),
		promise(0, 1, 4, -1, 0), settle(0), promise(0, 1, 4, -1, 0), advance(1, 0), promise(0, 1, 4, -1, 0)))
	// Forgeries of every kind against a funded signer, then a valid promise.
	f.Add(fuzzutil.Records(cacheFuzzOpWidth,
		balance(1, 9, 3), promise(1, 2, 1, 0, 1), promise(1, 2, 1, 0, 2), promise(1, 2, 1, 0, 3), promise(1, 2, 1, 0, 4),
		promise(1, 0, 9, 0, 0), promise(1, 2, 1, 0, 0)))
	// A withdrawal lowers the available balance; reservations stop at the old
	// budget until a failing reservation in a new block forces a sweep, and
	// reservations held past the withdrawal delay are dropped on that sweep.
	f.Add(fuzzutil.Records(cacheFuzzOpWidth,
		balance(2, 4, 3), promise(2, 1, 1, -2, 0), balance(2, 4, 0), promise(2, 1, 2, -2, 0), advance(0, 1),
		promise(2, 1, 3, -1, 0), advance(0, 3), balance(2, 4, 3), promise(2, 1, 4, -1, 0), promise(2, 1, 5, -1, 0), advance(1, 0), promise(2, 1, 6, -1, 0)))

	f.Fuzz(func(t *testing.T, ops []byte) {
		key := storetypes.NewKVStoreKey(types.StoreKey)
		tctx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
		ctx := tctx.Ctx.WithChainID("test-chain").WithBlockHeight(100).WithBlockTime(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
		k := NewKeeper(codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), key, nil, nil, authtypes.NewModuleAddress("gov").String(), false, nil)
		params := types.DefaultParams()
		params.WithdrawalDelay = 2 * time.Hour // short enough for sweeps to drop old reservations
		k.SetParams(ctx, params)
		reader := &sweepRecorder{k: k}
		k.promiseCache = NewLocalPromiseCache(reader)

		signers := make([]string, cacheFuzzSigners)
		for i, pk := range cacheFuzzKeys {
			signers[i] = sdk.AccAddress(pk.PubKey().Address()).String()
		}
		unit := types.PaymentAmount(cacheFuzzSizes[1]).Amount
		availAtSweep := map[string]math.Int{}
		sinceSweep := map[string]math.Int{}
		hashSigner := map[string]string{}
		var pool []types.PaymentPromise

		validate := func(p types.PaymentPromise, signer string, forged bool) {
			hash, err := promiseHash(&p)
			require.NoError(t, err)
			hashKey := hex.EncodeToString(hash)
			before := snapshotCache(k.promiseCache)
			reader.sweeps, reader.lastSigner = nil, ""
			_, err = k.ValidatePaymentPromise(ctx, &types.QueryValidatePaymentPromiseRequest{Promise: p})
			after := snapshotCache(k.promiseCache)
			if forged {
				require.Error(t, err, "accepted a forged promise")
				require.Equal(t, before, after, "a forged promise changed the cache")
				require.Empty(t, reader.lastSigner, "a forged promise reached the cache")
				return
			}
			_, reservedBefore := before.pending[hashKey]
			if reservedBefore {
				require.Equal(t, before, after, "re-reserving a promise changed the cache")
			}
			_, reservedAfter := after.pending[hashKey]
			if err == nil {
				require.True(t, reservedAfter, "an accepted promise must be reserved")
			} else if !reservedBefore {
				require.False(t, reservedAfter, "a rejected promise must not be reserved")
			}

			swept := map[string]bool{}
			// A reservation that reads a balance below the last sweep's must
			// sweep first, so the budget never exceeds what is available now.
			if prev, ok := availAtSweep[signer]; ok && !reservedBefore && reader.lastSigner == signer && reader.lastAvailable.LT(prev) {
				require.NotEmpty(t, reader.sweeps, "available balance fell from %s to %s without a sweep", prev, reader.lastAvailable)
			}
			for _, s := range reader.sweeps {
				availAtSweep[s.signer], sinceSweep[s.signer] = s.available, math.ZeroInt()
				swept[s.signer] = true
			}
			for h := range after.pending {
				if _, ok := before.pending[h]; !ok {
					require.Equal(t, hashKey, h, "reserved a hash other than the promise's")
					hashSigner[h] = signer
					sinceSweep[signer] = sinceSweep[signer].Add(requiredAmount(after.pending[h].blobSize))
					require.True(t, sinceSweep[signer].LTE(availAtSweep[signer]),
						"reserved %s since the last sweep, which read only %s", sinceSweep[signer], availAtSweep[signer])
				}
			}
			for h := range before.pending {
				if _, ok := after.pending[h]; !ok {
					require.True(t, swept[hashSigner[h]], "a reservation was dropped without a sweep of its signer")
				}
			}
			staleBefore := ctx.BlockTime().Add(-params.WithdrawalDelay)
			for signer := range swept {
				for h := range after.hashes[signer] {
					raw, _ := hex.DecodeString(h)
					require.False(t, k.IsPaymentProcessedByHash(ctx, raw), "a sweep kept a settled promise")
					require.True(t, after.pending[h].creationTimestamp.After(staleBefore), "a sweep kept an unsettleable promise")
				}
			}
		}

		c := fuzzutil.New(ops)
		for n := 0; n < 48 && !c.Exhausted(); n++ {
			r := c.Record(cacheFuzzOpWidth)
			switch r.Byte() % 5 {
			case 0:
				who := r.Intn(cacheFuzzSigners)
				pk := cacheFuzzKeys[who]
				p := types.PaymentPromise{
					ChainId:           "test-chain",
					Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes(),
					BlobSize:          cacheFuzzSizes[r.Intn(len(cacheFuzzSizes))],
					Commitment:        bytes.Repeat([]byte{r.Byte()}, 32),
					CreationTimestamp: ctx.BlockTime().Add(time.Duration(int8(r.Byte())) * time.Minute),
					Height:            ctx.BlockHeight() + int64(int8(r.Byte())%4),
					SignerPublicKey:   *pk.PubKey().(*secp256k1.PubKey),
				}
				forge := r.Byte() % 5
				signWith := pk
				if forge == 3 {
					signWith = cacheFuzzKeys[(who+1)%cacheFuzzSigners]
				}
				pp := fibre.PaymentPromise{}
				require.NoError(t, pp.FromProto(&p))
				signBytes, err := pp.SignBytes()
				require.NoError(t, err)
				p.Signature, err = signWith.Sign(signBytes)
				require.NoError(t, err)
				switch forge {
				case 1:
					p.Signature[r.Intn(len(p.Signature))] ^= 1 << r.Intn(8)
				case 2:
					p.BlobSize ^= 1
				case 4:
					p.Commitment[0] ^= 0x80
				}
				if forge == 0 && len(pool) < cacheFuzzPool {
					pool = append(pool, p)
				}
				validate(p, signers[who], forge != 0)
			case 1:
				if len(pool) > 0 {
					p := pool[r.Intn(len(pool))]
					validate(p, sdk.AccAddress(p.SignerPublicKey.Address()).String(), false)
				}
			case 2:
				signer := signers[r.Intn(cacheFuzzSigners)]
				bal := unit.MulRaw(int64(r.Byte() % 16))
				k.SetEscrowAccount(ctx, types.EscrowAccount{
					Signer:           signer,
					Balance:          sdk.NewCoin(appconsts.BondDenom, bal),
					AvailableBalance: sdk.NewCoin(appconsts.BondDenom, bal.MulRaw(int64(r.Byte()%4)).QuoRaw(3)),
				})
			case 3:
				if len(pool) > 0 {
					hash, err := promiseHash(&pool[r.Intn(len(pool))])
					require.NoError(t, err)
					k.SetProcessedPayment(ctx, types.ProcessedPayment{PaymentPromiseHash: hash, ProcessedAt: ctx.BlockTime()})
				}
			case 4:
				d := time.Duration(r.Byte()%60)*time.Minute + time.Duration(r.Byte()%4)*time.Hour
				ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1).WithBlockTime(ctx.BlockTime().Add(d))
			}
		}
	})
}
