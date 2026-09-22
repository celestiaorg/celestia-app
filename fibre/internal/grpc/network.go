package grpc

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	core "github.com/cometbft/cometbft/types"
	grpclib "google.golang.org/grpc"
)

// NetworkConfig pairs local source IPs with each validator's endpoints by index.
// Validator keys are uppercase hexadecimal consensus addresses. Missing peers fail closed.
type NetworkConfig struct {
	SourceIPs  []string            `json:"source_ips"`
	Validators map[string][]string `json:"validators"`
}

// Validate checks one or two distinct IP paths per validator.
func (n NetworkConfig) Validate() error {
	if len(n.SourceIPs) < 1 || len(n.SourceIPs) > 2 {
		return errors.New("network requires one or two source IPs")
	}
	sources := make(map[string]bool)
	for _, source := range n.SourceIPs {
		ip := net.ParseIP(source)
		if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
			return fmt.Errorf("invalid source IP %q", source)
		}
		if sources[ip.String()] {
			return fmt.Errorf("duplicate source IP %q", source)
		}
		sources[ip.String()] = true
	}
	if len(n.Validators) == 0 {
		return errors.New("network requires validator endpoints")
	}
	for address, endpoints := range n.Validators {
		decoded, err := hex.DecodeString(address)
		if err != nil || len(decoded) != 20 || address != strings.ToUpper(address) {
			return fmt.Errorf("invalid consensus address %q", address)
		}
		if len(endpoints) != len(n.SourceIPs) {
			return fmt.Errorf("validator %s: endpoint count must match source IP count", address)
		}
		seen := make(map[string]bool)
		for i, endpoint := range endpoints {
			host, port, err := net.SplitHostPort(endpoint)
			ip := net.ParseIP(host)
			p, portErr := strconv.Atoi(port)
			if err != nil || ip == nil || ip.IsUnspecified() || ip.IsMulticast() || portErr != nil || p < 1 || p > 65535 {
				return fmt.Errorf("invalid endpoint %q", endpoint)
			}
			if (ip.To4() == nil) != (net.ParseIP(n.SourceIPs[i]).To4() == nil) {
				return fmt.Errorf("endpoint %q and source must use the same IP family", endpoint)
			}
			if seen[ip.String()] {
				return fmt.Errorf("duplicate endpoint IP %q", host)
			}
			seen[ip.String()] = true
		}
	}
	return nil
}

type fixedHost validator.Host

func (h fixedHost) GetHost(context.Context, *core.Validator) (validator.Host, error) {
	return validator.Host(h), nil
}

// NetworkNewClientFn creates independently authenticated, source-bound paths.
func NetworkNewClientFn(n NetworkConfig, chainID func() string, maxMsgSize int, log *slog.Logger) NewClientFn {
	if err := n.Validate(); err != nil {
		return func(context.Context, *core.Validator) (Client, error) { return nil, err }
	}
	// Copy configuration so callers cannot mutate active routing tables.
	sources := append([]string(nil), n.SourceIPs...)
	peers := make(map[string][]string, len(n.Validators))
	for address, endpoints := range n.Validators {
		peers[address] = append([]string(nil), endpoints...)
	}
	return func(ctx context.Context, val *core.Validator) (Client, error) {
		endpoints, ok := peers[val.Address.String()]
		if !ok {
			return nil, fmt.Errorf("no network endpoints for validator %s", val.Address.String())
		}
		client := &pathClient{}
		for i, endpoint := range endpoints {
			path, err := newClientFn(fixedHost(endpoint), chainID, maxMsgSize, log, sources[i])(ctx, val)
			if err != nil {
				_ = client.Close()
				return nil, err
			}
			client.paths = append(client.paths, path)
		}
		client.uploadBytes = make([]int64, len(client.paths))
		client.downloads = make([]int64, len(client.paths))
		return client, nil
	}
}

type pathClient struct {
	paths                    []Client
	mu                       sync.Mutex
	uploadBytes, downloads   []int64
	nextUpload, nextDownload int
}

func (c *pathClient) acquire(weights []int64, next *int, weight int64) (Client, func()) {
	c.mu.Lock()
	best := *next
	for step := 1; step < len(weights); step++ {
		i := (*next + step) % len(weights)
		if weights[i] < weights[best] {
			best = i
		}
	}
	weights[best] += weight
	*next = (best + 1) % len(weights)
	c.mu.Unlock()
	return c.paths[best], func() { c.mu.Lock(); weights[best] -= weight; c.mu.Unlock() }
}

func (c *pathClient) UploadShard(ctx context.Context, req *types.UploadShardRequest, opts ...grpclib.CallOption) (*types.UploadShardResponse, error) {
	path, release := c.acquire(c.uploadBytes, &c.nextUpload, int64(req.Size()))
	defer release()
	return path.UploadShard(ctx, req, opts...)
}

func (c *pathClient) DownloadShard(ctx context.Context, req *types.DownloadShardRequest, opts ...grpclib.CallOption) (*types.DownloadShardResponse, error) {
	// Response sizes are unknown, so balance downloads by outstanding requests.
	path, release := c.acquire(c.downloads, &c.nextDownload, 1)
	defer release()
	return path.DownloadShard(ctx, req, opts...)
}

func (c *pathClient) Close() (err error) {
	for _, path := range c.paths {
		err = errors.Join(err, path.Close())
	}
	return err
}
