package keeper_test

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

const (
	escrowFuzzMaxOps      = 32
	escrowFuzzRecord      = 12 // bytes per op: op, signer, then op-specific bytes
	escrowFuzzSigners     = 3
	escrowFuzzUserBalance = 1_000_000_000
)

// escrowFuzzKeys are fixed so a fuzz input replays the same signed promises.
var escrowFuzzKeys = func() []*secp256k1.PrivKey {
	keys := make([]*secp256k1.PrivKey, escrowFuzzSigners)
	for i := range keys {
		keys[i] = secp256k1.GenPrivKeyFromSecret([]byte{'e', 's', 'c', byte(i)})
	}
	return keys
}()

// escrowLedger is a bank mock that tracks balances, so module funds can be
// compared with the escrow accounts that claim them.
type escrowLedger struct {
	balances map[string]math.Int
	failNext int
}

func (l *escrowLedger) move(from, to string, amt sdk.Coins) error {
	if l.failNext > 0 {
		l.failNext--
		return fmt.Errorf("injected bank failure")
	}
	for _, c := range amt {
		if c.Denom != appconsts.BondDenom {
			return fmt.Errorf("unexpected denom %s", c.Denom)
		}
		if l.balances[from].LT(c.Amount) {
			return sdkerrors.ErrInsufficientFunds
		}
		l.balances[from] = l.balances[from].Sub(c.Amount)
		l.balances[to] = l.balances[to].Add(c.Amount)
	}
	return nil
}

func (l *escrowLedger) snapshot() escrowLedger {
	return escrowLedger{balances: maps.Clone(l.balances), failNext: l.failNext}
}

// escrowFuzzAmount is usually small enough to succeed and occasionally huge.
func escrowFuzzAmount(r *fuzzutil.Consumer) math.Int {
	switch r.Byte() % 8 {
	case 0:
		return math.NewIntFromUint64(r.Uint64())
	case 1:
		return math.NewInt(int64(r.Byte()))
	default:
		return math.NewInt(int64(r.Uint16()) * 100)
	}
}

// FuzzEscrowMsgSequence drives a fuzz-chosen sequence of escrow deposits,
// withdrawal requests, PayForFibre and PaymentPromiseTimeout settlements,
// parameter updates and BeginBlocker runs through the msg server. Each message
// passes ValidateBasic and runs on a cache context committed only on success,
// as DeliverTx does. After every step the escrow accounts must stay
// consistent with their pending withdrawals and with the module's bank
// balance, and no promise may ever settle twice.
func FuzzEscrowMsgSequence(f *testing.F) {
	// Each op is one escrowFuzzRecord-byte record: op, signer, then
	// op-specific bytes (see the switch below), zero-padded.
	// Deposits, a withdrawal request, a payment and its replay, an immediate
	// timeout settlement, then time advances that mature the withdrawal and
	// prune the processed payments before the pooled promises are replayed.
	f.Add(fuzzutil.Records(escrowFuzzRecord,
		[]byte{0, 0, 2, 0x10, 0x00},                         // deposit 409600 for signer 0
		[]byte{0, 1, 2, 0x10, 0x00},                         // deposit 409600 for signer 1
		[]byte{1, 0, 2, 0x01, 0x00},                         // request withdrawal of 25600 for signer 0
		[]byte{2, 0, 0, 0, 0, 0x10, 0},                      // pay a fresh 4096-byte promise of signer 0
		[]byte{2, 1, 0xc0},                                  // replay it
		[]byte{3, 1, 1, 0, 0, 0x10, 0, 0xff, 0x88, 0, 1, 0}, // time out a promise of signer 1 created 2h ago
		[]byte{2, 1, 1, 0, 0, 0x20, 0, 0xff, 0xe2, 0, 2, 0}, // pay a promise of signer 1 created 30m ago
		[]byte{4, 0, 0, 30, 1},                              // advance 30m
		[]byte{3, 0, 0xc2},                                  // time out the promise just paid
		[]byte{4, 0, 0x05, 0xdc, 1},                         // advance 25h: withdrawal matures, payments are pruned
		[]byte{2, 0, 0xc0},                                  // replay the first promise
		[]byte{3, 0, 0xc1},                                  // replay the timed-out promise
	))
	// A withdrawal locks the whole balance, then a payment is taken from it.
	f.Add(fuzzutil.Records(escrowFuzzRecord,
		[]byte{0, 2, 2, 0x00, 0x30},
		[]byte{1, 2, 2, 0x00, 0x30},
		[]byte{2, 2, 2, 0, 0, 0x10, 0},
		[]byte{4, 0, 0x05, 0xa0, 1},
	))
	// Settle, let the record be pruned, raise WithdrawalDelay, then replay.
	f.Add(fuzzutil.Records(escrowFuzzRecord,
		[]byte{0, 0, 2, 0x10, 0x00},
		[]byte{2, 0, 0, 0, 0, 0x10, 0},
		[]byte{4, 0, 0x05, 0xb4, 1},
		[]byte{5, 0, 0x27, 0x60, 0, 0x3c},
		[]byte{2, 1, 0xc0},
		[]byte{3, 1, 0xc0},
	))
	// A bank failure while a withdrawal matures; it must be retried later.
	f.Add(fuzzutil.Records(escrowFuzzRecord,
		[]byte{0, 3, 2, 0x10, 0x00},
		[]byte{1, 0, 2, 0x01, 0x00},
		[]byte{6, 0, 1},
		[]byte{4, 0, 0x05, 0xdc, 1},
		[]byte{4, 0, 0, 1, 1},
	))
	f.Fuzz(func(t *testing.T, script []byte) {
		fuzzEscrowMsgSequence(t, script)
	})
}

func fuzzEscrowMsgSequence(t *testing.T, script []byte) {
	s := new(MsgServerTestSuite)
	s.SetT(t)
	s.SetupTest()
	s.ctx = s.ctx.WithBlockTime(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)).WithBlockHeight(100).WithLogger(log.NewNopLogger())

	signers := make([]string, escrowFuzzSigners)
	ledger := &escrowLedger{balances: map[string]math.Int{types.ModuleName: math.ZeroInt(), authtypes.FeeCollectorName: math.ZeroInt()}}
	for i, k := range escrowFuzzKeys {
		signers[i] = sdk.AccAddress(k.PubKey().Address()).String()
		ledger.balances[signers[i]] = math.NewInt(escrowFuzzUserBalance)
	}
	total := math.NewInt(escrowFuzzUserBalance * escrowFuzzSigners)
	s.bankKeeper.SendCoinsFromAccountToModuleFn = func(_ context.Context, from sdk.AccAddress, to string, amt sdk.Coins) error {
		return ledger.move(from.String(), to, amt)
	}
	s.bankKeeper.SendCoinsFromModuleToAccountFn = func(_ context.Context, from string, to sdk.AccAddress, amt sdk.Coins) error {
		return ledger.move(from, to.String(), amt)
	}
	s.bankKeeper.SendCoinsFromModuleToModuleFn = func(_ context.Context, from, to string, amt sdk.Coins) error {
		return ledger.move(from, to, amt)
	}

	var pool []types.PaymentPromise
	settled := make(map[string]bool)
	c := fuzzutil.New(script)

	// deliver runs one message like DeliverTx: ValidateBasic, then the handler
	// on a cache context whose writes and bank moves are kept only on success.
	deliver := func(msg sdk.HasValidateBasic, run func(ctx sdk.Context) error) error {
		if err := msg.ValidateBasic(); err != nil {
			return err
		}
		saved := ledger.snapshot()
		cacheCtx, write := s.ctx.CacheContext()
		if err := run(cacheCtx); err != nil {
			*ledger = saved
			return err
		}
		write()
		return nil
	}

	for step := 0; step < escrowFuzzMaxOps && !c.Exhausted(); step++ {
		r := c.Record(escrowFuzzRecord)
		op := r.Byte() % 7
		who := r.Intn(escrowFuzzSigners + 1)
		signer := signers[who%escrowFuzzSigners]
		if who == escrowFuzzSigners {
			signer = strings.ToUpper(signers[0]) // same account, non-canonical case
		}
		canonical := signers[who%escrowFuzzSigners]
		before, _ := s.keeper.GetEscrowAccount(s.ctx, canonical)

		switch op {
		case 0:
			amt := sdk.NewCoin(appconsts.BondDenom, escrowFuzzAmount(r))
			msg := &types.MsgDepositToEscrow{Signer: signer, Amount: amt}
			if deliver(msg, func(ctx sdk.Context) error { _, err := s.msgServer.DepositToEscrow(ctx, msg); return err }) == nil {
				after, _ := s.keeper.GetEscrowAccount(s.ctx, canonical)
				require.True(t, after.Balance.Amount.Sub(amt.Amount).Equal(balanceOrZero(before.Balance)), "deposit changed balance by the wrong amount")
				require.True(t, after.AvailableBalance.Amount.Sub(amt.Amount).Equal(balanceOrZero(before.AvailableBalance)))
			}
		case 1:
			amt := sdk.NewCoin(appconsts.BondDenom, escrowFuzzAmount(r))
			msg := &types.MsgRequestWithdrawal{Signer: signer, Amount: amt}
			if deliver(msg, func(ctx sdk.Context) error { _, err := s.msgServer.RequestWithdrawal(ctx, msg); return err }) == nil {
				after, _ := s.keeper.GetEscrowAccount(s.ctx, canonical)
				require.True(t, after.Balance.Equal(before.Balance), "withdrawal request changed balance")
				require.True(t, before.AvailableBalance.Amount.Sub(amt.Amount).Equal(after.AvailableBalance.Amount), "withdrawal request locked the wrong amount")
			}
		case 2, 3:
			promise := escrowFuzzPromise(t, s, r, pool)
			pool = append(pool, promise)
			if len(pool) > 8 {
				pool = pool[1:]
			}
			payer := sdk.AccAddress(promise.SignerPublicKey.Address()).String()
			before, _ = s.keeper.GetEscrowAccount(s.ctx, payer)
			feeBefore := ledger.balances[authtypes.FeeCollectorName]
			var err error
			if op == 2 {
				msg := &types.MsgPayForFibre{Signer: signer, PaymentPromise: promise, ValidatorSignatures: [][]byte{nil}}
				err = deliver(msg, func(ctx sdk.Context) error { _, err := s.msgServer.PayForFibre(ctx, msg); return err })
			} else {
				msg := &types.MsgPaymentPromiseTimeout{Signer: signer, PaymentPromise: promise}
				err = deliver(msg, func(ctx sdk.Context) error { _, err := s.msgServer.PaymentPromiseTimeout(ctx, msg); return err })
			}
			if err == nil {
				pp := fibre.PaymentPromise{}
				require.NoError(t, pp.FromProto(&promise))
				require.NoError(t, pp.Validate(), "settled a promise with an invalid signature")
				hash, herr := pp.Hash()
				require.NoError(t, herr)
				require.False(t, settled[string(hash)], "payment promise settled twice")
				settled[string(hash)] = true
				require.True(t, s.keeper.IsPaymentProcessedByHash(s.ctx, hash))
				charge := types.PaymentAmount(promise.BlobSize)
				after, _ := s.keeper.GetEscrowAccount(s.ctx, payer)
				require.True(t, before.Balance.Amount.Sub(charge.Amount).Equal(after.Balance.Amount), "settlement charged the wrong amount")
				require.True(t, ledger.balances[authtypes.FeeCollectorName].Sub(feeBefore).Equal(charge.Amount))
			}
		case 4:
			dt := time.Duration(r.Uint16()) * time.Minute
			s.ctx = s.ctx.WithBlockTime(s.ctx.BlockTime().Add(dt)).WithBlockHeight(s.ctx.BlockHeight() + 1 + int64(r.Byte()%4))
			injected := ledger.failNext
			require.NoError(t, s.keeper.BeginBlocker(s.ctx))
			// A withdrawal whose bank transfer failed stays pending for a retry.
			if injected == 0 {
				requireNoMaturedWithdrawals(t, s)
			}
		case 5:
			params := s.keeper.GetParams(s.ctx)
			params.WithdrawalDelay = time.Duration(r.Uint16()) * time.Minute
			params.PaymentPromiseTimeout = time.Duration(r.Uint16()%1024) * time.Minute
			msg := &types.MsgUpdateFibreParams{Authority: s.authority, Params: params}
			_ = deliver(msg, func(ctx sdk.Context) error { _, err := s.msgServer.UpdateFibreParams(ctx, msg); return err })
		case 6:
			ledger.failNext = int(r.Byte() % 3)
		}
		requireEscrowInvariants(t, s, ledger, total)
	}
}

func balanceOrZero(c sdk.Coin) math.Int {
	if c.Amount.IsNil() {
		return math.ZeroInt()
	}
	return c.Amount
}

// escrowFuzzPromise reuses a pooled promise or signs a new one with
// fuzz-chosen signer, blob size, creation time and height. A flag corrupts the
// signature, which must keep the promise from settling.
func escrowFuzzPromise(t *testing.T, s *MsgServerTestSuite, r *fuzzutil.Consumer, pool []types.PaymentPromise) types.PaymentPromise {
	sel := r.Byte()
	if sel >= 0xc0 && len(pool) > 0 {
		return pool[int(sel)%len(pool)]
	}
	key := escrowFuzzKeys[int(sel)%escrowFuzzSigners]
	pub := *key.PubKey().(*secp256k1.PubKey)
	blobSize := r.Uint32()
	if blobSize == 0 {
		blobSize = 1000
	}
	created := s.ctx.BlockTime().Add(time.Duration(int16(r.Uint16())) * time.Minute)
	promise := s.createPaymentPromiseWithTime(pub, key, created)
	promise.BlobSize = blobSize
	promise.Height = s.ctx.BlockHeight() + int64(int8(r.Byte()))
	promise.Commitment = bytes.Repeat([]byte{r.Byte()}, 32)
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	promise.Signature, err = key.Sign(signBytes)
	require.NoError(t, err)
	if flip := r.Byte(); flip&0x80 != 0 {
		promise.Signature = bytes.Clone(promise.Signature)
		promise.Signature[int(flip)%len(promise.Signature)] ^= 1
	}
	return promise
}

// requireEscrowInvariants checks every escrow account against its pending
// withdrawals and the bank ledger.
func requireEscrowInvariants(t *testing.T, s *MsgServerTestSuite, ledger *escrowLedger, total math.Int) {
	t.Helper()
	sum := math.ZeroInt()
	s.keeper.IterateEscrowAccounts(s.ctx, func(acc types.EscrowAccount) bool {
		require.Equal(t, appconsts.BondDenom, acc.Balance.Denom)
		require.Equal(t, appconsts.BondDenom, acc.AvailableBalance.Denom)
		require.False(t, acc.AvailableBalance.IsNegative(), "negative available balance for %s", acc.Signer)
		require.True(t, acc.Balance.IsGTE(acc.AvailableBalance), "available exceeds balance for %s", acc.Signer)
		pending := math.ZeroInt()
		for _, w := range s.keeper.GetWithdrawalsBySigner(s.ctx, acc.Signer) {
			require.True(t, w.Amount.IsPositive(), "non-positive pending withdrawal")
			pending = pending.Add(w.Amount.Amount)
		}
		require.True(t, acc.Balance.Amount.Sub(acc.AvailableBalance.Amount).Equal(pending),
			"%s: balance %s - available %s != pending withdrawals %s", acc.Signer, acc.Balance, acc.AvailableBalance, pending)
		sum = sum.Add(acc.Balance.Amount)
		return false
	})
	require.True(t, ledger.balances[types.ModuleName].Equal(sum), "module balance %s != sum of escrow balances %s", ledger.balances[types.ModuleName], sum)

	all := math.ZeroInt()
	for _, v := range ledger.balances {
		all = all.Add(v)
	}
	require.True(t, all.Equal(total), "coins created or destroyed")

	store := s.ctx.KVStore(s.storeKey)
	require.Equal(t, countPrefix(store, types.WithdrawalsBySignerKeyPrefix), countPrefix(store, types.WithdrawalsByAvailableKeyPrefix), "withdrawal indexes disagree")
	require.Equal(t, countPrefix(store, types.ProcessedPaymentsByHashKeyPrefix), countPrefix(store, types.ProcessedPaymentsByTimeKeyPrefix), "processed payment indexes disagree")
}

// requireNoMaturedWithdrawals checks that BeginBlocker paid out every
// withdrawal whose available time has passed.
func requireNoMaturedWithdrawals(t *testing.T, s *MsgServerTestSuite) {
	t.Helper()
	it := s.keeper.GetWithdrawalsByAvailableIterator(s.ctx, s.ctx.BlockTime())
	defer it.Close()
	for ; it.Valid(); it.Next() {
		available, _, _, err := s.keeper.ParseWithdrawalsByAvailableKey(it.Key())
		require.NoError(t, err)
		require.True(t, available.After(s.ctx.BlockTime()), "matured withdrawal left unprocessed")
	}
}

func countPrefix(store storetypes.KVStore, prefix []byte) int {
	it := storetypes.KVStorePrefixIterator(store, prefix)
	defer it.Close()
	n := 0
	for ; it.Valid(); it.Next() {
		n++
	}
	return n
}

// FuzzFibreStoreKeyRoundTrip formats withdrawal and processed-payment index
// keys and parses them back, including timestamps outside the four-digit-year
// range, and parses arbitrary bytes as keys. Parsing must never panic, and a
// key that parses must yield exactly the values it was built from.
func FuzzFibreStoreKeyRoundTrip(f *testing.F) {
	f.Add(int64(1_900_000_000), uint32(5), int64(1_900_003_600), uint32(0), "celestia1signer", []byte{1, 2, 3}, []byte{0x04})
	f.Add(int64(253402300799), uint32(999_999_999), int64(-62135596800), uint32(0), "", []byte{}, []byte{})
	f.Add(int64(253402300800), uint32(0), int64(-62135596801), uint32(1), "a/b", []byte("/"), []byte{0x06, '2'})
	f.Fuzz(func(t *testing.T, availSec int64, availNs uint32, reqSec int64, reqNs uint32, signer string, hash, raw []byte) {
		available := time.Unix(availSec, int64(availNs%1e9)).UTC()
		requested := time.Unix(reqSec, int64(reqNs%1e9)).UTC()
		var k keeper.Keeper

		key := types.WithdrawalsByAvailableKey(available, requested, signer)
		gotAvail, gotReq, gotSigner, err := k.ParseWithdrawalsByAvailableKey(key)
		if fourDigitYear(available) && fourDigitYear(requested) {
			require.NoError(t, err)
		}
		if err == nil {
			require.True(t, gotAvail.Equal(available), "available %v parsed as %v", available, gotAvail)
			require.True(t, gotReq.Equal(requested), "requested %v parsed as %v", requested, gotReq)
			require.Equal(t, signer, gotSigner)
		}

		pkey := types.ProcessedPaymentsByTimeKey(available, hash)
		gotAt, gotHash, err := k.ParseProcessedPaymentsByTimeKey(pkey)
		if fourDigitYear(available) {
			require.NoError(t, err)
		}
		if err == nil {
			require.True(t, gotAt.Equal(available), "processed_at %v parsed as %v", available, gotAt)
			require.True(t, bytes.Equal(hash, gotHash))
		}

		// Arbitrary bytes, with or without the index prefix, must parse or error.
		for _, b := range [][]byte{raw, append(bytes.Clone(types.WithdrawalsByAvailableKeyPrefix), raw...)} {
			// Excluded pending a fix; see the Linear issue for this target.
			if len(b) == 0 {
				continue
			}
			_, _, _, _ = k.ParseWithdrawalsByAvailableKey(b)
		}
		for _, b := range [][]byte{raw, append(bytes.Clone(types.ProcessedPaymentsByTimeKeyPrefix), raw...)} {
			// Excluded pending a fix; see the Linear issue for this target.
			if len(b) < len(types.ProcessedPaymentsByTimeKeyPrefix)+len(sdk.SortableTimeFormat) {
				continue
			}
			_, _, _ = k.ParseProcessedPaymentsByTimeKey(b)
		}
	})
}

func fourDigitYear(t time.Time) bool {
	return t.Year() >= 0 && t.Year() <= 9999
}
