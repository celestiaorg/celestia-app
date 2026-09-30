package fibre_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	mrand "math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre"
	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v10/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v10/fibre/state"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// System benchmarks across two processes.
//
// Write path: run the sink with SYS_MODE=sink and the client with
// SYS_MODE=client (or encode) on the same host.
//
// Read path: run SYS_MODE=serve (real servers with local stores, seeded with
// SYS_BLOBS blobs) on one host and SYS_MODE=download (real client, SYS_WORKERS
// concurrent downloads against SYS_SERVER) on another. Both derive the same
// blob IDs from a fixed seed. The serve process prints one TICK line per
// second with its cumulative CPU, I/O and RSS.

const sysChainID = "celestia"

// sysReadBlobBytes is the default read-path blob payload: a 128 MiB blob.
var sysReadBlobBytes = 128<<20 - (fibre.DefaultProtocolParams.MaxBlobSize - fibre.DefaultBlobConfigV0().MaxDataSize)

func sysEnvInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil {
		return v
	}
	return def
}

func sysValidators(n int) ([]*core.Validator, []cmted25519.PrivKey) {
	vals := make([]*core.Validator, n)
	keys := make([]cmted25519.PrivKey, n)
	for i := range n {
		k := cmted25519.GenPrivKeyFromSecret(fmt.Appendf(nil, "sys-val-%d", i))
		keys[i] = k
		vals[i] = &core.Validator{Address: k.PubKey().Address(), PubKey: k.PubKey(), VotingPower: 100}
	}
	return vals, keys
}

type sysSink struct {
	types.UnimplementedFibreServer
	pv      *testPrivValidator
	rows    atomic.Int64
	handled atomic.Int64
}

func (s *sysSink) UploadShard(_ context.Context, req *types.UploadShardRequest) (*types.UploadShardResponse, error) {
	var p fibre.PaymentPromise
	if err := p.FromProto(req.Promise); err != nil {
		return nil, err
	}
	sig, err := fibre.SignPaymentPromiseValidator(&p, s.pv)
	if err != nil {
		return nil, err
	}
	s.rows.Add(int64(len(req.Shard.Rows)))
	s.handled.Add(1)
	return &types.UploadShardResponse{ValidatorSignature: sig}, nil
}

func TestSystemSink(t *testing.T) {
	if os.Getenv("SYS_MODE") != "sink" {
		t.Skip("SYS_MODE=sink")
	}
	n := sysEnvInt("SYS_VALIDATORS", 100)
	base := sysEnvInt("SYS_PORT", 21000)
	vals, keys := sysValidators(n)
	params := fibre.DefaultProtocolParams
	sinks := make([]*sysSink, n)
	for i := range vals {
		pv := newTestPrivValidator(keys[i])
		srv, err := fibregrpc.Listen(fmt.Sprintf("127.0.0.1:%d", base+i), 64, 64)
		require.NoError(t, err)
		cert, err := tlsid.BuildServerCert(pv, sysChainID)
		require.NoError(t, err)
		creds := credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})
		// SYS_SINK_OLD_EVERY=n makes every n-th sink TLS-only, like an old server.
		if every := sysEnvInt("SYS_SINK_OLD_EVERY", 0); every == 0 || i%every != 0 {
			creds = fibregrpc.NewDetectingServerCreds(creds)
		}
		sinks[i] = &sysSink{pv: pv}
		var winOpts []grpclib.ServerOption
		if w := sysEnvInt("SYS_SINK_WINDOW", 0); w > 0 {
			winOpts = append(winOpts, grpclib.InitialWindowSize(int32(w)), grpclib.InitialConnWindowSize(int32(sysEnvInt("SYS_SINK_CONN_WINDOW", w))))
		}
		srv.Register(sinks[i], append(winOpts,
			grpclib.Creds(creds),
			grpclib.MaxRecvMsgSize(params.MaxMessageSize()),
			grpclib.ForceServerCodecV2(fibregrpc.NewServerCodec(params.MaxRowsPerValidator(), params.MerkleProofDepth())),
		)...)
		srv.Serve()
	}
	t.Logf("sink: %d validators on ports %d..", n, base)
	for {
		time.Sleep(time.Duration(sysEnvInt("SYS_SINK_TICK_MS", 10000)) * time.Millisecond)
		var h int64
		for _, s := range sinks {
			h += s.handled.Load()
		}
		c := sysRusage()
		fmt.Fprintf(os.Stderr, "sink handled=%d cpu_s=%.2f\n", h, (c.user + c.sys).Seconds())
	}
}

type sysCPU struct{ user, sys time.Duration }

func sysRusage() sysCPU {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return sysCPU{time.Duration(ru.Utime.Nano()), time.Duration(ru.Stime.Nano())}
}

var sysMetricNames = []string{
	"/cpu/classes/gc/total:cpu-seconds",
	"/cpu/classes/idle:cpu-seconds",
	"/cpu/classes/user:cpu-seconds",
	"/cpu/classes/scavenge/total:cpu-seconds",
	"/cpu/classes/total:cpu-seconds",
	"/gc/cycles/total:gc-cycles",
	"/gc/heap/allocs:bytes",
	"/gc/heap/allocs:objects",
	"/sync/mutex/wait/total:seconds",
}

func sysReadMetrics() map[string]float64 {
	s := make([]metrics.Sample, len(sysMetricNames))
	for i, n := range sysMetricNames {
		s[i].Name = n
	}
	metrics.Read(s)
	out := map[string]float64{}
	for _, m := range s {
		switch m.Value.Kind() {
		case metrics.KindFloat64:
			out[m.Name] = m.Value.Float64()
		case metrics.KindUint64:
			out[m.Name] = float64(m.Value.Uint64())
		}
	}
	return out
}

func sysRSS() int64 {
	b, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for l := range strings.SplitSeq(string(b), "\n") {
		if strings.HasPrefix(l, "VmRSS:") {
			f := strings.Fields(l)
			v, _ := strconv.ParseInt(f[1], 10, 64)
			return v << 10
		}
	}
	return 0
}

// sysIO returns the process's cumulative read/write bytes and syscalls from
// /proc/self/io (rchar, wchar, syscr, syscw).
func sysIO() map[string]float64 {
	out := map[string]float64{}
	b, err := os.ReadFile("/proc/self/io")
	if err != nil {
		return out
	}
	for l := range strings.SplitSeq(string(b), "\n") {
		if k, v, ok := strings.Cut(l, ": "); ok {
			out[k], _ = strconv.ParseFloat(v, 64)
		}
	}
	return out
}

// sysMeasure runs workers calling one in a loop and measures the window after
// warm for dur: throughput, CPU, allocations, I/O and RSS per blob, plus
// millisecond percentiles of each phase duration one returns.
func sysMeasure(t *testing.T, workers int, warm, dur time.Duration, dataSize int, phases []string, one func(ctx context.Context, w int) ([]time.Duration, error)) map[string]any {
	var (
		measuring atomic.Bool
		stop      atomic.Bool
		done      atomic.Int64
		mu        sync.Mutex
		peakRSS   atomic.Int64
		peakGor   atomic.Int64
	)
	phaseMs := make([][]float64, len(phases))
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := range workers {
		wg.Go(func() {
			for !stop.Load() {
				ds, err := one(t.Context(), w)
				if err != nil {
					errs <- err
					return
				}
				if measuring.Load() {
					done.Add(1)
					mu.Lock()
					for i, d := range ds {
						phaseMs[i] = append(phaseMs[i], float64(d.Microseconds())/1e3)
					}
					mu.Unlock()
				}
			}
		})
	}
	sampDone := make(chan struct{})
	go func() {
		tk := time.NewTicker(100 * time.Millisecond)
		defer tk.Stop()
		for {
			select {
			case <-sampDone:
				return
			case <-tk.C:
				if !measuring.Load() {
					continue
				}
				if r := sysRSS(); r > peakRSS.Load() {
					peakRSS.Store(r)
				}
				if g := int64(runtime.NumGoroutine()); g > peakGor.Load() {
					peakGor.Store(g)
				}
			}
		}
	}()

	time.Sleep(warm)
	if p := os.Getenv("SYS_CPUPROF"); p != "" {
		f, err := os.Create(p)
		require.NoError(t, err)
		require.NoError(t, pprof.StartCPUProfile(f))
		defer pprof.StopCPUProfile()
	}
	if os.Getenv("SYS_BLOCKPROF") != "" {
		runtime.SetBlockProfileRate(100000)
		runtime.SetMutexProfileFraction(10)
	}
	m0, c0, io0, w0 := sysReadMetrics(), sysRusage(), sysIO(), time.Now()
	measuring.Store(true)
	time.Sleep(dur)
	measuring.Store(false)
	m1, c1, io1, w1 := sysReadMetrics(), sysRusage(), sysIO(), time.Now()
	pprof.StopCPUProfile()
	stop.Store(true)
	close(sampDone)
	for _, pair := range [][2]string{{"SYS_BLOCKPROF", "block"}, {"SYS_BLOCKPROF", "mutex"}, {"SYS_HEAPPROF", "allocs"}, {"SYS_GORPROF", "goroutine"}} {
		if p := os.Getenv(pair[0]); p != "" {
			f, err := os.Create(p + "." + pair[1])
			require.NoError(t, err)
			_ = pprof.Lookup(pair[1]).WriteTo(f, 0)
			_ = f.Close()
		}
	}
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}

	blobs := float64(done.Load())
	wall := w1.Sub(w0).Seconds()
	d := func(k string) float64 { return m1[k] - m0[k] }
	cpu := (c1.user + c1.sys - c0.user - c0.sys).Seconds()
	pct := func(v []float64, q float64) float64 {
		if len(v) == 0 {
			return 0
		}
		sort.Float64s(v)
		return v[int(q*float64(len(v)-1))]
	}
	res := map[string]any{
		"workers": workers, "gomaxprocs": runtime.GOMAXPROCS(0), "blobs": blobs, "wall_s": wall,
		"window_start_ms":    w0.UnixMilli(),
		"window_end_ms":      w1.UnixMilli(),
		"GBps":               blobs * float64(dataSize) / wall / 1e9,
		"cpu_s_per_blob":     cpu / blobs,
		"user_s_per_blob":    (c1.user - c0.user).Seconds() / blobs,
		"sys_s_per_blob":     (c1.sys - c0.sys).Seconds() / blobs,
		"cores_busy":         cpu / wall,
		"go_idle_cores":      d("/cpu/classes/idle:cpu-seconds") / wall,
		"gc_cpu_s_per_blob":  d("/cpu/classes/gc/total:cpu-seconds") / blobs,
		"scav_s_per_blob":    d("/cpu/classes/scavenge/total:cpu-seconds") / blobs,
		"gc_cycles":          d("/gc/cycles/total:gc-cycles"),
		"alloc_MiB_per_blob": d("/gc/heap/allocs:bytes") / blobs / (1 << 20),
		"allocs_per_blob":    d("/gc/heap/allocs:objects") / blobs,
		"mutex_wait_s":       d("/sync/mutex/wait/total:seconds"),
		"peak_rss_GiB":       float64(peakRSS.Load()) / (1 << 30),
		"peak_goroutines":    peakGor.Load(),
	}
	for _, k := range []string{"rchar", "wchar", "syscr", "syscw"} {
		res[k+"_per_blob"] = (io1[k] - io0[k]) / blobs
	}
	for i, name := range phases {
		res[name+"_ms_p50"] = pct(phaseMs[i], 0.5)
		res[name+"_ms_p90"] = pct(phaseMs[i], 0.9)
		res[name+"_ms_p99"] = pct(phaseMs[i], 0.99)
	}
	return res
}

func sysPrintResult(res map[string]any) {
	b, _ := json.Marshal(res)
	fmt.Printf("SYSRESULT %s\n", b)
}

func TestSystemClient(t *testing.T) {
	mode := os.Getenv("SYS_MODE")
	if mode != "client" && mode != "encode" {
		t.Skip("SYS_MODE=client|encode")
	}
	n := sysEnvInt("SYS_VALIDATORS", 100)
	base := sysEnvInt("SYS_PORT", 21000)
	workers := sysEnvInt("SYS_WORKERS", 16)
	dur := time.Duration(sysEnvInt("SYS_SECONDS", 30)) * time.Second
	warm := time.Duration(sysEnvInt("SYS_WARMUP", 5)) * time.Second
	dataSize := sysEnvInt("SYS_BYTES", fibre.DefaultBlobConfigV0().MaxDataSize)
	awaitAll := os.Getenv("SYS_AWAIT_ALL") != "0"

	var client *fibre.Client
	if mode == "client" {
		vals, _ := sysValidators(n)
		addrs := map[string]string{}
		for i, v := range vals {
			addrs[v.Address.String()] = fmt.Sprintf("127.0.0.1:%d", base+i)
		}
		client = sysClient(t, makeTestKeyring(t), vals, addrs, 0, &sysRPCStats{})
		defer func() { _ = client.Stop(context.Background()) }()
	}
	ns := share.MustNewV0Namespace([]byte("sysbench"))
	cfg := fibre.DefaultBlobConfigV0()

	datas := make([][]byte, workers)
	for i := range datas {
		datas[i] = make([]byte, dataSize)
		_, _ = rand.Read(datas[i])
	}

	one := func(ctx context.Context, w int) ([]time.Duration, error) {
		t0 := time.Now()
		blob, err := fibre.NewBlob(datas[w], cfg)
		if err != nil {
			return nil, err
		}
		t1 := time.Now()
		if client != nil {
			var opts []fibre.UploadOption
			if awaitAll {
				opts = append(opts, fibre.WithAwaitAllSignatures())
			}
			_, err = client.Upload(ctx, ns, blob, opts...)
		}
		blob.Free()
		t2 := time.Now()
		if err != nil {
			return nil, err
		}
		if os.Getenv("SYS_TRACE") != "" {
			fmt.Printf("BLOB enc_ms=%d up_ms=%d\n", t1.Sub(t0).Milliseconds(), t2.Sub(t1).Milliseconds())
		}
		return []time.Duration{t1.Sub(t0), t2.Sub(t1), t2.Sub(t0)}, nil
	}
	res := sysMeasure(t, workers, warm, dur, dataSize, []string{"enc", "up", "tot"}, one)
	res["mode"] = mode
	sysPrintResult(res)
}

// sysRPCStats counts the shard RPCs a client issues.
type sysRPCStats struct{ calls, rows, errs atomic.Int64 }

type sysCountingClient struct {
	fibregrpc.Client
	stats *sysRPCStats
}

func (c *sysCountingClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	c.stats.calls.Add(1)
	resp, err := c.Client.DownloadShard(ctx, req, opts...)
	if err != nil {
		c.stats.errs.Add(1)
		return resp, err
	}
	c.stats.rows.Add(int64(len(resp.GetShard().GetRows())))
	return resp, nil
}

// sysClient builds a client that resolves vals through addrs and sees them as
// the validator set. A nil keyring gives a download-only client; a zero
// rpcTimeout keeps the default. Shard RPCs are counted in stats.
func sysClient(t *testing.T, kr keyring.Keyring, vals []*core.Validator, addrs map[string]string, rpcTimeout time.Duration, stats *sysRPCStats) *fibre.Client {
	cfg := fibre.DefaultClientConfig()
	if rpcTimeout > 0 {
		cfg.RPCTimeout = rpcTimeout
	}
	newClient := fibregrpc.DefaultNewClientFn(&testHostRegistry{addresses: addrs}, func() string { return sysChainID }, cfg.MaxMessageSize, nil)
	cfg.NewClientFn = func(ctx context.Context, val *core.Validator) (fibregrpc.Client, error) {
		c, err := newClient(ctx, val)
		if err != nil {
			return nil, err
		}
		return &sysCountingClient{Client: c, stats: stats}, nil
	}
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(vals), Height: 100}
	cfg.StateClientFn = func() (state.Client, error) {
		return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: valSet}, chainID: sysChainID}, nil
	}
	client, err := fibre.NewClient(kr, cfg)
	require.NoError(t, err)
	require.NoError(t, client.Start(t.Context()))
	return client
}

// sysBlobs returns SYS_BLOBS deterministic blobs of dataSize bytes, so the
// serve and download processes agree on blob IDs without talking.
func sysBlobs(t *testing.T, dataSize int) ([]*fibre.Blob, [][]byte) {
	n := sysEnvInt("SYS_BLOBS", 4)
	blobs := make([]*fibre.Blob, n)
	datas := make([][]byte, n)
	for i := range n {
		datas[i] = make([]byte, dataSize)
		mrand.NewChaCha8([32]byte{byte(i), 's', 'y', 's'}).Read(datas[i])
		var err error
		blobs[i], err = fibre.NewBlob(datas[i], fibre.DefaultBlobConfigV0())
		require.NoError(t, err)
	}
	return blobs, datas
}

// tlsOnlyCreds rejects plaintext connections, like a server without plaintext
// support, so clients fall back to TLS; TLS is served by the wrapped creds.
type tlsOnlyCreds struct{ credentials.TransportCredentials }

func (c tlsOnlyCreds) ServerHandshake(raw net.Conn) (net.Conn, credentials.AuthInfo, error) {
	var first [1]byte
	if _, err := io.ReadFull(raw, first[:]); err != nil {
		return nil, nil, err
	}
	if first[0] != 0x16 {
		return nil, nil, errors.New("plaintext rejected")
	}
	return c.TransportCredentials.ServerHandshake(&sysPrefixConn{Conn: raw, prefix: first[:]})
}

func (c tlsOnlyCreds) Clone() credentials.TransportCredentials {
	return tlsOnlyCreds{c.TransportCredentials.Clone()}
}

type sysPrefixConn struct {
	net.Conn
	prefix []byte
}

func (c *sysPrefixConn) Read(p []byte) (int, error) {
	if len(c.prefix) > 0 {
		n := copy(p, c.prefix)
		c.prefix = c.prefix[n:]
		return n, nil
	}
	return c.Conn.Read(p)
}

// TestSystemServe runs SYS_VALIDATORS real servers with local stores on
// SYS_BIND:SYS_PORT.., seeds them with the blobs of sysBlobs and serves until
// killed. SYS_TLS_ONLY=1 serves TLS only, so clients take the TLS fallback.
func TestSystemServe(t *testing.T) {
	if os.Getenv("SYS_MODE") != "serve" {
		t.Skip("SYS_MODE=serve")
	}
	n := sysEnvInt("SYS_VALIDATORS", 100)
	base := sysEnvInt("SYS_PORT", 21000)
	bind := os.Getenv("SYS_BIND")
	if bind == "" {
		bind = "0.0.0.0"
	}
	dir := os.Getenv("SYS_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	tlsOnly := os.Getenv("SYS_TLS_ONLY") == "1"
	dataSize := sysEnvInt("SYS_BYTES", sysReadBlobBytes)

	vals, keys := sysValidators(n)
	params := fibre.DefaultProtocolParams
	params.MaxValidatorCount = max(params.MaxValidatorCount, n)
	valSet := validator.Set{ValidatorSet: core.NewValidatorSet(vals), Height: 100}
	// Info logs one line per served shard; keep them out of the syscall counts
	// unless SYS_LOG_INFO is set.
	level := slog.LevelWarn
	if os.Getenv("SYS_LOG_INFO") != "" {
		level = slog.LevelInfo
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	addrs := map[string]string{}
	for i, val := range vals {
		pv := newTestPrivValidator(keys[i])
		cfg := fibre.NewServerConfigFromParams(params)
		cfg.ServerListenAddress = fmt.Sprintf("%s:%d", bind, base+i)
		if tlsOnly {
			cfg.ServerListenAddress = "127.0.0.1:0"
		}
		cfg.StoreConfig.Path = filepath.Join(dir, strconv.Itoa(i))
		cfg.Log = log
		// Uploads only seed the stores; 100 verifiers per server would not fit.
		cfg.UploadVerifyWorkers = 1
		cfg.StateClientFn = func() (state.Client, error) {
			return &mockStateClient{chainID: sysChainID, SetGetter: &mockValidatorSetGetter{set: valSet}, budget: int64(types.DefaultFullStakeStorageBudget)}, nil
		}
		cfg.SignerFn = func(string) (core.PrivValidator, error) { return pv, nil }
		srv, err := fibre.NewServer(cfg)
		require.NoError(t, err)
		require.NoError(t, srv.Start(t.Context()))
		addrs[val.Address.String()] = srv.ListenAddress()
		if tlsOnly {
			// Expose the same handlers and store on the public port over TLS only.
			pub, err := fibregrpc.Listen(fmt.Sprintf("%s:%d", bind, base+i), cfg.MaxConnections, cfg.MaxConcurrentStreams)
			require.NoError(t, err)
			cert, err := tlsid.BuildServerCert(pv, sysChainID)
			require.NoError(t, err)
			creds := fibregrpc.NewDetectingServerCreds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}))
			pub.RegisterWithUploadBufferReuse(srv, cfg.MaxMessageSize, params.MaxRowsPerValidator(), params.MerkleProofDepth(),
				grpclib.MaxSendMsgSize(cfg.MaxMessageSize),
				grpclib.Creds(tlsOnlyCreds{creds}))
			pub.Serve()
		}
	}

	go func() {
		for {
			time.Sleep(time.Duration(sysEnvInt("SYS_SINK_TICK_MS", 1000)) * time.Millisecond)
			c, io := sysRusage(), sysIO()
			var ms runtime.MemStats
			runtime.ReadMemStats(&ms)
			pools, _ := json.Marshal(fibre.DefaultBlobConfigV0().MemoryStats())
			fmt.Fprintf(os.Stderr, "TICK t=%d user_s=%.3f sys_s=%.3f rchar=%.0f wchar=%.0f syscr=%.0f syscw=%.0f rss=%d heap_inuse=%d heap_sys=%d sys=%d pools=%s\n",
				time.Now().UnixMilli(), c.user.Seconds(), c.sys.Seconds(), io["rchar"], io["wchar"], io["syscr"], io["syscw"], sysRSS(),
				ms.HeapInuse, ms.HeapSys, ms.Sys, pools)
		}
	}()

	blobs, _ := sysBlobs(t, dataSize)
	// Seeding is not measured; a long RPC timeout keeps slow disks from
	// causing retries.
	client := sysClient(t, makeTestKeyring(t), vals, addrs, 5*time.Minute, &sysRPCStats{})
	ns := share.MustNewV0Namespace([]byte("sysbench"))
	for _, blob := range blobs {
		_, err := client.Upload(t.Context(), ns, blob, fibre.WithAwaitAllSignatures())
		require.NoError(t, err)
		fmt.Printf("BLOB %s\n", blob.ID())
		blob.Free()
	}
	require.NoError(t, client.Stop(context.Background()))
	fmt.Printf("SYS_READY validators=%d blobs=%d tls_only=%v\n", n, len(blobs), tlsOnly)

	// SYS_CPUPROF profiles SYS_PROF_SECONDS of serving, starting SYS_PROF_DELAY
	// seconds after READY; SYS_HEAPPROF dumps the heap at the end of that window.
	if cpu, heap := os.Getenv("SYS_CPUPROF"), os.Getenv("SYS_HEAPPROF"); cpu != "" || heap != "" {
		time.Sleep(time.Duration(sysEnvInt("SYS_PROF_DELAY", 20)) * time.Second)
		if cpu != "" {
			f, err := os.Create(cpu)
			require.NoError(t, err)
			require.NoError(t, pprof.StartCPUProfile(f))
		}
		time.Sleep(time.Duration(sysEnvInt("SYS_PROF_SECONDS", 20)) * time.Second)
		pprof.StopCPUProfile()
		if heap != "" {
			f, err := os.Create(heap)
			require.NoError(t, err)
			require.NoError(t, pprof.Lookup("heap").WriteTo(f, 0))
			require.NoError(t, f.Close())
		}
		fmt.Printf("SYS_PROFILED\n")
	}
	select {}
}

// TestSystemDownload downloads the blobs of sysBlobs from a TestSystemServe
// process at SYS_SERVER with SYS_WORKERS concurrent downloads.
func TestSystemDownload(t *testing.T) {
	if os.Getenv("SYS_MODE") != "download" {
		t.Skip("SYS_MODE=download")
	}
	n := sysEnvInt("SYS_VALIDATORS", 100)
	base := sysEnvInt("SYS_PORT", 21000)
	server := os.Getenv("SYS_SERVER")
	if server == "" {
		server = "127.0.0.1"
	}
	workers := sysEnvInt("SYS_WORKERS", 16)
	dur := time.Duration(sysEnvInt("SYS_SECONDS", 30)) * time.Second
	warm := time.Duration(sysEnvInt("SYS_WARMUP", 5)) * time.Second
	dataSize := sysEnvInt("SYS_BYTES", sysReadBlobBytes)

	vals, _ := sysValidators(n)
	addrs := map[string]string{}
	for i, v := range vals {
		addrs[v.Address.String()] = fmt.Sprintf("%s:%d", server, base+i)
	}
	blobs, datas := sysBlobs(t, dataSize)
	ids := make([]fibre.BlobID, len(blobs))
	for i, blob := range blobs {
		ids[i] = blob.ID()
		blob.Free()
	}
	var stats sysRPCStats
	client := sysClient(t, nil, vals, addrs, 0, &stats)
	defer func() { _ = client.Stop(context.Background()) }()

	got, err := client.Download(t.Context(), ids[0])
	require.NoError(t, err)
	require.True(t, bytes.Equal(got.Data(), datas[0]), "downloaded data mismatch")
	got.Free()

	var iter atomic.Int64
	one := func(ctx context.Context, _ int) ([]time.Duration, error) {
		id := ids[int(iter.Add(1))%len(ids)]
		t0 := time.Now()
		blob, err := client.Download(ctx, id)
		if err != nil {
			return nil, err
		}
		blob.Free()
		return []time.Duration{time.Since(t0)}, nil
	}
	calls0, rows0, errs0 := stats.calls.Load(), stats.rows.Load(), stats.errs.Load()
	res := sysMeasure(t, workers, warm, dur, dataSize, []string{"dl"}, one)
	// The counters also cover the warmup and the drain, so these are approximate.
	done := res["blobs"].(float64)
	res["mode"] = "download"
	res["rpcs_per_blob"] = float64(stats.calls.Load()-calls0) / done
	res["rows_per_blob"] = float64(stats.rows.Load()-rows0) / done
	res["rpc_errs_per_blob"] = float64(stats.errs.Load()-errs0) / done
	sysPrintResult(res)
}
