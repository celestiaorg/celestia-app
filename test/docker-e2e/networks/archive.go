package networks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	rpctypes "github.com/cometbft/cometbft/rpc/core/types"
)

// defaultP2PPort is used when a node's listen address carries no port.
const defaultP2PPort = "26656"

// FetchArchivePeers returns the p2p addresses of the configured RPC nodes that
// still hold the full block history, so a node block syncing from genesis can
// fetch block 1 from them. Hardcoded archive peers go stale silently — an
// operator enables pruning or moves a port and the genesis sync test stalls at
// height 0 until it times out — so the addresses are read back from the nodes
// themselves on every run.
//
// Seeds are skipped even when they report the full history: a node in seed mode
// disconnects right after exchanging addresses and never serves a block, and
// listing one as a persistent peer only produces a reconnect loop.
func FetchArchivePeers(ctx context.Context, cfg *Config) (string, error) {
	if len(cfg.RPCs) == 0 {
		return "", errors.New("no RPC endpoints configured")
	}
	seeds := nodeIDs(cfg.Seeds)

	var (
		peers    []string
		seen     = make(map[string]struct{})
		failures []error
	)
	for i, endpoint := range cfg.RPCs {
		id, addr, err := archivePeer(ctx, endpoint, cfg.ChainID)
		if err != nil {
			failures = append(failures, fmt.Errorf("RPC %d: %w", i+1, err))
			continue
		}
		if _, ok := seeds[id]; ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		peers = append(peers, fmt.Sprintf("%s@%s", id, addr))
	}
	if len(peers) == 0 {
		return "", fmt.Errorf("no RPC exposed an archive peer: %w", errors.Join(failures...))
	}
	return strings.Join(peers, ","), nil
}

// archivePeer reads one node's p2p address off its own status, provided it
// serves the expected chain and still has block 1.
func archivePeer(ctx context.Context, endpoint, chainID string) (id, addr string, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	client, err := NewClient(endpoint)
	if err != nil {
		return "", "", err
	}
	status, err := client.Status(ctx)
	if err != nil {
		return "", "", err
	}
	if status == nil || status.NodeInfo.Network != chainID {
		return "", "", errors.New("RPC is not serving the expected chain")
	}
	if status.SyncInfo.EarliestBlockHeight > 1 {
		return "", "", fmt.Errorf("node is pruned: earliest block height %d", status.SyncInfo.EarliestBlockHeight)
	}
	id = string(status.NodeInfo.DefaultNodeID)
	if id == "" {
		return "", "", errors.New("RPC reported no node ID")
	}
	addr, err = p2pAddress(status, endpoint)
	if err != nil {
		return "", "", err
	}
	return id, addr, nil
}

// p2pAddress turns a node's advertised listen address into one that can be
// dialed from outside. Nodes commonly advertise a wildcard or loopback host, in
// which case the host the RPC was reached on is the routable one.
func p2pAddress(status *rpctypes.ResultStatus, endpoint string) (string, error) {
	listen := strings.TrimPrefix(strings.TrimPrefix(status.NodeInfo.ListenAddr, "tcp://"), "//")
	host, port := listen, defaultP2PPort
	if h, p, err := net.SplitHostPort(listen); err == nil {
		host, port = h, p
	}
	if !routableHost(host) {
		rpcHost, err := endpointHost(endpoint)
		if err != nil {
			return "", err
		}
		host = rpcHost
	}
	// CometBFT's persistent_peers format does not reliably handle IPv6.
	if ip := net.ParseIP(host); ip != nil && ip.To4() == nil {
		return "", fmt.Errorf("unsupported IPv6 peer address %q", host)
	}
	return net.JoinHostPort(host, port), nil
}

// routableHost reports whether a host can be dialed from another machine.
func routableHost(host string) bool {
	if host == "" || host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil {
		return !ip.IsUnspecified() && !ip.IsLoopback()
	}
	return true
}

// endpointHost extracts the host from an RPC endpoint, which may or may not
// carry a scheme (both "https://host:443" and "host:443" are accepted).
func endpointHost(endpoint string) (string, error) {
	if !strings.Contains(endpoint, "//") {
		endpoint = "//" + endpoint
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return "", err
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("RPC endpoint %q has no host", endpoint)
	}
	return parsed.Hostname(), nil
}

// nodeIDs indexes the node IDs of a comma-separated id@host:port peer list.
func nodeIDs(list string) map[string]struct{} {
	ids := make(map[string]struct{})
	for entry := range strings.SplitSeq(list, ",") {
		id, _, found := strings.Cut(strings.TrimSpace(entry), "@")
		if found && id != "" {
			ids[id] = struct{}{}
		}
	}
	return ids
}

// MergePeers combines peer lists into one, keeping the first address seen for
// each node ID so freshly discovered addresses win over hardcoded ones.
func MergePeers(lists ...string) string {
	var merged []string
	seen := make(map[string]struct{})
	for _, list := range lists {
		for entry := range strings.SplitSeq(list, ",") {
			entry = strings.TrimSpace(entry)
			id, _, found := strings.Cut(entry, "@")
			if !found || id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			merged = append(merged, entry)
		}
	}
	return strings.Join(merged, ",")
}
