package e2e

import (
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"github.com/tendermint/tendermint/rpc/client/http"
)

const (
	secp256k1Type               = "secp256k1"
	ed25519Type                 = "ed25519"
	networkIPv4                 = "10.186.73.0/24"
	firstProxyPort       uint32 = 4201
	dockerSrcURL                = "ghcr.io/celestiaorg/celestia-app"
	randomSeed           int64  = 589308084734268
	defaultAccountTokens        = 1e6
	rpcPort                     = 26657
)

type Testnet struct {
	Name     string // also used as the chain-id
	Dir      string
	IP       *net.IPNet
	Nodes    map[string]*Node
	Accounts map[string]*Account
}

type Node struct {
	Name           string
	Versions       []string
	StartHeight    int64
	Peers          []string
	SignerKey      crypto.PrivKey
	NetworkKey     crypto.PrivKey
	AccountKey     crypto.PrivKey
	IP             net.IP
	ProxyPort      uint32
	SelfDelegation int64
}

type Account struct {
	Name   string
	Tokens int64
	Key    crypto.PrivKey
}

func LoadTestnet(manifest Manifest, file string) (*Testnet, error) {
	// the directory that the toml file is located in
	dir := strings.TrimSuffix(file, filepath.Ext(file))
	name := fmt.Sprintf("%s-%d", filepath.Base(dir), rand.Intn(math.MaxUint16))
	_, ipNet, err := net.ParseCIDR(networkIPv4)
	if err != nil {
		return nil, fmt.Errorf("invalid IP network address %q: %w", networkIPv4, err)
	}
	ipGen := newIPGenerator(ipNet)
	keyGen := newKeyGenerator(randomSeed)
	proxyPort := firstProxyPort

	testnet := &Testnet{
		Dir:      dir,
		Name:     name,
		Nodes:    make(map[string]*Node),
		Accounts: make(map[string]*Account),
		IP:       ipGen.Network(),
	}

	// deterministically sort names in alphabetical order
	nodeNames := []string{}
	for name := range manifest.Nodes {
		nodeNames = append(nodeNames, name)
	}
	sort.Strings(nodeNames)

	for _, name := range nodeNames {
		nodeManifest := manifest.Nodes[name]
		if _, ok := testnet.Nodes[name]; ok {
			return nil, fmt.Errorf("duplicate node name %s", name)
		}
		node := &Node{
			Name:           name,
			Versions:       nodeManifest.Versions,
			StartHeight:    nodeManifest.StartHeight,
			Peers:          nodeManifest.Peers,
			SignerKey:      keyGen.Generate(ed25519Type),
			NetworkKey:     keyGen.Generate(ed25519Type),
			AccountKey:     keyGen.Generate(secp256k1Type),
			SelfDelegation: nodeManifest.SelfDelegation,
			IP:             ipGen.Next(),
			ProxyPort:      proxyPort,
		}
		if len(node.Versions) == 0 {
			node.Versions = []string{"current"}
		}

		testnet.Nodes[name] = node
		proxyPort++
	}

	for name, node := range testnet.Nodes {
		// fill up the peers field if it is empty
		if len(node.Peers) == 0 {
			for otherName := range testnet.Nodes {
				if otherName == name {
					continue
				}
				node.Peers = append(node.Peers, otherName)
			}
		}
		// replace the peer names with the P2P address.
		for idx, peer := range node.Peers {
			node.Peers[idx] = testnet.Nodes[peer].AddressP2P(true)
		}
	}

	// deterministically sort accounts in alphabetical order
	accountNames := []string{}
	for name := range manifest.Accounts {
		accountNames = append(accountNames, name)
	}
	sort.Strings(accountNames)

	for _, name := range accountNames {
		accountManifest := manifest.Accounts[name]
		if _, ok := testnet.Accounts[name]; ok {
			return nil, fmt.Errorf("duplicate account name %s", name)
		}
		account := &Account{
			Name:   name,
			Tokens: accountManifest.Tokens,
			Key:    keyGen.Generate(accountManifest.KeyType),
		}
		if account.Tokens == 0 {
			account.Tokens = defaultAccountTokens
		}
		testnet.Accounts[name] = account
	}

	return testnet, testnet.Validate()
}

func (t *Testnet) Validate() (err error) {
	if len(t.Accounts) == 0 {
		return errors.New("at least one account is required")
	}
	if len(t.Nodes) == 0 {
		return errors.New("at least one node is required")
	}
	validators := 0
	for name, node := range t.Nodes {
		if err := node.Validate(); err != nil {
			return fmt.Errorf("invalid node %s: %w", name, err)
		}
		// must have at least one validator
		if node.SelfDelegation > 0 {
			validators++
		}
	}
	if validators == 0 {
		return errors.New("at least one node must a validator by having an associated account")
	}
	for _, account := range t.Accounts {
		if err := account.Validate(); err != nil {
			return err
		}
	}

	return nil
}

func (t *Testnet) GetAllVersions() []string {
	versions := make(map[string]struct{})
	// filter duplicate version strings
	for _, node := range t.Nodes {
		for _, version := range node.Versions {
			versions[version] = struct{}{}
		}
	}

	// convert to list
	versionsList := []string{}
	for version := range versions {
		versionsList = append(versionsList, version)
	}
	return versionsList
}

func (t *Testnet) NodesByStartHeight() []*Node {
	nodes := make([]*Node, 0, len(t.Nodes))
	for _, node := range t.Nodes {
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].StartHeight == nodes[j].StartHeight {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].StartHeight < nodes[j].StartHeight
	})
	return nodes
}

// Address returns a P2P endpoint address for the node.
func (n Node) AddressP2P(withID bool) string {
	addr := fmt.Sprintf("%v:26656", n.IP.String())
	if withID {
		addr = fmt.Sprintf("%x@%v", n.NetworkKey.PubKey().Address().Bytes(), addr)
	}
	return addr
}

// Address returns an RPC endpoint address for the node.
func (n Node) AddressRPC() string {
	return fmt.Sprintf("%v:%d", n.IP.String(), rpcPort)
}

func (n Node) IsValidator() bool {
	return n.SelfDelegation != 0
}

func (n Node) Client() (*http.HTTP, error) {
	return http.New(fmt.Sprintf("http://127.0.0.1:%v", n.ProxyPort), "/websocket")
}

func (n Node) Validate() error {
	if len(n.Versions) == 0 {
		return errors.New("at least one version is required")
	}
	if n.StartHeight < 0 {
		return errors.New("start height must be non-negative")
	}
	if n.SelfDelegation < 0 {
		return errors.New("self delegation must be non-negative")
	}
	return nil
}

func (a Account) Validate() error {
	if a.Tokens < 0 {
		return errors.New("tokens must be non-negative")
	}
	return nil
}

type keyGenerator struct {
	random *rand.Rand
}

func newKeyGenerator(seed int64) *keyGenerator {
	return &keyGenerator{
		random: rand.New(rand.NewSource(seed)), //nolint:gosec
	}
}

func (g *keyGenerator) Generate(keyType string) crypto.PrivKey {
	seed := make([]byte, ed25519.SeedSize)

	_, err := io.ReadFull(g.random, seed)
	if err != nil {
		panic(err) // this shouldn't happen
	}
	switch keyType {
	case "secp256k1":
		return secp256k1.GenPrivKeySecp256k1(seed)
	case "", "ed25519":
		return ed25519.GenPrivKeyFromSecret(seed)
	default:
		panic("KeyType not supported") // should not make it this far
	}
}

type ipGenerator struct {
	network *net.IPNet
	nextIP  net.IP
}

func newIPGenerator(network *net.IPNet) *ipGenerator {
	nextIP := make([]byte, len(network.IP))
	copy(nextIP, network.IP)
	gen := &ipGenerator{network: network, nextIP: nextIP}
	// Skip network and gateway addresses
	gen.Next()
	gen.Next()
	return gen
}

func (g *ipGenerator) Network() *net.IPNet {
	n := &net.IPNet{
		IP:   make([]byte, len(g.network.IP)),
		Mask: make([]byte, len(g.network.Mask)),
	}
	copy(n.IP, g.network.IP)
	copy(n.Mask, g.network.Mask)
	return n
}

func (g *ipGenerator) Next() net.IP {
	ip := make([]byte, len(g.nextIP))
	copy(ip, g.nextIP)
	for i := len(g.nextIP) - 1; i >= 0; i-- {
		g.nextIP[i]++
		if g.nextIP[i] != 0 {
			break
		}
	}
	return ip
}
