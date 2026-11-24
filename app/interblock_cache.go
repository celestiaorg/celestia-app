package app

import (
	"context"
	"fmt"
	"math"
	"strings"

	errorsmod "cosmossdk.io/errors"
	storecache "cosmossdk.io/store/cache"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	gogogrpc "github.com/cosmos/gogoproto/grpc"
	"github.com/spf13/cast"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// The inter-block cache is a write-through cache in front of every IAVL store.
// A read that misses it reads the tree and then fills the cache; neither step
// is atomic with block writes. A CheckTx, Simulate or query that reads through
// it while FinalizeBlock writes the block into the root store can leave a
// stale value in the cache, which the next block then reads: a consensus
// failure. The cosmos-sdk fork of this app lacks the baseapp locking that
// makes the cache safe for such readers (celestia-app#7750), so the
// multiplexer turns the SDK's own cache off.
//
// "sync-only" turns the cache on together with refusing every entry point
// that reads through it outside block execution: CheckTx, QuerySequence, ABCI
// Query, all app gRPC services, Simulate and snapshot restore. Block
// execution is sequential on the consensus connection, so the cache stays
// coherent and every result, event and gas value is unchanged. Info is still
// served: it reads committed versions that bypass the cache.
const (
	// FlagInterBlockCache is the app.toml key that selects the mode.
	FlagInterBlockCache = "performance.inter-block-cache"
	// FlagInterBlockCacheSize is the app.toml key for the number of entries
	// cached per store in "sync-only" mode.
	FlagInterBlockCacheSize = "performance.inter-block-cache-size"

	InterBlockCacheOff      = "off"
	InterBlockCacheSyncOnly = "sync-only"

	// DefaultInterBlockCacheSize is the number of entries cached per store.
	DefaultInterBlockCacheSize = 100_000
)

// ErrInterBlockCacheSyncOnly is returned by every entry point refused in
// "sync-only" mode.
var ErrInterBlockCacheSyncOnly = errorsmod.Wrap(sdkerrors.ErrInvalidRequest,
	`performance.inter-block-cache is "sync-only": this node only replays blocks in this app version and serves no transactions, queries or state sync`)

// InterBlockCacheConfig is the parsed [performance] inter-block cache config.
type InterBlockCacheConfig struct {
	SyncOnly bool
	Size     uint
}

// ParseInterBlockCacheConfig reads performance.inter-block-cache and
// performance.inter-block-cache-size. A missing or empty mode means "off"; a
// missing size means DefaultInterBlockCacheSize.
func ParseInterBlockCacheConfig(appOpts servertypes.AppOptions) (InterBlockCacheConfig, error) {
	cfg := InterBlockCacheConfig{Size: DefaultInterBlockCacheSize}
	if appOpts == nil {
		return cfg, nil
	}

	raw := appOpts.Get(FlagInterBlockCache)
	mode, err := cast.ToStringE(raw)
	mode = strings.ToLower(strings.TrimSpace(mode))
	if err != nil || (mode != "" && mode != InterBlockCacheOff && mode != InterBlockCacheSyncOnly) {
		return cfg, fmt.Errorf("invalid %s %q: must be %q or %q", FlagInterBlockCache, fmt.Sprint(raw), InterBlockCacheOff, InterBlockCacheSyncOnly)
	}
	cfg.SyncOnly = mode == InterBlockCacheSyncOnly

	if rawSize := appOpts.Get(FlagInterBlockCacheSize); rawSize != nil {
		size, err := cast.ToInt64E(rawSize)
		if err != nil || size <= 0 || size > math.MaxInt32 {
			return cfg, fmt.Errorf("invalid %s %q: must be an integer between 1 and %d", FlagInterBlockCacheSize, fmt.Sprint(rawSize), math.MaxInt32)
		}
		cfg.Size = uint(size)
	}
	return cfg, nil
}

// withInterBlockCache returns baseAppOptions with the inter-block cache
// installed after the caller's options, so that it overrides the SDK's
// --inter-block-cache flag, if cfg selects "sync-only".
func withInterBlockCache(cfg InterBlockCacheConfig, baseAppOptions []func(*baseapp.BaseApp)) []func(*baseapp.BaseApp) {
	if !cfg.SyncOnly {
		return baseAppOptions
	}
	opts := make([]func(*baseapp.BaseApp), 0, len(baseAppOptions)+1)
	opts = append(opts, baseAppOptions...)
	return append(opts, baseapp.SetInterBlockCache(storecache.NewCommitKVStoreCacheManager(cfg.Size)))
}

// InterBlockCacheSyncOnly reports whether the inter-block cache is on and the
// app refuses transactions, queries and state sync.
func (app *App) InterBlockCacheSyncOnly() bool {
	return app.interBlockCacheSyncOnly
}

// Query refuses every ABCI query except peer filtering in "sync-only" mode.
func (app *App) Query(ctx context.Context, req *abci.RequestQuery) (*abci.ResponseQuery, error) {
	if app.interBlockCacheSyncOnly && !strings.HasPrefix(req.Path, "/p2p/") {
		return sdkerrors.QueryResult(ErrInterBlockCacheSyncOnly, false), nil
	}
	return app.BaseApp.Query(ctx, req)
}

// Simulate refuses in "sync-only" mode; it reads check state.
func (app *App) Simulate(txBytes []byte) (sdk.GasInfo, *sdk.Result, error) {
	if app.interBlockCacheSyncOnly {
		return sdk.GasInfo{}, nil, ErrInterBlockCacheSyncOnly
	}
	return app.BaseApp.Simulate(txBytes)
}

// OfferSnapshot rejects every snapshot in "sync-only" mode: restoring writes
// the stores beneath the cache.
func (app *App) OfferSnapshot(req *abci.RequestOfferSnapshot) (*abci.ResponseOfferSnapshot, error) {
	if app.interBlockCacheSyncOnly {
		return &abci.ResponseOfferSnapshot{Result: abci.ResponseOfferSnapshot_REJECT}, nil
	}
	return app.BaseApp.OfferSnapshot(req)
}

// ApplySnapshotChunk aborts state sync in "sync-only" mode.
func (app *App) ApplySnapshotChunk(req *abci.RequestApplySnapshotChunk) (*abci.ResponseApplySnapshotChunk, error) {
	if app.interBlockCacheSyncOnly {
		return &abci.ResponseApplySnapshotChunk{Result: abci.ResponseApplySnapshotChunk_ABORT}, nil
	}
	return app.BaseApp.ApplySnapshotChunk(req)
}

// RegisterGRPCServer registers the app's gRPC services. In "sync-only" mode
// every method answers codes.Unavailable without running.
func (app *App) RegisterGRPCServer(server gogogrpc.Server) {
	if app.interBlockCacheSyncOnly {
		server = refusingGRPCServer{Server: server}
	}
	app.BaseApp.RegisterGRPCServer(server)
}

// refusingGRPCServer registers every service with handlers that refuse.
type refusingGRPCServer struct {
	gogogrpc.Server
}

func (s refusingGRPCServer) RegisterService(sd *grpc.ServiceDesc, ss any) {
	desc := *sd
	desc.Methods = make([]grpc.MethodDesc, len(sd.Methods))
	for i, m := range sd.Methods {
		desc.Methods[i] = grpc.MethodDesc{MethodName: m.MethodName, Handler: refuseUnary}
	}
	desc.Streams = make([]grpc.StreamDesc, len(sd.Streams))
	for i, st := range sd.Streams {
		st.Handler = refuseStream
		desc.Streams[i] = st
	}
	s.Server.RegisterService(&desc, ss)
}

func refuseUnary(any, context.Context, func(any) error, grpc.UnaryServerInterceptor) (any, error) {
	return nil, status.Error(codes.Unavailable, ErrInterBlockCacheSyncOnly.Error())
}

func refuseStream(any, grpc.ServerStream) error {
	return status.Error(codes.Unavailable, ErrInterBlockCacheSyncOnly.Error())
}
