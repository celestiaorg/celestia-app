package hdrchain_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"

	cmtjson "github.com/cometbft/cometbft/libs/json"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/hdrchain"
	"github.com/celestiaorg/celestia-app/tools/fastsync/internal/testchain"
)

// blockchainServer serves /blockchain for c; mutate may alter each response.
func blockchainServer(t *testing.T, c *testchain.Chain, mutate func([]*types.BlockMeta) []*types.BlockMeta) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lo, _ := strconv.ParseInt(r.URL.Query().Get("minHeight"), 10, 64)
		hi, _ := strconv.ParseInt(r.URL.Query().Get("maxHeight"), 10, 64)
		var metas []*types.BlockMeta
		for h := hi; h >= lo; h-- {
			metas = append(metas, types.NewBlockMeta(c.Blocks[h], c.Parts[h]))
		}
		if mutate != nil {
			metas = mutate(metas)
		}
		res, err := cmtjson.Marshal(&coretypes.ResultBlockchainInfo{LastHeight: c.N(), BlockMetas: metas})
		require.NoError(t, err)
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": -1, "result": json.RawMessage(res)})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func heights(hs []*types.Header) []int64 {
	var out []int64
	for _, h := range hs {
		out = append(out, h.Height)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func TestHTTPSourceHeaders(t *testing.T) {
	c := testchain.New(t, 50)
	srv := blockchainServer(t, c, nil)
	src := hdrchain.Parallel(hdrchain.NewHTTPSource(srv.URL), 3, hdrchain.MaxBlockchainMetas)
	hs, err := src.Headers(context.Background(), 2, 47)
	require.NoError(t, err)
	require.Len(t, hs, 46)
	require.EqualValues(t, 2, heights(hs)[0])
	for _, h := range hs {
		require.Equal(t, []byte(c.Header(h.Height).Hash()), []byte(h.Hash()))
	}
}

func TestHTTPSourceRejectsTooManyMetas(t *testing.T) {
	c := testchain.New(t, 5)
	srv := blockchainServer(t, c, func(m []*types.BlockMeta) []*types.BlockMeta { return append(m, m[0]) })
	src := hdrchain.NewHTTPSource(srv.URL)
	src.Retries = 0
	_, err := src.Headers(context.Background(), 2, 3)
	require.ErrorContains(t, err, "returned 3 metas, asked for 2")
}

func TestParallelPropagatesErrors(t *testing.T) {
	src := hdrchain.Parallel(failing{at: 30}, 4, 10)
	_, err := src.Headers(context.Background(), 1, 100)
	require.ErrorContains(t, err, "boom at 30")
}

type failing struct{ at int64 }

func (f failing) Headers(_ context.Context, lo, hi int64) ([]*types.Header, error) {
	if lo <= f.at && f.at <= hi {
		return nil, fmt.Errorf("boom at %d", f.at)
	}
	return nil, nil
}
