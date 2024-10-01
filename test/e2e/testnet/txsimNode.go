//nolint:staticcheck
package testnet

import (
	"context"
	"fmt"

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

func CreateTxClient(
	ctx context.Context,
	name, version string,
	endpoint string,
	seed int64,
	sequences int,
	blobRange string,
	blobsPerSeq int,
	pollTime int,
	resources Resources,
	volumePath string,
	knuu *knuu.Knuu,
) (*TxSim, error) {
	txIns, err := knuu.NewInstance(name)
	if err != nil {
		return nil, err
	}
	image := txsimDockerImageName(version)
	log.Info().
		Str("name", name).
		Str("image", image).
		Msg("setting image for tx client")
	err = txIns.Build().SetImage(ctx, image)
	if err != nil {
		log.Err(err).
			Str("name", name).
			Str("image", image).
			Msg("failed to set image for tx client")
		return nil, err
	}
	err = txIns.Resources().SetMemory(resources.MemoryRequest, resources.MemoryLimit)
	if err != nil {
		return nil, err
	}
	err = txIns.Resources().SetCPU(resources.CPU)
	if err != nil {
		return nil, err
	}
	err = txIns.Storage().AddVolumeWithOwner(volumePath, resources.Volume, 10001)
	if err != nil {
		return nil, err
	}
	args := []string{
		fmt.Sprintf("-grpc-endpoint %s", endpoint),
		fmt.Sprintf("-poll-time %ds", pollTime),
		fmt.Sprintf("-seed %d ", seed),
		fmt.Sprintf("-blob %d ", sequences),
		fmt.Sprintf("-blob-amounts %d ", blobsPerSeq),
		fmt.Sprintf("-blob-sizes %s ", blobRange),
	}

	if err := txIns.Build().SetArgs(args...); err != nil {
		return nil, err
	}

	return &TxSim{
		Name:     name,
		Instance: txIns,
	}, nil
}
