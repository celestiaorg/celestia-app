package expected

import (
	"bytes"
	"context"
	"strings"
	"testing"

	dbm "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/hdrchain"
	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/testchain"
)

type memSource struct {
	c      *testchain.Chain
	mutate func(*types.Header)
}

func (m memSource) Headers(_ context.Context, lo, hi int64) ([]*types.Header, error) {
	var out []*types.Header
	for h := lo; h <= hi; h++ {
		hd := *m.c.Header(h)
		if m.mutate != nil {
			m.mutate(&hd)
		}
		out = append(out, &hd)
	}
	return out, nil
}

func walk(t *testing.T, c *testchain.Chain, src hdrchain.RangeSource, p Params) ([]Row, error) {
	t.Helper()
	var windows [][]Row
	err := Walk(context.Background(), p, src, func(rows []Row) error {
		windows = append(windows, rows)
		return nil
	})
	var all []Row
	for i := len(windows) - 1; i >= 0; i-- {
		all = append(all, windows[i]...)
	}
	return all, err
}

func TestWalk(t *testing.T) {
	c := testchain.New(t, 30)
	for _, anchor := range []int64{21, 25, 30} { // anchor at to+1 and above the range
		p := Params{ChainID: testchain.ChainID, AnchorHeight: anchor, AnchorHash: c.Header(anchor).Hash(), From: 3, To: 20, Window: 4}
		rows, err := walk(t, c, memSource{c: c}, p)
		require.NoError(t, err)
		require.Len(t, rows, 18)
		for i, r := range rows {
			h := int64(3 + i)
			require.Equal(t, h, r.Height)
			require.Equal(t, testchain.Hash("app", h+1), r.AppHash)
			require.Equal(t, []byte(c.Header(h+1).Hash()), r.NextHeaderHash)
			require.EqualValues(t, testchain.AppVersion, r.AppVersion)
		}
	}
}

func TestWalkFromBlockstore(t *testing.T) {
	c := testchain.New(t, 12)
	db := dbm.NewMemDB()
	bs := store.NewBlockStore(db)
	for h := int64(1); h <= c.N(); h++ {
		bs.SaveBlock(c.Blocks[h], c.Parts[h], c.Commits[h])
	}
	p := Params{ChainID: testchain.ChainID, AnchorHeight: 12, AnchorHash: c.Header(12).Hash(), From: 1, To: 11, Window: 5}
	rows, err := walk(t, c, &hdrchain.BlockstoreSource{Blocks: bs}, p)
	require.NoError(t, err)
	require.Len(t, rows, 11)
}

func TestWalkRejects(t *testing.T) {
	c := testchain.New(t, 20)
	good := Params{ChainID: testchain.ChainID, AnchorHeight: 16, AnchorHash: c.Header(16).Hash(), From: 2, To: 15, Window: 6}
	for name, tc := range map[string]struct {
		p      func(Params) Params
		mutate func(*types.Header)
		want   string
	}{
		"forged app hash": {mutate: func(h *types.Header) {
			if h.Height == 9 {
				h.AppHash = bytes.Repeat([]byte{1}, 32)
			}
		}, want: "hash chain broken at height 9"},
		"wrong anchor": {p: func(p Params) Params { p.AnchorHash = c.Header(15).Hash(); return p }, want: "hash chain broken at height 16"},
		"other chain":  {p: func(p Params) Params { p.ChainID = "other"; return p }, want: "chain id"},
		"bad params":   {p: func(p Params) Params { p.AnchorHeight = p.To; return p }, want: "anchor height"},
	} {
		t.Run(name, func(t *testing.T) {
			p := good
			if tc.p != nil {
				p = tc.p(p)
			}
			_, err := walk(t, c, memSource{c: c, mutate: tc.mutate}, p)
			require.ErrorContains(t, err, tc.want)
		})
	}
	t.Run("missing header", func(t *testing.T) {
		_, err := walk(t, c, gap{memSource{c: c}, 7}, good)
		require.ErrorContains(t, err, "no header for height 7")
	})
}

type gap struct {
	memSource
	skip int64
}

func (g gap) Headers(ctx context.Context, lo, hi int64) ([]*types.Header, error) {
	hs, err := g.memSource.Headers(ctx, lo, hi)
	var out []*types.Header
	for _, h := range hs {
		if h.Height != g.skip {
			out = append(out, h)
		}
	}
	return out, err
}

func TestRowRoundTripAndFind(t *testing.T) {
	c := testchain.New(t, 5)
	var sb strings.Builder
	sb.WriteString(Header("verify", testchain.ChainID, "anchor=6:AB") + "\n")
	for h := int64(2); h <= 5; h++ {
		sb.WriteString(FromHeader(c.Header(h)).String() + "\n")
	}
	file := sb.String()
	r, err := find(strings.NewReader(file), testchain.ChainID, 3)
	require.NoError(t, err)
	require.Equal(t, FromHeader(c.Header(4)), r)
	_, err = find(strings.NewReader(file), testchain.ChainID, 9)
	require.ErrorContains(t, err, "no row for height 9")
	for _, bad := range []string{"1\t2", "x\tAA\tAA\tAA\tAA\tAA\t1", "1\tAA\tZZ\tAA\tAA\tAA\t1", "1\tAA\tAA\tAA\tAA\tAA\t1"} {
		_, err := ParseRow(bad)
		require.Error(t, err, bad)
	}
}

func TestFindRequiresVerifiedHeader(t *testing.T) {
	rows := FromHeader(testchain.New(t, 3).Header(3)).String() + "\n"
	for name, first := range map[string]string{
		"unverified dump": "# fshdr headers (UNVERIFIED) block store base=1 height=3",
		"other chain":     Header("verify", "other-chain", "anchor=3:AB"),
		"chain prefix":    Header("forward", testchain.ChainID+"-2", "trusted=1:AB"),
		"no header":       rows,
	} {
		_, err := find(strings.NewReader(first+"\n"+rows), testchain.ChainID, 2)
		require.ErrorContains(t, err, "not a verified fshdr header", name)
	}
	_, err := find(strings.NewReader(Header("forward", testchain.ChainID, "trusted=1:AB")+"\n"+rows), testchain.ChainID, 2)
	require.NoError(t, err)
}
