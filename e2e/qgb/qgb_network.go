package e2e

import (
	"github.com/google/uuid"
	"github.com/testcontainers/testcontainers-go"
	"strings"
)

type QGBNetwork struct {
	ComposePaths []string
	Identifier   string
	Instance     *testcontainers.LocalDockerCompose
}

func NewQGBNetwork() (*QGBNetwork, error) {
	id := strings.ToLower(uuid.New().String())
	paths := []string{"./docker-compose.yml"}
	instance := testcontainers.NewLocalDockerCompose(paths, id)

	return &QGBNetwork{
		Identifier:   id,
		ComposePaths: paths,
		Instance:     instance,
	}, nil
}

// StartAll starts the whole QGB cluster with multiple validators, orchestrators and a relayer
// Make sure to release the ressources after finishing by calling the `StopAll()` method.
func (network QGBNetwork) StartAll() error {
	err := network.Instance.
		WithCommand([]string{"up", "-d"}).
		Invoke()
	if err.Error != nil {
		return err.Error
	}
	return nil
}

// StopAll stops the network and leaves the containers created. This allows to resume
// execution from the point where they stopped.
func (network QGBNetwork) StopAll() error {
	err := network.Instance.
		WithCommand([]string{"stop"}).
		Invoke()
	if err.Error != nil {
		return err.Error
	}
	return nil
}

// DeleteAll deletes the containers, network and everything related to the cluster.
func (network QGBNetwork) DeleteAll() error {
	err := network.Instance.
		WithCommand([]string{"down"}).
		Invoke()
	if err.Error != nil {
		return err.Error
	}
	return nil
}

// Start starts a service from the `Service` enum. Make sure to call `Stop`, in the
// end, to release the resources.
func (network QGBNetwork) Start(service Service) error {
	serviceName, err := service.toString()
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"up", "-d", serviceName}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

func (network QGBNetwork) Stop(service Service) error {
	serviceName, err := service.toString()
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand([]string{"stop", serviceName}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// TODO investigate the change on the Dockerfile from entrypoint to command
func (network QGBNetwork) ExecCommand(service Service, command []string) error {
	serviceName, err := service.toString()
	if err != nil {
		return err
	}
	err = network.Instance.
		WithCommand(append([]string{"exec", serviceName}, command...)).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// StartMinimal starts a network containing: 1 validator, 1 orchestrator, 1 relayer
// and a ganache instance
func (network QGBNetwork) StartMinimal() error {
	err := network.Instance.
		WithCommand([]string{"up", "-d", "core0", "core0-orch", "relayer", "ganache"}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}

// StartBase starts the very minimal component to have a network.
// It consists of starting `core0` as it is the genesis validator, and the docker network
// will be created along with it, allowing more containers to join it.
func (network QGBNetwork) StartBase() error {
	err := network.Instance.
		WithCommand([]string{"up", "-d", "core0"}).
		Invoke().Error
	if err != nil {
		return err
	}
	return nil
}
