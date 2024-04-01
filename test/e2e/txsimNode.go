package e2e

import (
	"fmt"

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
	Instance *knuu.Instance
}

func CreateTxSimNode(
	name, version string,
	endpoint string,
	rpcEndpoint string,
	seed int,
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
	fmt.Println("txsim version", txsimDockerImageName(version))
	err = instance.SetImage(txsimDockerImageName(version))
	if err != nil {
		fmt.Println("err setting image")
		return nil, err
	}
	err = instance.SetMemory(resources.memoryRequest, resources.memoryLimit)
	if err != nil {
		return nil, err
	}
	err = instance.SetCPU(resources.cpu)
	if err != nil {
		return nil, err
	}
	err = instance.AddVolumeWithOwner(volumePath, resources.volume, 10001)
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
		//fmt.Sprintf("-r %s", rpcEndpoint),
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
