package keeper

import (
	"bytes"
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
	// cacheFuzzCap scales the per-signer pending cap down so it is reachable.
	cacheFuzzCap = 4
)

var cacheFuzzKeys = func() []*secp256k1.PrivKey {
	keys := make([]*secp256k1.PrivKey, cacheFuzzSigners)
	for i := range keys {
		keys[i] = secp256k1.GenPrivKeyFromSecret([]byte{'p', 'c', byte(i)})
	}
	return keys
}()

var cacheFuzzSizes = []uint32{1, 4096, 65536, 1 << 20}

// sweepRecorder is the keeper as the cache's state reader, recording reads.
// The cache reads the escrow on every reservation but reads the params only
// in a sweep, so a params read marks a sweep of the signer last read.
type sweepRecorder struct {
	k             *Keeper
	sweeps        []string
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

func (r *sweepRecorder) IsPromiseProcessed(ctx sdk.Context, creationTimestamp time.Time, hash []byte) bool {
	return r.k.IsPromiseProcessed(ctx, creationTimestamp, hash)
}

func (r *sweepRecorder) GetParams(ctx sdk.Context) types.Params {
	r.sweeps = append(r.sweeps, r.lastSigner)
	return r.k.GetParams(ctx)
}

// signerSnapshot is a signer's reservations; replays and forgeries must not
// change it, while height and available may move on any read.
type signerSnapshot struct {
	pending   map[string]pendingPromise
	committed string
}

type cacheSnapshot map[string]signerSnapshot

func snapshotCache(c *LocalPromiseCache) cacheSnapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := cacheSnapshot{}
	for signer, b := range c.budgets {
		b.mu.Lock()
		s[signer] = signerSnapshot{pending: maps.Clone(b.pending), committed: b.committed.String()}
		b.mu.Unlock()
	}
	return s
}

// reserved reports whether any signer holds a reservation for hashKey.
func (s cacheSnapshot) reserved(hashKey string) bool {
	for _, b := range s {
		if _, ok := b.pending[hashKey]; ok {
			return true
		}
	}
	return false
}

// newestEscrow is the AvailableBalance the cache read at the newest height.
type newestEscrow struct {
	height    int64
	available math.Int
}

// FuzzPromiseCacheReservations checks ValidatePaymentPromise's cache: forgeries leave it untouched,
// replays unchanged, and a signer's pending stays ≤ the AvailableBalance read at its newest height.
// A reservation is dropped only by a sweep of its signer, which keeps no settled or expired one.
func FuzzPromiseCacheReservations(f *testing.F) {
	promise := func(signer, size, commit byte, minutes int8, forge, stale byte) []byte {
		return []byte{0, signer, size, commit, byte(minutes), 0, forge, stale}
	}
	replay := func(idx byte) []byte { return []byte{1, idx} }
	balance := func(signer, units, availThirds, minus byte) []byte {
		return []byte{2, signer, units, availThirds, minus}
	}
	settle := func(idx byte) []byte { return []byte{3, idx} }
	advance := func(minutes, hours byte) []byte { return []byte{4, minutes, hours} }
	f.Add([]byte{})
	// Reserve up to a budget one utia short of three promises, replay, fail;
	// a settlement lowers the escrow by its amount, so the next block's sweep
	// frees nothing net and only a deposit admits the next promise.
	f.Add(fuzzutil.Records(cacheFuzzOpWidth,
		balance(0, 3, 3, 1), promise(0, 1, 1, -1, 0, 0), replay(0), promise(0, 1, 2, -1, 0, 0), promise(0, 1, 3, -1, 0, 0),
		settle(0), balance(0, 2, 3, 1), promise(0, 1, 3, -1, 0, 0), advance(1, 0), promise(0, 1, 3, -1, 0, 0),
		balance(0, 4, 3, 0), promise(0, 1, 4, -1, 0, 0)))
	// Forgeries of every kind against a funded signer, then a valid promise.
	f.Add(fuzzutil.Records(cacheFuzzOpWidth,
		balance(1, 9, 3, 0), promise(1, 2, 1, 0, 1, 0), promise(1, 2, 1, 0, 2, 0), promise(1, 2, 1, 0, 3, 0), promise(1, 2, 1, 0, 4, 0),
		promise(1, 0, 9, 0, 0, 0), promise(1, 2, 1, 0, 0, 0)))
	// A withdrawal empties the available balance and reservations fail; once
	// the oldest passes the withdrawal delay a failing reservation sweeps it,
	// and a deposit admits again.
	f.Add(fuzzutil.Records(cacheFuzzOpWidth,
		balance(2, 4, 3, 0), promise(2, 1, 1, -2, 0, 0), balance(2, 4, 0, 0), promise(2, 1, 2, -2, 0, 0), advance(0, 1),
		promise(2, 1, 3, -1, 0, 0), advance(0, 3), promise(2, 1, 4, -1, 0, 0), balance(2, 4, 3, 0), promise(2, 1, 5, -1, 0, 0)))
	// The cache reads a low escrow at the new height, then a deposit is read
	// through the previous block's ctx: the height guard keeps the low budget.
	f.Add(fuzzutil.Records(cacheFuzzOpWidth,
		balance(0, 6, 3, 0), advance(1, 0), balance(0, 6, 1, 0), promise(0, 1, 1, 0, 0, 0), balance(0, 9, 3, 0),
		promise(0, 1, 2, 0, 0, 1), promise(0, 1, 3, 0, 0, 1), promise(0, 1, 4, 0, 0, 1), promise(0, 1, 5, 0, 0, 0)))
	// A well-funded signer reaches the pending cap; a settlement swept in the
	// next block frees a slot.
	f.Add(fuzzutil.Records(cacheFuzzOpWidth,
		balance(1, 15, 3, 0), promise(1, 1, 1, 0, 0, 0), promise(1, 1, 2, 0, 0, 0), promise(1, 1, 3, 0, 0, 0), promise(1, 1, 4, 0, 0, 0),
		promise(1, 1, 5, 0, 0, 0), settle(0), balance(1, 14, 3, 0), advance(1, 0), promise(1, 1, 5, 0, 0, 0)))

	f.Fuzz(func(t *testing.T, ops []byte) {
		key := storetypes.NewKVStoreKey(types.StoreKey)
		tctx := testutil.DefaultContextWithDB(t, key, storetypes.NewTransientStoreKey("transient_test"))
		ctx := tctx.Ctx.WithChainID("test-chain").WithBlockHeight(100).WithBlockTime(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
		// prevCtx is the previous block's ctx; it reads the current store at an older height.
		prevCtx := ctx
		k := NewKeeper(codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), key, nil, nil, authtypes.NewModuleAddress("gov").String(), false, nil)
		params := types.DefaultParams()
		params.WithdrawalDelay = 2 * time.Hour // short enough for sweeps to drop old reservations
		k.SetParams(ctx, params)
		reader := &sweepRecorder{k: k}
		k.promiseCache = NewLocalPromiseCache(reader)
		k.promiseCache.maxPending = cacheFuzzCap

		signers := make([]string, cacheFuzzSigners)
		for i, pk := range cacheFuzzKeys {
			signers[i] = sdk.AccAddress(pk.PubKey().Address()).String()
		}
		unit := types.PaymentAmount(cacheFuzzSizes[1]).Amount
		newest := map[string]newestEscrow{}
		var pool []types.PaymentPromise

		validate := func(vctx sdk.Context, p types.PaymentPromise, signer string, forged bool) {
			hash, err := promiseHash(&p)
			require.NoError(t, err)
			hashKey := string(hash)
			before := snapshotCache(k.promiseCache)
			reader.sweeps, reader.lastSigner = nil, ""
			_, err = k.ValidatePaymentPromise(vctx, &types.QueryValidatePaymentPromiseRequest{Promise: p})
			after := snapshotCache(k.promiseCache)
			if forged {
				require.Error(t, err, "accepted a forged promise")
				require.Equal(t, before, after, "a forged promise changed the cache")
				require.Empty(t, reader.lastSigner, "a forged promise reached the cache")
				return
			}
			reservedBefore := before.reserved(hashKey)
			if reservedBefore {
				require.Equal(t, before, after, "re-reserving a promise changed the cache")
			}
			if err == nil {
				require.True(t, after.reserved(hashKey), "an accepted promise must be reserved")
			} else if !reservedBefore {
				require.False(t, after.reserved(hashKey), "a rejected promise must not be reserved")
			}

			// Mirror the cache: keep the escrow read at the newest height.
			if n, ok := newest[signer]; reader.lastSigner == signer && (!ok || vctx.BlockHeight() >= n.height) {
				newest[signer] = newestEscrow{vctx.BlockHeight(), reader.lastAvailable}
			}
			for s, b := range after {
				require.LessOrEqual(t, len(b.pending), cacheFuzzCap, "signer %s is over the pending cap", s)
			}
			if err == nil && !reservedBefore {
				total := math.ZeroInt()
				for _, pp := range after[signer].pending {
					total = total.Add(requiredAmount(pp.blobSize))
				}
				require.True(t, total.LTE(newest[signer].available), "%s pending above the %s available at height %d",
					total, newest[signer].available, newest[signer].height)
			}

			for s, b := range after {
				for h := range b.pending {
					if !before.reserved(h) {
						require.Equal(t, hashKey, h, "reserved a hash other than the promise's")
						require.Equal(t, signer, s, "reserved under another signer")
					}
				}
			}
			swept := map[string]bool{}
			for _, s := range reader.sweeps {
				swept[s] = true
			}
			staleBefore := vctx.BlockTime().Add(-params.WithdrawalDelay)
			for s, b := range before {
				for h, pp := range b.pending {
					if _, ok := after[s].pending[h]; !ok {
						require.True(t, swept[s], "a reservation was dropped without a sweep of its signer")
						require.True(t, k.IsPromiseProcessed(vctx, pp.creationTimestamp, []byte(h)) || !pp.creationTimestamp.After(staleBefore),
							"a sweep dropped a promise that is neither settled nor expired")
					}
				}
			}
			for s := range swept {
				for h, pp := range after[s].pending {
					require.False(t, k.IsPromiseProcessed(vctx, pp.creationTimestamp, []byte(h)), "a sweep kept a settled promise")
					require.True(t, pp.creationTimestamp.After(staleBefore), "a sweep kept an expired promise")
				}
			}
		}

		c := fuzzutil.New(ops)
		for n := 0; n < 48 && !c.Exhausted(); n++ {
			r := c.Record(cacheFuzzOpWidth)
			switch r.Byte() % 5 {
			case 0:
				who, size, commit, minutes, hoff, forge := r.Intn(cacheFuzzSigners), r.Intn(len(cacheFuzzSizes)), r.Byte(), int8(r.Byte()), int8(r.Byte())%4, r.Byte()%5
				vctx := ctx
				if r.Byte()%2 == 1 {
					vctx = prevCtx
				}
				pk := cacheFuzzKeys[who]
				p := types.PaymentPromise{
					ChainId:           "test-chain",
					Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes(),
					BlobSize:          cacheFuzzSizes[size],
					Commitment:        bytes.Repeat([]byte{commit}, 32),
					CreationTimestamp: vctx.BlockTime().Add(time.Duration(minutes) * time.Minute),
					Height:            vctx.BlockHeight() + int64(hoff),
					SignerPublicKey:   *pk.PubKey().(*secp256k1.PubKey),
				}
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
				validate(vctx, p, signers[who], forge != 0)
			case 1:
				if len(pool) > 0 {
					p := pool[r.Intn(len(pool))]
					validate(ctx, p, sdk.AccAddress(p.SignerPublicKey.Address()).String(), false)
				}
			case 2:
				signer := signers[r.Intn(cacheFuzzSigners)]
				bal := unit.MulRaw(int64(r.Byte() % 16))
				avail := math.MaxInt(math.ZeroInt(), bal.MulRaw(int64(r.Byte()%4)).QuoRaw(3).SubRaw(int64(r.Byte())))
				k.SetEscrowAccount(ctx, types.EscrowAccount{
					Signer:           signer,
					Balance:          sdk.NewCoin(appconsts.BondDenom, bal),
					AvailableBalance: sdk.NewCoin(appconsts.BondDenom, avail),
				})
			case 3:
				if len(pool) > 0 {
					settled := &pool[r.Intn(len(pool))]
					hash, err := promiseHash(settled)
					require.NoError(t, err)
					k.SetProcessedPayment(ctx, types.ProcessedPayment{PaymentPromiseHash: hash, ProcessedAt: ctx.BlockTime(), CreationTimestamp: settled.CreationTimestamp})
				}
			case 4:
				d := time.Duration(r.Byte()%60)*time.Minute + time.Duration(r.Byte()%4)*time.Hour
				prevCtx = ctx
				ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1).WithBlockTime(ctx.BlockTime().Add(d))
			}
		}
	})
}
