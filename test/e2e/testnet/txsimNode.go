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
) (*TxSim, error) {
	k, err := knuu.New(ctx)
	if err != nil {
		return nil, err
	}
	instance, err := k.NewInstance(name)
	if err != nil {
		return nil, err
	}
	image := txsimDockerImageName(version)
	log.Info().
		Str("name", name).
		Str("image", image).
		Msg("setting image for tx client")
	err = instance.SetImage(ctx, image)
	if err != nil {
		log.Err(err).
			Str("name", name).
			Str("image", image).
			Msg("failed to set image for tx client")
		return nil, err
	}
	err = instance.SetMemory(resources.MemoryRequest, resources.MemoryLimit)
	if err != nil {
		return nil, err
	}
	err = instance.SetCPU(resources.CPU)
	if err != nil {
		return nil, err
	}
	err = instance.AddVolumeWithOwner(volumePath, resources.Volume, 10001)
	if err != nil {
		return nil, err
	}
	args := []string{
		fmt.Sprintf("-k %d", 0),
		fmt.Sprintf("-g %s", endpoint),
		fmt.Sprintf("-t %ds", pollTime),
		fmt.Sprintf("-b %d ", sequences),
		fmt.Sprintf("-d %d ", seed),
		fmt.Sprintf("-a %d ", blobsPerSeq),
		fmt.Sprintf("-s %s ", blobRange),
	}

	err = instance.SetArgs(args...)
	if err != nil {
		return nil, err
	}

	return &TxSim{
		Name:     name,
		Instance: instance,
	}, nil
}
