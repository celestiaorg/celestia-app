package grpc

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/fibre/validator"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/x/valaddr/types"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/filecoin-project/go-clock"
	"github.com/stretchr/testify/require"
	grpc2 "google.golang.org/grpc"
)

const (
	hostOpWidth        = 4
	hostFuzzValidators = 4
)

// fuzzHostChain is a query client over a scripted provider registry: each
// call answers with the outcome the harness set for it and counts queries.
type fuzzHostChain struct {
	hosts    map[string]string // consensus address -> registered host
	outcome  byte              // next FibreProviderInfo: 0 found, 1 not found, 2 error, 3 found without info
	failAll  bool
	bulk     []types.FibreProvider
	queries  map[string]int
	bulkHits int
}

func (q *fuzzHostChain) FibreProviderInfo(_ context.Context, in *types.QueryFibreProviderInfoRequest, _ ...grpc2.CallOption) (*types.QueryFibreProviderInfoResponse, error) {
	q.queries[in.ValidatorConsensusAddress]++
	switch q.outcome {
	case 0:
		return &types.QueryFibreProviderInfoResponse{Found: true, Info: &types.FibreProviderInfo{Host: q.hosts[in.ValidatorConsensusAddress]}}, nil
	case 1:
		return &types.QueryFibreProviderInfoResponse{}, nil
	case 3:
		return &types.QueryFibreProviderInfoResponse{Found: true}, nil
	default:
		return nil, errors.New("state node unavailable")
	}
}

func (q *fuzzHostChain) AllBondedFibreProviders(context.Context, *types.QueryAllBondedFibreProvidersRequest, ...grpc2.CallOption) (*types.QueryAllBondedFibreProvidersResponse, error) {
	q.bulkHits++
	if q.failAll {
		return nil, errors.New("state node unavailable")
	}
	return &types.QueryAllBondedFibreProvidersResponse{Providers: q.bulk}, nil
}

// FuzzHostRegistry drives host resolution and bulk pulls for four validators
// against a scripted query client and a mock clock, with found, not-found,
// failing and found-without-info answers, host rotations and clock advances across the refresh
// window. A model of the last resolution time and last known host per
// validator predicts every answer: a validator is queried at most once per
// window, a failed query serves the last known host, and no host is returned
// for a validator that was never resolved to it.
func FuzzHostRegistry(f *testing.F) {
	resolve := func(v, outcome byte) []byte { return []byte{0, v, outcome} }
	advance := func(sixteenths byte) []byte { return []byte{1, sixteenths} }
	pull := func(mask, fail byte) []byte { return []byte{2, mask, fail} }
	rotate := func(v byte) []byte { return []byte{3, v} }
	f.Add(uint8(5), []byte{})
	// Resolve, hit the window, rotate, re-query after the window and fail over.
	f.Add(uint8(5), fuzzutil.Records(hostOpWidth,
		resolve(0, 0), resolve(0, 0), rotate(0), advance(15), resolve(0, 0), advance(1), resolve(0, 0),
		advance(16), resolve(0, 2), resolve(1, 2), resolve(1, 0), advance(16), resolve(1, 1), advance(16), resolve(1, 3), resolve(2, 3)))
	// A bulk pull warms some validators; a failed pull changes nothing.
	f.Add(uint8(0), fuzzutil.Records(hostOpWidth,
		pull(0b0101, 0), resolve(0, 2), resolve(1, 2), rotate(2), advance(32), resolve(2, 2), pull(0b1111, 1), resolve(3, 1), pull(0b11000, 0), resolve(3, 0)))

	f.Fuzz(func(t *testing.T, intervalSecs uint8, ops []byte) {
		interval := time.Duration(intervalSecs%30+1) * time.Second
		vals := make([]*core.Validator, hostFuzzValidators)
		addrs := make([]string, hostFuzzValidators)
		chain := &fuzzHostChain{hosts: map[string]string{}, queries: map[string]int{}}
		version := make([]int, hostFuzzValidators)
		for i := range vals {
			vals[i] = &core.Validator{Address: make([]byte, 20), VotingPower: 1}
			vals[i].Address[0] = byte(i + 1)
			addrs[i] = sdk.ConsAddress(vals[i].Address).String()
			chain.hosts[addrs[i]] = fmt.Sprintf("val%d-0.example:2121", i)
		}
		clk := clock.NewMock()
		g := NewHostRegistry(chain, slog.New(slog.DiscardHandler), WithClock(clk), WithRefreshInterval(interval))

		lastRefresh := map[string]time.Time{}
		lastHost := map[string]string{}
		served := map[string]map[string]bool{} // hosts the chain ever reported per validator
		serve := func(addr, host string) {
			if served[addr] == nil {
				served[addr] = map[string]bool{}
			}
			served[addr][host] = true
		}

		c := fuzzutil.New(ops)
		for n := 0; n < 128 && !c.Exhausted(); n++ {
			r := c.Record(hostOpWidth)
			switch r.Byte() % 4 {
			case 0:
				v := int(r.Byte()) % hostFuzzValidators
				addr := addrs[v]
				chain.outcome = r.Byte() % 4
				before := chain.queries[addr]
				last, resolved := lastRefresh[addr]
				fallback, hasFallback := lastHost[addr]
				inWindow := resolved && clk.Now().Sub(last) < interval

				host, err := g.resolveHost(context.Background(), vals[v])

				if inWindow {
					require.Equal(t, before, chain.queries[addr], "queried within the refresh window")
				} else {
					require.Equal(t, before+1, chain.queries[addr], "expected exactly one query")
					lastRefresh[addr] = clk.Now()
				}
				switch {
				case !inWindow && chain.outcome == 0:
					lastHost[addr] = chain.hosts[addr]
					serve(addr, chain.hosts[addr])
					require.NoError(t, err)
					require.Equal(t, validator.Host(chain.hosts[addr]), host)
				case hasFallback:
					require.NoError(t, err)
					require.Equal(t, validator.Host(fallback), host, "must serve the last known host")
				default:
					require.Error(t, err)
					require.Empty(t, host)
				}
				if err == nil {
					require.True(t, served[addr][string(host)], "returned a host the chain never reported for this validator")
				}
			case 1:
				clk.Add(interval * time.Duration(r.Byte()%48) / 16)
			case 2:
				mask, fail := r.Byte(), r.Bool()
				chain.failAll = fail
				chain.bulk = nil
				for v := range hostFuzzValidators {
					if mask&(1<<v) != 0 {
						chain.bulk = append(chain.bulk, types.FibreProvider{ValidatorConsensusAddress: addrs[v], Info: types.FibreProviderInfo{Host: chain.hosts[addrs[v]]}})
					}
				}
				if mask&(1<<hostFuzzValidators) != 0 { // a provider the client never asks about
					chain.bulk = append(chain.bulk, types.FibreProvider{ValidatorConsensusAddress: "stranger", Info: types.FibreProviderInfo{Host: "stranger.example:2121"}})
				}
				before := chain.bulkHits
				err := g.PullAll(context.Background())
				require.Equal(t, before+1, chain.bulkHits)
				if fail {
					require.Error(t, err)
					break
				}
				require.NoError(t, err)
				for _, p := range chain.bulk {
					lastHost[p.ValidatorConsensusAddress] = p.Info.Host
					lastRefresh[p.ValidatorConsensusAddress] = clk.Now()
					serve(p.ValidatorConsensusAddress, p.Info.Host)
				}
			case 3:
				v := int(r.Byte()) % hostFuzzValidators
				version[v]++
				chain.hosts[addrs[v]] = fmt.Sprintf("val%d-%d.example:2121", v, version[v])
			}

			g.mu.RLock()
			for _, addr := range addrs {
				require.Equal(t, lastHost[addr], string(g.lastHosts[addr]), "last known host diverged")
				require.Equal(t, lastRefresh[addr], g.lastRefresh[addr], "refresh time diverged")
			}
			g.mu.RUnlock()
		}
	})
}
