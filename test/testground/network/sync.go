package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/testground/sdk-go/network"
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
	ConfigTopic = sync.NewTopic("network-config", Config{})
)

type Config struct {
	ChainID string          `json:"chain_id"`
	Genesis json.RawMessage `json:"genesis"`
	Nodes   []NodeConfig    `json:"nodes"`
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

func ConfigureNetwork(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	initCtx.NetClient.MustWaitNetworkInitialized(ctx)

	config, err := CreateNetworkConfig(runenv, initCtx)
	if err != nil {
		return err
	}

	return initCtx.NetClient.ConfigureNetwork(ctx, &config)
}

func CreateNetworkConfig(runenv *runtime.RunEnv, initCtx *run.InitContext) (network.Config, error) {
	bandwidth, err := parseBandwidth(runenv.StringParam("bandwidth"))
	if err != nil {
		return network.Config{}, err
	}
	config := network.Config{
		Network: "default",
		Enable:  true,
		Default: network.LinkShape{
			Latency:   time.Duration(runenv.IntParam("latency")),
			Bandwidth: bandwidth,
		},
		CallbackState: "network-configured",
		RoutingPolicy: network.AllowAll,
	}

	config.IPv4 = runenv.TestSubnet

	// using the assigned `GlobalSequencer` id per each of instance
	// to fill in the last 2 octets of the new IP address for the instance
	ipC := byte((initCtx.GlobalSeq >> 8) + 1)
	ipD := byte(initCtx.GlobalSeq)
	config.IPv4.IP = append(config.IPv4.IP[0:2:2], ipC, ipD)

	return config, nil
}

func parseBandwidth(s string) (uint64, error) {
	var multiplier uint64

	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "Kib") {
		multiplier = 1 << 10
	} else if strings.HasSuffix(s, "Mib") {
		multiplier = 1 << 20
	} else if strings.HasSuffix(s, "Gib") {
		multiplier = 1 << 30
	} else if strings.HasSuffix(s, "Tib") {
		multiplier = 1 << 40
	} else if strings.HasSuffix(s, "Kb") {
		multiplier = 1000
	} else if strings.HasSuffix(s, "Mb") {
		multiplier = 1000 * 1000
	} else if strings.HasSuffix(s, "Gb") {
		multiplier = 1000 * 1000 * 1000
	} else if strings.HasSuffix(s, "Tb") {
		multiplier = 1000 * 1000 * 1000 * 1000
	} else {
		return 0, fmt.Errorf("unknown unit in string")
	}

	numberStr := strings.TrimRight(s, "KibMibGibTibKBMGBT")
	number, err := strconv.ParseFloat(numberStr, 64)
	if err != nil {
		return 0, err
	}

	return uint64(number * float64(multiplier)), nil
}

// Given the first two octets as a string (e.g., "192.168")
// and a slice of GlobalSeq values,
// this function returns a slice of full IP address strings.
func calculateIPAddresses(baseIP string, globalSequence int) string {
	ipC := byte((globalSequence >> 8) + 1)
	ipD := byte(globalSequence)
	fullIP := fmt.Sprintf("%s.%d.%d", baseIP, ipC, ipD)

	return fullIP
}
