package compositions

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	tmrand "github.com/tendermint/tendermint/libs/rand"

	"github.com/testground/sdk-go/network"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

func InitTest(runenv *runtime.RunEnv, initCtx *run.InitContext) (*run.InitContext, context.Context, context.CancelFunc, error) {
	runenv.RecordMessage("starting init test")
	syncclient := initCtx.SyncClient
	netclient := network.NewClient(syncclient, runenv)
	timeout, err := time.ParseDuration(runenv.TestInstanceParams["timeout"])
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	netclient.MustWaitNetworkInitialized(ctx)
	initCtx.NetClient = netclient

	config, err := CreateNetworkConfig(runenv, initCtx)
	if err != nil {
		return initCtx, ctx, cancel, err
	}

	err = initCtx.NetClient.ConfigureNetwork(ctx, &config)
	runenv.RecordMessage("configured network")
	return initCtx, ctx, cancel, err
}

func CreateNetworkConfig(runenv *runtime.RunEnv, initCtx *run.InitContext) (network.Config, error) {
	bandwidth, err := parseBandwidth(runenv.StringParam("bandwidth"))
	if err != nil {
		return network.Config{}, err
	}
	l := runenv.IntParam("latency")
	// rand.Intn will panic if l == 0
	if l == 0 {
		l = 1
	}
	if runenv.BooleanParam("random_latency") {
		l = tmrand.Intn(l)
	}
	config := network.Config{
		Network: "default",
		Enable:  true,
		Default: network.LinkShape{
			Latency:   time.Millisecond * time.Duration(l),
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

	runenv.RecordMessage(fmt.Sprintf("setup node \n ip: %s", config.IPv4.IP.String()))

	return config, nil
}

// parseBandwidth is a crude helper function to parse bandwidth strings. For
// example Kib, Kb, or KB are all valid units. Kb and KB are treated as 1000.
// Kib is 1024.
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
	} else if strings.HasSuffix(s, "Kb") || strings.HasSuffix(s, "KB") {
		multiplier = 1000
	} else if strings.HasSuffix(s, "Mb") || strings.HasSuffix(s, "MB") {
		multiplier = 1000 * 1000
	} else if strings.HasSuffix(s, "Gb") || strings.HasSuffix(s, "GB") {
		multiplier = 1000 * 1000 * 1000
	} else if strings.HasSuffix(s, "Tb") || strings.HasSuffix(s, "TB") {
		multiplier = 1000 * 1000 * 1000 * 1000
	} else {
		return 0, fmt.Errorf("unknown unit in string: %s", s)
	}

	numberStr := strings.TrimRight(s, "KMGTib")
	number, err := strconv.ParseFloat(numberStr, 64)
	if err != nil {
		return 0, err
	}

	return uint64(number * float64(multiplier)), nil
}
