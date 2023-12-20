package network

import (
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"

	"github.com/testground/sdk-go/runtime"
)

const (
	TopologyParam         = "topology"
	ConnectAllTopology    = "connect_all"
	ConnectRandomTopology = "connect_random"
	SeedTopology          = "seed"
	SeedGroupID           = "seeds"
)

func DefaultTopologies() []string {
	return []string{
		ConnectAllTopology,
	}
}

// GetConfigurators
func GetConfigurators(runenv *runtime.RunEnv) ([]Configurator, error) {
	topology := runenv.StringParam(TopologyParam)
	if topology == "" {
		topology = ConnectAllTopology
	}
	ops := make([]Configurator, 0)
	switch topology {
	case ConnectAllTopology:
		ops = append(ops, ConnectAll)
	case ConnectRandomTopology:
		ops = append(ops, ConnectRandom(10))
	case SeedTopology:
		// don't do anything since we are manually adding peers to the address book
	default:
		return nil, fmt.Errorf("unknown topology func: %s", topology)
	}

	ops = append(ops, TracingConfigurator(runenv, ParseTracingParams(runenv)))

	return ops, nil
}

// Configurator is a function that arbitrarily modifies the provided node
// configurations. It is used to generate the topology (which nodes are
// connected to which) of the network, along with making other arbitrary changes
// to the configs.
type Configurator func(nodes []RoleConfig) ([]RoleConfig, error)

var _ = Configurator(ConnectAll)

// ConnectAll is a Configurator that connects all nodes to each other via
// persistent peers.
func ConnectAll(nodes []RoleConfig) ([]RoleConfig, error) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].GlobalSequence < nodes[j].GlobalSequence
	})
	peerIDs := peerIDs(nodes)

	// For each node, generate the string that excludes its own P2PID
	for i, nodeConfig := range nodes {
		var filteredP2PIDs []string
		for _, pid := range peerIDs {
			if pid != nodeConfig.PeerID {
				filteredP2PIDs = append(filteredP2PIDs, pid)
			}
		}

		// Here you could put the concatenated string into another field in NodeConfig
		// or do whatever you want with it.
		nodeConfig.CmtConfig.P2P.PersistentPeers = strings.Join(filteredP2PIDs, ",")
		nodes[i] = nodeConfig
	}

	return nodes, nil
}

var _ = Configurator(ConnectRandom(1))

func ConnectRandom(numPeers int) Configurator {
	return func(nodes []RoleConfig) ([]RoleConfig, error) {
		if numPeers >= len(nodes) {
			return nil, errors.New("numPeers should be less than the total number of nodes")
		}

		for i, nodeConfig := range nodes {
			// Shuffle the indexes for each nodeConfig
			indexes := rand.Perm(len(nodes))

			var chosenPeers []string

			for _, idx := range indexes {
				potentialPeer := nodes[idx]

				if len(chosenPeers) >= numPeers {
					break
				}
				if potentialPeer.PeerID != nodeConfig.PeerID {
					chosenPeers = append(chosenPeers, potentialPeer.PeerID)
				}
			}

			nodeConfig.CmtConfig.P2P.PersistentPeers = strings.Join(chosenPeers, ",")
			nodes[i] = nodeConfig
		}

		return nodes, nil
	}
}

var _ = Configurator(SeedConfigurator)

// SeedConfigurator is a Configurator that finds and sets the seed node for the
// network. Note that this only supports a single seed node for the time being.
func SeedConfigurator(nodes []RoleConfig) ([]RoleConfig, error) {
	// find the seed node
	var seedNode RoleConfig
	for i, node := range nodes {
		if node.GroupID == SeedGroupID {
			seedNode = node
			nodes[i].CmtConfig.P2P.SeedMode = true
			break
		}
	}
	if seedNode.PeerID == "" {
		return nil, fmt.Errorf("expected at one seed node, found none")
	}
	// add the seed node to all of the peers
	for i, node := range nodes {
		if node.GroupID != SeedGroupID {
			nodes[i].CmtConfig.P2P.Seeds = strings.Join([]string{seedNode.PeerID}, ",")
		}
	}
	return nodes, nil
}

func TracingConfigurator(runenv *runtime.RunEnv, tparams TracingParams) Configurator {
	return func(nodes []RoleConfig) ([]RoleConfig, error) {
		runenv.RecordMessage(fmt.Sprintf("tracing nodes: %+v", tparams))

		tracedNodes := 0
		for i := 0; i < len(nodes) && tracedNodes < tparams.Nodes; i++ {
			if nodes[i].GroupID == SeedGroupID {
				continue
			}
			runenv.RecordMessage(fmt.Sprintf("tracing node %+v", nodes[i]))
			nodes[i].CmtConfig.Instrumentation.InfluxOrg = "celestia"
			nodes[i].CmtConfig.Instrumentation.InfluxBucket = "testground"
			nodes[i].CmtConfig.Instrumentation.InfluxBatchSize = 500
			nodes[i].CmtConfig.Instrumentation.InfluxURL = tparams.Url
			nodes[i].CmtConfig.Instrumentation.InfluxToken = tparams.Token
			tracedNodes++
		}

		return nodes, nil
	}
}
