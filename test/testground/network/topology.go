package network

import (
	"github.com/testground/sdk-go/runtime"
)

type TopologyFn func(runenv *runtime.RunEnv, nodes map[string]NodeConfig) (map[string]NodeConfig, error)

// // ConnectAll is a TopologyFn that connects all nodes to each other via
// // persistent peers.
// func ConnectAll() TopologyFn {
// 	return func(runenv *runtime.RunEnv, nodes map[string]NodeConfig) (map[string]NodeConfig, error) {
// 		peerIDs := make([]string, 0, len(nodes))
// 		for _, nodeCfg := range nodes {
// 			peerIDs = append(peerIDs, nodeCfg.P2PID)
// 		}
// 		var peersStr bytes.Buffer
// 		for i, nodeCfg := range nodes {
// 			for j, peerID := range peerIDs {
// 				if i == j {
// 					continue
// 				}
// 				peersStr.WriteString(fmt.Sprintf("%s,", peerID))
// 			}
// 		}

// 	}
// }
