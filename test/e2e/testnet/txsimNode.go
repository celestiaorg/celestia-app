//nolint:staticcheck
package testnet

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/knuu/pkg/instance"
	"github.com/celestiaorg/knuu/pkg/knuu"
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
	logger *log.Logger,
	name string,
	version string,
	endpoint string,
	seed int64,
	blobSequences int,
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
	logger.Println("setting image for tx client", "name", name, "image", image)
	err = instance.Build().SetImage(ctx, image)
	if err != nil {
		logger.Println("failed to set image for tx client", "name", name, "image", image, "error", err)
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
		fmt.Sprintf("--blob %d", blobSequences),
		fmt.Sprintf("--blob-amounts %d", blobsPerSeq),
		fmt.Sprintf("--blob-sizes %s", blobRange),
		fmt.Sprintf("--blob-share-version %d", share.ShareVersionZero),
	}

	if len(upgradeSchedule) > 0 {
		args = append(args, fmt.Sprintf("--upgrade-schedule %s", stringifyUpgradeSchedule(upgradeSchedule)))
	}

	if err := instance.Build().SetArgs(args...); err != nil {
		return nil, err
	}

	logger.Println("created tx client", "name", name, "image", image, "args", strings.Join(args, " "))

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
