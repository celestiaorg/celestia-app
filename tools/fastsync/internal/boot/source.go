package boot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	dbm "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/light/provider"
	lighthttp "github.com/cometbft/cometbft/light/provider/http"
	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	sm "github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/store"
	"github.com/cometbft/cometbft/types"
)

// Source serves the data a bootstrap needs. It is untrusted: Verify checks
// everything it returns against the trusted hash of header H+1.
type Source interface {
	Header(ctx context.Context, h int64) (*types.Header, error)
	// Commit returns a commit for block h (canonical or seen).
	Commit(ctx context.Context, h int64) (*types.Commit, error)
	Validators(ctx context.Context, h int64) (*types.ValidatorSet, error)
	// Params returns the consensus params in effect at height h.
	Params(ctx context.Context, h int64) (types.ConsensusParams, error)
	Close() error
	String() string
}

// RPCSource makes the calls the stock light-client state provider makes
// (/commit, /validators, /consensus_params), on one or more servers. Fields
// that no header authenticates (proposer priorities, consensus params other
// than block max bytes and gas) must be identical on every server.
type RPCSource struct {
	urls  []string
	provs []provider.Provider
	rpcs  []*rpchttp.HTTP
	lbs   map[int64]*types.LightBlock
}

// NewRPCSource returns a source over urls for chainID.
func NewRPCSource(chainID string, urls []string) (*RPCSource, error) {
	if len(urls) == 0 {
		return nil, fmt.Errorf("no RPC servers")
	}
	s := &RPCSource{urls: urls, lbs: map[int64]*types.LightBlock{}}
	for _, u := range urls {
		p, err := lighthttp.New(chainID, u)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", u, err)
		}
		c, err := rpchttp.New(u, "/websocket")
		if err != nil {
			return nil, fmt.Errorf("%s: %w", u, err)
		}
		s.provs, s.rpcs = append(s.provs, p), append(s.rpcs, c)
	}
	return s, nil
}

func (s *RPCSource) lightBlock(ctx context.Context, h int64) (*types.LightBlock, error) {
	if lb := s.lbs[h]; lb != nil {
		return lb, nil
	}
	var errs []string
	for i, p := range s.provs { // any server will do: the content is checked
		lb, err := p.LightBlock(ctx, h)
		if err == nil {
			s.lbs[h] = lb
			return lb, nil
		}
		errs = append(errs, fmt.Sprintf("%s: %v", s.urls[i], err))
	}
	return nil, fmt.Errorf("light block %d: %s", h, strings.Join(errs, "; "))
}

// Header implements Source.
func (s *RPCSource) Header(ctx context.Context, h int64) (*types.Header, error) {
	lb, err := s.lightBlock(ctx, h)
	if err != nil {
		return nil, err
	}
	return lb.Header, nil
}

// Commit implements Source.
func (s *RPCSource) Commit(ctx context.Context, h int64) (*types.Commit, error) {
	lb, err := s.lightBlock(ctx, h)
	if err != nil {
		return nil, err
	}
	return lb.Commit, nil
}

// Validators implements Source. Proposer priorities are not covered by
// ValidatorsHash, so every server must return identical bytes.
func (s *RPCSource) Validators(ctx context.Context, h int64) (*types.ValidatorSet, error) {
	lb, err := s.lightBlock(ctx, h)
	if err != nil {
		return nil, err
	}
	want, err := valsetBytes(lb.ValidatorSet)
	if err != nil {
		return nil, err
	}
	for i := 1; i < len(s.provs); i++ {
		o, err := s.provs[i].LightBlock(ctx, h)
		if err != nil {
			return nil, fmt.Errorf("cross-check validators %d with %s: %w", h, s.urls[i], err)
		}
		got, err := valsetBytes(o.ValidatorSet)
		if err != nil {
			return nil, err
		}
		if !bytes.Equal(got, want) {
			return nil, fmt.Errorf("validators %d: %s and %s disagree (proposer priorities or order are not covered by the header)", h, s.urls[0], s.urls[i])
		}
	}
	return lb.ValidatorSet.Copy(), nil
}

func valsetBytes(vs *types.ValidatorSet) ([]byte, error) {
	if vs == nil {
		return nil, fmt.Errorf("nil validator set")
	}
	pb, err := vs.ToProto()
	if err != nil {
		return nil, err
	}
	return pb.Marshal()
}

// Params implements Source. ConsensusHash covers only block max bytes and
// max gas, so every server must return identical params.
func (s *RPCSource) Params(ctx context.Context, h int64) (types.ConsensusParams, error) {
	var first []byte
	var out types.ConsensusParams
	for i, c := range s.rpcs {
		res, err := c.ConsensusParams(ctx, &h)
		if err != nil {
			return types.ConsensusParams{}, fmt.Errorf("consensus params %d from %s: %w", h, s.urls[i], err)
		}
		if res.BlockHeight != h {
			return types.ConsensusParams{}, fmt.Errorf("%s: consensus params for height %d, asked for %d", s.urls[i], res.BlockHeight, h)
		}
		pb := res.ConsensusParams.ToProto()
		bz, err := pb.Marshal()
		if err != nil {
			return types.ConsensusParams{}, err
		}
		if i == 0 {
			first, out = bz, res.ConsensusParams
		} else if !bytes.Equal(bz, first) {
			return types.ConsensusParams{}, fmt.Errorf("consensus params %d: %s and %s disagree (fields not covered by ConsensusHash)", h, s.urls[0], s.urls[i])
		}
	}
	return out, nil
}

// Close implements Source.
func (s *RPCSource) Close() error { return nil }

func (s *RPCSource) String() string { return "RPC " + strings.Join(s.urls, ",") }

// DataSource reads blockstore.db and state.db of a stopped node that synced
// past H+2, or of a copy of them.
type DataSource struct {
	dir      string
	bdb, sdb dbm.DB
	bs       *store.BlockStore
	st       sm.Store
}

// OpenDataSource opens dir/blockstore.db and dir/state.db.
func OpenDataSource(dir, backend string) (*DataSource, error) {
	bdb, err := dbm.NewDB("blockstore", dbm.BackendType(backend), dir)
	if err != nil {
		return nil, fmt.Errorf("open %s/blockstore.db (is the node stopped?): %w", dir, err)
	}
	sdb, err := dbm.NewDB("state", dbm.BackendType(backend), dir)
	if err != nil {
		_ = bdb.Close()
		return nil, fmt.Errorf("open %s/state.db (is the node stopped?): %w", dir, err)
	}
	return &DataSource{dir: dir, bdb: bdb, sdb: sdb, bs: store.NewBlockStore(bdb),
		st: sm.NewStore(sdb, sm.StoreOptions{DiscardABCIResponses: true})}, nil
}

// recovered turns a panic of the stores on a corrupt record into an error.
func recovered(what string, h int64, err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("source %s %d: %v", what, h, r)
	}
}

// Header implements Source.
func (s *DataSource) Header(_ context.Context, h int64) (hd *types.Header, err error) {
	defer recovered("header", h, &err)
	m := s.bs.LoadBlockMeta(h)
	if m == nil {
		return nil, fmt.Errorf("source block store has no block meta at %d (base %d, height %d)", h, s.bs.Base(), s.bs.Height())
	}
	c := m.Header
	return &c, nil
}

// Commit implements Source: the canonical commit (block h+1's LastCommit),
// else the seen commit.
func (s *DataSource) Commit(_ context.Context, h int64) (c *types.Commit, err error) {
	defer recovered("commit", h, &err)
	if c = s.bs.LoadBlockCommit(h); c != nil {
		return c, nil
	}
	if c = s.bs.LoadSeenCommit(h); c != nil {
		return c, nil
	}
	return nil, fmt.Errorf("source block store has no commit for %d", h)
}

// Validators implements Source.
func (s *DataSource) Validators(_ context.Context, h int64) (vs *types.ValidatorSet, err error) {
	defer recovered("validators", h, &err)
	return s.st.LoadValidators(h)
}

// Params implements Source.
func (s *DataSource) Params(_ context.Context, h int64) (p types.ConsensusParams, err error) {
	defer recovered("consensus params", h, &err)
	return s.st.LoadConsensusParams(h)
}

// Close implements Source.
func (s *DataSource) Close() error { return errors.Join(s.bdb.Close(), s.sdb.Close()) }

func (s *DataSource) String() string { return "data dir " + s.dir }
