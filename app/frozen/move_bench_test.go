//go:build benchmarks

package frozen_test

import (
	"math"
	mrand "math/rand"
	"os"
	"strconv"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/frozen"
	dbm "github.com/cosmos/cosmos-db"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// Profile of the 41,963-address list: accounts with stake, unbondings, liquid TIA.
const (
	listSize      = 41963
	listStaked    = 8321
	listUnbonding = 3742
	listLiquid    = 55
)

// Delegations per staked account, from an on-chain snapshot of 6,755 listed accounts.
var ndelDist = [][2]int{{1, 4933}, {2, 1007}, {3, 423}, {4, 203}, {5, 87}, {6, 37}, {7, 24}, {8, 17}, {9, 7}, {10, 1}, {11, 5}, {12, 5}, {14, 2}, {15, 2}, {17, 1}, {18, 1}}

func sampleNdel(r *mrand.Rand) int {
	x := r.Intn(6755)
	for _, d := range ndelDist {
		if x < d[1] {
			return d[0]
		}
		x -= d[1]
	}
	return 1
}

// TestMoveAllTiming times MoveAll over MOVE_N listed accounts with the list's
// profile on a goleveldb-backed app with 100 validators:
//
//	MOVE_N=41963 go test -tags benchmarks -run TestMoveAllTiming -v ./app/frozen/
func TestMoveAllTiming(t *testing.T) {
	n, _ := strconv.Atoi(os.Getenv("MOVE_N"))
	if n == 0 {
		t.Skip("set MOVE_N")
	}
	db, err := dbm.NewGoLevelDB("app", t.TempDir(), nil)
	require.NoError(t, err)
	c := newChain(t, db, 99)
	k := c.app.StakingKeeper
	dest := sdk.MustAccAddressFromBech32(frozen.Destination)

	r := mrand.New(mrand.NewSource(1))
	victims := make([]sdk.AccAddress, n)
	for i := range victims {
		victims[i] = randAddr()
	}
	pick := func(k int) map[int]bool {
		m := map[int]bool{}
		for _, i := range r.Perm(n)[:max(1, k*n/listSize)] {
			m[i] = true
		}
		return m
	}
	staked, unbonding, liquid := pick(listStaked), pick(listUnbonding), pick(listLiquid)
	ndels := 0
	setupW, setupC := c.block(func(ctx sdk.Context) {
		for i, a := range victims {
			if staked[i] {
				for j := sampleNdel(r); j > 0; j-- {
					c.delegate(ctx, a, c.vals[r.Intn(len(c.vals))], 100_000_000)
					ndels++
				}
			}
			if unbonding[i] {
				va := c.vals[r.Intn(len(c.vals))]
				sh := c.delegate(ctx, a, va, 100_000_000)
				_, _, err := k.Undelegate(ctx, a, va, sh)
				require.NoError(t, err)
			}
			if liquid[i] {
				c.mint(ctx, a, 5_000_000)
			}
		}
	})
	t.Logf("setup: %d accounts, %d staked (%d delegations), %d unbonding, %d liquid (setup %s + block %s)",
		n, len(staked), ndels, len(unbonding), len(liquid), setupW, setupC)
	c.block(func(ctx sdk.Context) { c.allocateRewards(ctx, 1_000_000_000) })
	for range 3 {
		_, d := c.block(nil)
		t.Logf("empty block: finalize+commit %s", d)
	}
	c.checkInvariants("before")

	var moved []frozen.Moved
	w, d := c.block(func(ctx sdk.Context) {
		moved, err = frozen.MoveAll(ctx, c.keepers(), victims, dest)
		require.NoError(t, err)
	})
	t.Logf("MoveAll over %d accounts: migration %s, finalize+commit %s, total %s", n, w, d, w+d)
	for range 2 {
		_, d := c.block(nil)
		t.Logf("next empty block: finalize+commit %s", d)
	}
	c.checkInvariants("after")

	ctx := c.ctx()
	total := sdk.NewCoins()
	for i, a := range victims {
		dels, err := k.GetDelegatorDelegations(ctx, a, math.MaxUint16)
		require.NoError(t, err)
		require.Empty(t, dels)
		ubds, err := k.GetUnbondingDelegations(ctx, a, math.MaxUint16)
		require.NoError(t, err)
		require.Empty(t, ubds)
		require.True(t, c.app.BankKeeper.GetAllBalances(ctx, a).IsZero(), "victim %d still holds funds", i)
		total = total.Add(moved[i].Total(denom)...)
	}
	destDels, err := k.GetDelegatorDelegations(ctx, dest, math.MaxUint16)
	require.NoError(t, err)
	t.Logf("all %d listed accounts empty; recorded %s moved; destination has %d delegations, %s liquid",
		n, total, len(destDels), c.app.BankKeeper.GetBalance(ctx, dest, denom).Amount)
}
