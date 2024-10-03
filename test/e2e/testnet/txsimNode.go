//nolint:staticcheck
package testnet

import (
	"context"
	"fmt"
	"strings"

	"github.com/celestiaorg/knuu/pkg/instance"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/rs/zerolog/log"
)

const (
	txsimDockerSrcURL = "ghcr.io/celestiaorg/txsim"
)

func txsimDockerImageName(version string) string {
	return fmt.Sprintf("%s:%s", txsimDockerSrcURL, version)
}

type TxSim struct {
	Name     string
	Instance *instance.Instance
}

// CreateTxClient returns a new TxSim instance.
func CreateTxClient(
	ctx context.Context,
	name string,
	version string,
	endpoint string,
	seed int64,
	sequences int,
	blobRange string,
	blobsPerSeq int,
	pollTime int,
	resources Resources,
	volumePath string,
	knuu *knuu.Knuu,
	upgradeSchedule map[int64]uint64,
) (*TxSim, error) {
	instance, err := knuu.NewInstance(name)
	if err != nil {
		return nil, err
	}
	image := txsimDockerImageName(version)
	err = instance.Build().SetImage(ctx, image)
	if err != nil {
		log.Err(err).
			Str("name", name).
			Str("image", image).
			Msg("failed to set image for tx client")
		return nil, err
	}
	err = instance.Resources().SetMemory(resources.MemoryRequest, resources.MemoryLimit)
	if err != nil {
		return nil, err
	}
	err = instance.Resources().SetCPU(resources.CPU)
	if err != nil {
		return nil, err
	}
	err = instance.Storage().AddVolumeWithOwner(volumePath, resources.Volume, 10001)
	if err != nil {
		return nil, err
	}
	args := []string{
		fmt.Sprintf("--key-path %s", volumePath),
		fmt.Sprintf("--grpc-endpoint %s", endpoint),
		fmt.Sprintf("--poll-time %ds", pollTime),
		fmt.Sprintf("--seed %d", seed),
		// fmt.Sprintf("--blob %d", sequences),
		// fmt.Sprintf("--blob-amounts %d", blobsPerSeq),
		// fmt.Sprintf("--blob-sizes %s", blobRange),
		fmt.Sprintf("--upgrade-schedule %s", stringifyUpgradeSchedule(upgradeSchedule)),
		// fmt.Sprintf("--blob-share-version %d", share.ShareVersionZero),
	}

	if err := instance.Build().SetArgs(args...); err != nil {
		return nil, err
	}

	log.Info().
		Str("name", name).
		Str("image", image).
		Str("args", strings.Join(args, " ")).
		Msg("created tx client")

	return &TxSim{
		Name:     name,
		Instance: instance,
	}, nil
}

func stringifyUpgradeSchedule(schedule map[int64]uint64) string {
	if schedule == nil {
		return ""
	}
	scheduleParts := make([]string, 0, len(schedule))
	for height, version := range schedule {
		scheduleParts = append(scheduleParts, fmt.Sprintf("%d:%d", height, version))
	}
	return strings.Join(scheduleParts, ",")
}
