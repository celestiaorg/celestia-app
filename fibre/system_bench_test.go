package fibre_test

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"os"
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
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// System benchmark of the write path across two processes. Run the sink with
// SYS_MODE=sink and the client with SYS_MODE=client (or encode) on the same host.

const sysChainID = "celestia"

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
		time.Sleep(10 * time.Second)
		var h int64
		for _, s := range sinks {
			h += s.handled.Load()
		}
		fmt.Fprintf(os.Stderr, "sink handled=%d\n", h)
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
		cfg := fibre.DefaultClientConfig()
		cfg.NewClientFn = fibregrpc.DefaultNewClientFn(&testHostRegistry{addresses: addrs}, func() string { return sysChainID }, cfg.MaxMessageSize, nil)
		valSet := validator.Set{ValidatorSet: core.NewValidatorSet(vals), Height: 100}
		cfg.StateClientFn = func() (state.Client, error) {
			return &mockStateClient{SetGetter: &mockValidatorSetGetter{set: valSet}, chainID: sysChainID}, nil
		}
		var err error
		client, err = fibre.NewClient(makeTestKeyring(t), cfg)
		require.NoError(t, err)
		require.NoError(t, client.Start(t.Context()))
		defer func() { _ = client.Stop(context.Background()) }()
	}
	ns := share.MustNewV0Namespace([]byte("sysbench"))
	cfg := fibre.DefaultBlobConfigV0()

	datas := make([][]byte, workers)
	for i := range datas {
		datas[i] = make([]byte, dataSize)
		_, _ = rand.Read(datas[i])
	}

	var (
		measuring atomic.Bool
		stop      atomic.Bool
		done      atomic.Int64
		mu        sync.Mutex
		encMs     []float64
		upMs      []float64
		totMs     []float64
		peakRSS   atomic.Int64
		peakGor   atomic.Int64
	)
	one := func(ctx context.Context, data []byte) error {
		t0 := time.Now()
		blob, err := fibre.NewBlob(data, cfg)
		if err != nil {
			return err
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
			return err
		}
		if os.Getenv("SYS_TRACE") != "" {
			fmt.Printf("BLOB enc_ms=%d up_ms=%d\n", t1.Sub(t0).Milliseconds(), t2.Sub(t1).Milliseconds())
		}
		if measuring.Load() {
			done.Add(1)
			mu.Lock()
			encMs = append(encMs, float64(t1.Sub(t0).Microseconds())/1e3)
			upMs = append(upMs, float64(t2.Sub(t1).Microseconds())/1e3)
			totMs = append(totMs, float64(t2.Sub(t0).Microseconds())/1e3)
			mu.Unlock()
		}
		return nil
	}

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for w := range workers {
		wg.Go(func() {
			for !stop.Load() {
				if err := one(t.Context(), datas[w]); err != nil {
					errs <- err
					return
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
	m0, c0, w0 := sysReadMetrics(), sysRusage(), time.Now()
	measuring.Store(true)
	time.Sleep(dur)
	measuring.Store(false)
	m1, c1, w1 := sysReadMetrics(), sysRusage(), time.Now()
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
		"mode": mode, "workers": workers, "gomaxprocs": runtime.GOMAXPROCS(0), "blobs": blobs, "wall_s": wall,
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
		"enc_ms_p50":         pct(encMs, 0.5), "enc_ms_p90": pct(encMs, 0.9),
		"up_ms_p50": pct(upMs, 0.5), "up_ms_p90": pct(upMs, 0.9),
		"tot_ms_p50": pct(totMs, 0.5), "tot_ms_p90": pct(totMs, 0.9),
	}
	b, _ := json.Marshal(res)
	fmt.Printf("SYSRESULT %s\n", b)
}
