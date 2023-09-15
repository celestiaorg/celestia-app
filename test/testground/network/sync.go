package network

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

const (
	FinishedConfigState = sync.State("finished-config")
)

var (
	GenesisTopic = sync.NewTopic("genesis", map[string]json.RawMessage{})
	// NetworkConfigTopic is the topic used to exchange network configuration
	// between test instances.
	ConfigTopic  = sync.NewTopic("network-config", Config{})
	StatusTopic  = sync.NewTopic("new-born-status", Status{})
	CommandTopic = sync.NewTopic("command", Command{})
)

type Config struct {
	ChainID string          `json:"chain_id"`
	Genesis json.RawMessage `json:"genesis"`
	Nodes   []NodeConfig    `json:"nodes"`
}

// Status is used by followers to signal to the leader that they are
// online and thier network config.
type Status struct {
	IP             string `json:"ip"`
	GlobalSequence int64  `json:"global_sequence"`
	GroupSequence  int64  `json:"group_sequence"`
	Group          string `json:"group"`
	NodeType       string `json:"node_type"`
}

func PublishConfig(ctx context.Context, initCtx *run.InitContext, cfg Config) error {
	_, err := initCtx.SyncClient.Publish(ctx, ConfigTopic, cfg)
	return err
}

func DownloadNetworkConfig(ctx context.Context, initCtx *run.InitContext) (Config, error) {
	cfgs, err := DownloadSync(ctx, initCtx, ConfigTopic, Config{}, 1)
	if err != nil {
		return Config{}, err
	}
	if len(cfgs) != 1 {
		return Config{}, errors.New("no network config was downloaded despite there not being an error")
	}
	return cfgs[0], nil
}

func SyncStatus(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) ([]Status, error) {
	ip, err := initCtx.NetClient.GetDataNetworkIP()
	if err != nil {
		return nil, err
	}

	ns := Status{
		IP:             ip.String(),
		GlobalSequence: initCtx.GlobalSeq,
		GroupSequence:  initCtx.GroupSeq,
		Group:          runenv.TestGroupID,
		NodeType:       runenv.TestGroupID,
	}

	err = publishStatus(ctx, runenv, initCtx, ns)
	if err != nil {
		return nil, err
	}

	stats, err := downloadStatuses(ctx, initCtx, runenv.TestInstanceCount)
	if err != nil {
		return nil, err
	}

	stats = append(stats, ns)

	// remove duplicate stats
	seen := make(map[string]struct{})
	uniqueStats := make([]Status, 0, runenv.TestInstanceCount)
	for _, s := range stats {
		if _, ok := seen[s.IP]; !ok {
			seen[s.IP] = struct{}{}
			uniqueStats = append(uniqueStats, s)
		}
	}

	return uniqueStats, nil
}

func publishStatus(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext, ns Status) error {
	_, err := initCtx.SyncClient.Publish(ctx, StatusTopic, ns)
	return err
}

func downloadStatuses(ctx context.Context, initCtx *run.InitContext, count int) ([]Status, error) {
	return DownloadSync(ctx, initCtx, StatusTopic, Status{}, count)
}

func DownloadSync[T any](ctx context.Context, initCtx *run.InitContext, topic *sync.Topic, t T, count int) ([]T, error) {
	ch := make(chan T)
	sub, err := initCtx.SyncClient.Subscribe(ctx, topic, ch)
	if err != nil {
		return nil, err
	}

	output := make([]T, 0, count)
	for i := 0; i < count; i++ {
		select {
		case err := <-sub.Done():
			if err != nil {
				return nil, err
			}
			return output, errors.New("subscription was closed before receiving the expected number of messages")
		case o := <-ch:
			output = append(output, o)
		}
	}
	return output, nil
}
