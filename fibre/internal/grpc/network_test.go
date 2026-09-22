package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/fibre/internal/tlsid"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
	grpclib "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

func TestNetworkConfigValidation(t *testing.T) {
	address := strings.Repeat("AB", 20)
	valid := func() NetworkConfig {
		return NetworkConfig{SourceIPs: []string{"127.0.0.1", "::1"}, Validators: map[string][]string{address: {"127.0.0.1:7980", "[::1]:7980"}}}
	}
	require.NoError(t, valid().Validate())
	for _, mutate := range []func(*NetworkConfig){
		func(n *NetworkConfig) { n.SourceIPs = nil },
		func(n *NetworkConfig) { n.SourceIPs[1] = n.SourceIPs[0] },
		func(n *NetworkConfig) { n.SourceIPs[0] = "0.0.0.0" },
		func(n *NetworkConfig) { n.Validators = nil },
		func(n *NetworkConfig) { n.Validators["bad"] = n.Validators[address] },
		func(n *NetworkConfig) { n.Validators[address] = []string{"127.0.0.1:7980"} },
		func(n *NetworkConfig) { n.Validators[address][1] = "127.0.0.1:7980" },
		func(n *NetworkConfig) { n.Validators[address][0] = "localhost:7980" },
		func(n *NetworkConfig) { n.Validators[address][0] = "127.0.0.1:0" },
	} {
		n := valid()
		mutate(&n)
		require.Error(t, n.Validate())
	}
}

type failingPath struct{ types.FibreClient }

func (f *failingPath) Close() error { return nil }
func (f *failingPath) UploadShard(context.Context, *types.UploadShardRequest, ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	return nil, errors.New("failed")
}

func TestPathAccounting(t *testing.T) {
	c := &pathClient{paths: []Client{&failingPath{}, &failingPath{}}, uploadBytes: make([]int64, 2), downloads: make([]int64, 2)}
	a, releaseA := c.acquire(c.uploadBytes, &c.nextUpload, 100)
	b, releaseB := c.acquire(c.uploadBytes, &c.nextUpload, 10)
	require.Same(t, c.paths[0], a)
	require.Same(t, c.paths[1], b)
	next, releaseNext := c.acquire(c.uploadBytes, &c.nextUpload, 20)
	require.Same(t, b, next)
	releaseA()
	releaseB()
	releaseNext()
	require.Equal(t, []int64{0, 0}, c.uploadBytes)
	_, err := c.UploadShard(context.Background(), &types.UploadShardRequest{}, nil...)
	require.Error(t, err)
	require.Equal(t, []int64{0, 0}, c.uploadBytes)
	for i := range 4 {
		path, release := c.acquire(c.downloads, &c.nextDownload, 1)
		require.Same(t, c.paths[i%2], path)
		release()
	}
}

type networkTestServer struct {
	types.UnimplementedFibreServer
	peers chan string
}

func (s *networkTestServer) DownloadShard(ctx context.Context, _ *types.DownloadShardRequest) (*types.DownloadShardResponse, error) {
	p, _ := peer.FromContext(ctx)
	host, _, _ := net.SplitHostPort(p.Addr.String())
	s.peers <- host
	return &types.DownloadShardResponse{}, nil
}

func TestNetworkPathsAndTLSIdentity(t *testing.T) {
	signer := core.NewMockPV()
	pubkey, err := signer.GetPubKey()
	require.NoError(t, err)
	val := core.NewValidator(pubkey, 1)
	start := func(ip string, signer core.PrivValidator) (string, chan string) {
		t.Helper()
		listener, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
		require.NoError(t, err)
		cert, err := tlsid.BuildServerCert(signer, "network-test")
		require.NoError(t, err)
		server := grpclib.NewServer(grpclib.Creds(credentials.NewTLS(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13})))
		service := &networkTestServer{peers: make(chan string, 2)}
		types.RegisterFibreServer(server, service)
		go func() { _ = server.Serve(listener) }()
		t.Cleanup(server.Stop)
		return listener.Addr().String(), service.peers
	}
	endpointA, peersA := start("127.0.0.1", signer)
	endpointB, peersB := start("::1", signer)
	cfg := NetworkConfig{SourceIPs: []string{"127.0.0.1", "::1"}, Validators: map[string][]string{val.Address.String(): {endpointA, endpointB}}}
	require.NoError(t, cfg.Validate())
	create := func() Client {
		client, err := NetworkNewClientFn(cfg, func() string { return "network-test" }, 1<<20, nil)(context.Background(), val)
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, client.Close()) })
		return client
	}
	client := create()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range 2 {
		_, err := client.DownloadShard(ctx, &types.DownloadShardRequest{})
		require.NoError(t, err)
	}
	require.Equal(t, "127.0.0.1", <-peersA)
	require.Equal(t, "::1", <-peersB)
	wrong, _ := start("::1", core.NewMockPV())
	cfg.Validators[val.Address.String()][1] = wrong
	client = create()
	_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{})
	require.NoError(t, err)
	_, err = client.DownloadShard(ctx, &types.DownloadShardRequest{})
	require.Error(t, err)
	cfg.Validators = map[string][]string{strings.Repeat("AB", 20): {endpointA, endpointB}}
	_, err = NetworkNewClientFn(cfg, func() string { return "network-test" }, 1<<20, nil)(ctx, val)
	require.ErrorContains(t, err, "no network endpoints")
}

type blockingPath struct {
	types.FibreClient
	index   int
	entered chan int
	fail    chan struct{}
}

func (p *blockingPath) Close() error { return nil }
func (p *blockingPath) UploadShard(ctx context.Context, _ *types.UploadShardRequest, _ ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	p.entered <- p.index
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.fail:
		return nil, errors.New("upload failed")
	}
}

func TestConcurrentPathAccounting(t *testing.T) {
	entered := make(chan int, 3)
	failed := make(chan struct{})
	c := &pathClient{paths: []Client{
		&blockingPath{index: 0, entered: entered, fail: failed},
		&blockingPath{index: 1, entered: entered, fail: failed},
	}, uploadBytes: make([]int64, 2), downloads: make([]int64, 2)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cancelled, cancelUpload := context.WithCancel(ctx)
	defer cancelUpload()
	results := make(chan error, 3)
	start := func(ctx context.Context, size, expectedPath int) {
		go func() {
			_, err := c.UploadShard(ctx, &types.UploadShardRequest{Shard: &types.BlobShard{Rlcs: make([]byte, size)}})
			results <- err
		}()
		select {
		case path := <-entered:
			require.Equal(t, expectedPath, path)
		case <-ctx.Done():
			t.Fatal("upload did not enter path")
		}
	}
	start(cancelled, 1000, 0)
	start(ctx, 10, 1)
	start(ctx, 20, 1)
	cancelUpload()
	require.ErrorIs(t, <-results, context.Canceled)
	c.mu.Lock()
	require.Zero(t, c.uploadBytes[0])
	require.Positive(t, c.uploadBytes[1])
	c.mu.Unlock()
	close(failed)
	require.ErrorContains(t, <-results, "upload failed")
	require.ErrorContains(t, <-results, "upload failed")
	require.Equal(t, []int64{0, 0}, c.uploadBytes)
}
