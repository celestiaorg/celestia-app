package keeper_test

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/forwarding/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// decodeBinding decodes a (custom_hook_id, custom_hook_metadata) pair the way
// MsgForward does, normalising the zero hook to none.
func decodeBinding(hook, metadata string) (hookID, meta []byte, ok bool) {
	if hook != "" {
		h, err := util.DecodeHexAddress(hook)
		if err != nil {
			return nil, nil, false
		}
		if !h.IsZeroAddress() {
			hookID = h.Bytes()
		}
	}
	if metadata != "" {
		m, err := util.DecodeEthHex(metadata)
		if err != nil {
			return nil, nil, false
		}
		meta = m
	}
	return hookID, meta, true
}

// FuzzForwardingAddressQueryMsgParity checks that the DeriveForwardingAddress
// query and the MsgForward handler agree on the address for the same
// fuzzer-chosen hex strings: a queried address is never rejected as a mismatch
// by MsgForward with those strings, any other binding is, and strings the query
// rejects are rejected by MsgForward too.
func FuzzForwardingAddressQueryMsgParity(f *testing.F) {
	recipient := "0x00000000000000000000000000000000000000000000000000000000deadbeef"
	hook := "0x000000000000000000000000000000000000000000000000000000000000abcd"
	zero := "0x0000000000000000000000000000000000000000000000000000000000000000"
	f.Add(recipient, "", "", hook, "")
	f.Add(recipient, hook, "0xabcdef", hook, "abcdef")
	f.Add(recipient, zero, "", "", "0x")
	f.Add(recipient, "", "0x00", "", "")
	f.Add(recipient, hook, "", "", "0x000000000000000000000000000000000000000000000000000000000000abcd")
	f.Add("0xdeadbeef", "nothex", "0xzz", "", "")
	f.Add(recipient, "0X"+hook[2:], "0xABCDEF", hook, "0xabcdef")

	f.Fuzz(func(t *testing.T, recipient, hook, metadata, otherHook, otherMetadata string) {
		s := newTestIGPSetup(t)
		query := keeper.NewQueryServerImpl(keeper.NewKeeper(s.bankKeeper, s.warpKeeper, s.hyperlaneKeeper))
		forward := func(addr, hook, metadata string) error {
			msg := types.NewMsgForward(s.signer.String(), addr, s.destDomain, recipient, s.tokenID,
				sdk.NewCoin(appconsts.BondDenom, math.NewInt(100)))
			msg.CustomHookId, msg.CustomHookMetadata = hook, metadata
			_, err := s.msgServer.Forward(s.ctx, msg)
			return err
		}

		resp, err := query.DeriveForwardingAddress(s.ctx, &types.QueryDeriveForwardingAddressRequest{
			DestDomain:         s.destDomain,
			DestRecipient:      recipient,
			TokenId:            s.tokenID,
			CustomHookId:       hook,
			CustomHookMetadata: metadata,
		})
		if err != nil {
			if status.Code(err) != codes.InvalidArgument {
				t.Fatalf("query with a known token and route failed: %v", err)
			}
			// Input the query rejects never reaches a balance lookup.
			if err := forward(s.forwardAddr.String(), hook, metadata); err == nil || errors.Is(err, types.ErrNoBalance) {
				t.Fatalf("MsgForward accepted input the query rejected: %v", err)
			}
			return
		}

		// Nothing is deposited, so a matching address fails only for lack of balance.
		if err := forward(resp.Address, hook, metadata); !errors.Is(err, types.ErrNoBalance) {
			t.Fatalf("MsgForward disagrees with the queried address: %v", err)
		}

		hookID, meta, _ := decodeBinding(hook, metadata)
		otherHookID, otherMeta, ok := decodeBinding(otherHook, otherMetadata)
		if !ok {
			return
		}
		err = forward(resp.Address, otherHook, otherMetadata)
		same := bytes.Equal(hookID, otherHookID) && bytes.Equal(meta, otherMeta)
		if same && !errors.Is(err, types.ErrNoBalance) {
			t.Fatalf("equivalent binding rejected: %v", err)
		}
		if !same && !errors.Is(err, types.ErrAddressMismatch) {
			t.Fatalf("different binding not rejected as a mismatch: %v", err)
		}
	})
}

// fuzzAmount reads a non-negative amount below 2^248 so sums of a few amounts
// stay within math.Int's 256-bit range, as bank balances bounded by the total
// supply do.
func fuzzAmount(b []byte) math.Int {
	return math.NewIntFromBigInt(new(big.Int).SetBytes(b[:min(len(b), 31)]))
}

// FuzzForwardIGPFee drives MsgForward and QuoteForwardingFee with arbitrary
// deposits, locked amounts, quoted fees, relayer limits and balances, and a
// warp transfer that consumes an arbitrary share of the quoted fee. Neither may
// panic; the quote must be the hook's fee; the forward must succeed exactly
// when its preconditions hold; and on success the relayer's net cost must equal
// the fee actually consumed, so the refunded excess lies in [0, quoted].
func FuzzForwardIGPFee(f *testing.F) {
	n := func(v int64) []byte { return big.NewInt(v).Bytes() }
	maxAmt := bytes.Repeat([]byte{0xff}, 31)
	f.Add(n(1000), n(0), n(100), n(100), n(200), n(80), n(0), byte(0))
	f.Add(n(1000), n(0), n(100), n(100), n(200), n(80), n(5), byte(1))
	f.Add(n(1000), n(400), n(0), n(0), n(0), n(0), n(0), byte(0))
	f.Add(n(1000), n(1000), n(100), n(100), n(200), n(0), n(0), byte(0))
	f.Add(n(1000), n(0), n(100), n(99), n(200), n(100), n(0), byte(0))
	f.Add(n(1000), n(0), n(100), n(100), n(50), n(10), n(0), byte(0))
	f.Add(n(1000), n(0), n(100), n(100), n(200), n(10), n(0), byte(2))
	f.Add(n(1000), n(0), n(100), n(100), n(200), n(10), n(0), byte(4))
	f.Add(maxAmt, n(0), maxAmt, maxAmt, maxAmt, maxAmt, maxAmt, byte(1))
	f.Add(maxAmt, maxAmt[:30], maxAmt, maxAmt, maxAmt, n(0), n(0), byte(0))

	const otherDenom = "uother"
	f.Fuzz(func(t *testing.T, depositB, lockedB, quotedB, maxFeeB, signerB, usedB, extraB []byte, flags byte) {
		deposit, locked, quoted := fuzzAmount(depositB), fuzzAmount(lockedB), fuzzAmount(quotedB)
		maxFee, signerBal, extra := fuzzAmount(maxFeeB), fuzzAmount(signerB), fuzzAmount(extraB)
		// A hook charges at most the max fee it is given, which is the quote.
		used := fuzzAmount(usedB).Mod(quoted.AddRaw(1))
		feeDenom := appconsts.BondDenom
		if flags&1 != 0 {
			feeDenom = otherDenom
		}
		wrongMaxDenom := flags&2 != 0
		// A second fee coin only accompanies a positive quote.
		multiDenom := flags&4 != 0 && quoted.IsPositive()

		s := newTestIGPSetup(t)
		fwd, signer := s.forwardAddr.String(), s.signer.String()
		s.bankKeeper.Balances[fwd] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, deposit))
		if feeDenom != appconsts.BondDenom {
			s.bankKeeper.Balances[fwd] = s.bankKeeper.Balances[fwd].Add(sdk.NewCoin(feeDenom, extra))
		}
		s.bankKeeper.Locked[fwd] = sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, locked))
		s.bankKeeper.Balances[signer] = sdk.NewCoins(sdk.NewCoin(feeDenom, signerBal))
		s.hyperlaneKeeper.QuotedFee = sdk.NewCoins(sdk.NewCoin(feeDenom, quoted))
		if multiDenom {
			s.hyperlaneKeeper.QuotedFee = s.hyperlaneKeeper.QuotedFee.Add(sdk.NewCoin("uthird", math.OneInt()))
		}
		spendable := math.MaxInt(deposit.Sub(locked), math.ZeroInt())
		s.warpKeeper.OnTransfer = func(sender string, fee sdk.Coin) {
			// A zero quote is passed on as zero of the bond denom.
			if (quoted.IsPositive() && fee.Denom != feeDenom) || fee.Amount.LT(used) {
				t.Fatalf("warp transfer got max fee %s, want at least %s%s", fee, used, feeDenom)
			}
			// The transfer moves the forwarded tokens; the hook spends used.
			for _, c := range []sdk.Coin{sdk.NewCoin(appconsts.BondDenom, spendable), sdk.NewCoin(feeDenom, used)} {
				if c.IsPositive() {
					s.bankKeeper.Balances[sender] = s.bankKeeper.Balances[sender].Sub(c)
				}
			}
		}

		query := keeper.NewQueryServerImpl(keeper.NewKeeper(s.bankKeeper, s.warpKeeper, s.hyperlaneKeeper))
		quote, err := query.QuoteForwardingFee(s.ctx, &types.QueryQuoteForwardingFeeRequest{DestDomain: s.destDomain, TokenId: s.tokenID})
		switch {
		case multiDenom:
			if err == nil {
				t.Fatal("multi-denom quote accepted")
			}
		case err != nil:
			t.Fatalf("quote failed: %v", err)
		case !quote.Fee.Amount.Equal(quoted) || (quoted.IsPositive() && quote.Fee.Denom != feeDenom):
			t.Fatalf("quote %s, want %s%s", quote.Fee, quoted, feeDenom)
		}

		maxDenom := feeDenom
		if wrongMaxDenom {
			maxDenom = "uwrong"
		}
		msg := types.NewMsgForward(signer, fwd, s.destDomain, s.destRecipient, s.tokenID, sdk.NewCoin(maxDenom, maxFee))
		resp, err := s.msgServer.Forward(s.ctx, msg)

		want := spendable.IsPositive() && !multiDenom
		if quoted.IsPositive() {
			want = want && !wrongMaxDenom && maxFee.GTE(quoted) && signerBal.GTE(quoted)
		}
		if want != (err == nil) {
			t.Fatalf("Forward err = %v, want success=%t", err, want)
		}
		if err != nil {
			return
		}
		if !resp.Amount.Equal(spendable) {
			t.Fatalf("forwarded %s, want spendable %s", resp.Amount, spendable)
		}
		if got, wantBal := s.bankKeeper.GetBalance(s.ctx, s.signer, feeDenom).Amount, signerBal.Sub(used); !got.Equal(wantBal) {
			t.Fatalf("relayer balance %s, want %s (quoted %s, used %s)", got, wantBal, quoted, used)
		}
		if feeDenom != appconsts.BondDenom {
			if got := s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, feeDenom).Amount; !got.Equal(extra) {
				t.Fatalf("forwarding address %s balance %s, want untouched %s", feeDenom, got, extra)
			}
		}
		// Only the spendable deposit leaves; every fee coin is spent or refunded.
		wantFwd := math.MinInt(locked, deposit)
		if got := s.bankKeeper.GetBalance(s.ctx, s.forwardAddr, appconsts.BondDenom).Amount; !got.Equal(wantFwd) {
			t.Fatalf("forwarding address keeps %s, want the locked %s", got, wantFwd)
		}
	})
}
