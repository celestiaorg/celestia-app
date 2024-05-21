package testnet

import (
	"fmt"

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
	Instance *knuu.Instance
}

func CreateTxClient(
	name, version string,
	endpoint string,
	seed int64,
	sequences int,
	blobRange string,
	pollTime int,
	resources Resources,
	volumePath string,
) (*TxSim, error) {
	instance, err := knuu.NewInstance(name)
	if err != nil {
		return nil, err
	}
	image := txsimDockerImageName(version)
	log.Info().
		Str("name", name).
		Str("image", image).
		Msg("setting image for tx client")
	err = instance.SetImage(image)
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
		fmt.Sprintf("-a %d ", 5),
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
