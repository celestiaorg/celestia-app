package hdrchain

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	dbm "github.com/cometbft/cometbft-db"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	"github.com/cometbft/cometbft/libs/log"
	"github.com/cometbft/cometbft/light"
	lightdb "github.com/cometbft/cometbft/light/store/db"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cometbft/cometbft/types"
)

// RangeSource serves headers by height range, for backward hash-chain
// checks. It is untrusted like Source.
type RangeSource interface {
	// Headers returns the headers lo..hi (inclusive), in any order.
	Headers(ctx context.Context, lo, hi int64) ([]*types.Header, error)
}

// MaxBlockchainMetas is the most block metas one /blockchain call returns.
const MaxBlockchainMetas = 20

// DecodeBlockchainResponse decodes a /blockchain JSON-RPC response and checks
// that it holds at most want metas.
func DecodeBlockchainResponse(raw []byte, want int64) ([]*types.Header, error) {
	res, err := unwrap(raw)
	if err != nil {
		return nil, err
	}
	var rb coretypes.ResultBlockchainInfo
	if err := cmtjson.Unmarshal(res, &rb); err != nil {
		return nil, fmt.Errorf("decode /blockchain result: %w", err)
	}
	if int64(len(rb.BlockMetas)) > want {
		return nil, fmt.Errorf("/blockchain returned %d metas, asked for %d", len(rb.BlockMetas), want)
	}
	out := make([]*types.Header, 0, len(rb.BlockMetas))
	for _, m := range rb.BlockMetas {
		if m == nil {
			return nil, errors.New("/blockchain returned a nil meta")
		}
		h := m.Header
		out = append(out, &h)
	}
	return out, nil
}

// Headers implements RangeSource with /blockchain.
func (s *HTTPSource) Headers(ctx context.Context, lo, hi int64) ([]*types.Header, error) {
	var out []*types.Header
	for l := lo; l <= hi; l += MaxBlockchainMetas {
		h := min(l+MaxBlockchainMetas-1, hi)
		raw, err := s.get(ctx, "blockchain", url.Values{
			"minHeight": {strconv.FormatInt(l, 10)}, "maxHeight": {strconv.FormatInt(h, 10)},
		})
		if err != nil {
			return nil, err
		}
		hs, err := DecodeBlockchainResponse(raw, h-l+1)
		if err != nil {
			return nil, fmt.Errorf("blockchain %d..%d: %w", l, h, err)
		}
		out = append(out, hs...)
	}
	return out, nil
}

// Headers implements RangeSource from the block store.
func (s *BlockstoreSource) Headers(ctx context.Context, lo, hi int64) ([]*types.Header, error) {
	out := make([]*types.Header, 0, max(hi-lo+1, 0))
	for h := lo; h <= hi; h++ {
		sh, err := s.header(h)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, nil
}

func (s *BlockstoreSource) header(h int64) (hd *types.Header, err error) {
	defer func() {
		if r := recover(); r != nil {
			hd, err = nil, fmt.Errorf("blockstore height %d: %v", h, r)
		}
	}()
	meta := s.Blocks.LoadBlockMeta(h)
	if meta == nil {
		return nil, fmt.Errorf("blockstore: no block meta at %d", h)
	}
	c := meta.Header
	return &c, nil
}

// Parallel splits each Headers call into chunks fetched by n concurrent workers.
func Parallel(src RangeSource, n int, chunk int64) RangeSource {
	return parallel{src: src, n: max(n, 1), chunk: max(chunk, 1)}
}

type parallel struct {
	src   RangeSource
	n     int
	chunk int64
}

func (p parallel) Headers(ctx context.Context, lo, hi int64) ([]*types.Header, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type job struct{ lo, hi int64 }
	jobs := make(chan job)
	var (
		mu       sync.Mutex
		out      []*types.Header
		firstErr error
		wg       sync.WaitGroup
	)
	for range p.n {
		wg.Go(func() {
			for j := range jobs {
				hs, err := p.src.Headers(ctx, j.lo, j.hi)
				mu.Lock()
				if err != nil && firstErr == nil {
					firstErr = err
					cancel()
				}
				out = append(out, hs...)
				mu.Unlock()
			}
		})
	}
send:
	for l := lo; l <= hi; l += p.chunk {
		select {
		case jobs <- job{l, min(l+p.chunk-1, hi)}:
		case <-ctx.Done():
			break send
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr == nil {
		firstErr = ctx.Err()
	}
	return out, firstErr
}

// LightClientAnchor verifies the header at height with a light client started
// from the trust root (primary = first server, witnesses = the rest) and
// returns its hash.
func LightClientAnchor(ctx context.Context, chainID string, trust light.TrustOptions, servers []string, height int64) ([]byte, error) {
	if len(servers) == 0 {
		return nil, errors.New("no RPC servers")
	}
	witnesses := servers[1:]
	if len(witnesses) == 0 {
		witnesses = servers[:1] // the light client needs a witness; the primary is a weak one
	}
	lc, err := light.NewHTTPClient(ctx, chainID, trust, servers[0], witnesses,
		lightdb.New(dbm.NewMemDB(), "fshdr"), light.Logger(log.NewNopLogger()), light.MaxRetryAttempts(5))
	if err != nil {
		return nil, fmt.Errorf("light client: %w", err)
	}
	lb, err := lc.VerifyLightBlockAtHeight(ctx, height, time.Now())
	if err != nil {
		return nil, fmt.Errorf("light client verify height %d: %w", height, err)
	}
	return lb.Hash(), nil
}
