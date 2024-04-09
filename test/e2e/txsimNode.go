package e2e

import (
	"fmt"
	"time"

	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/rs/zerolog/log"
)

const (
	txsimDockerSrcURL = "ghcr.io/celestiaorg/txsim"
	defaultTickerTime = 20 * time.Second
)

func txsimDockerImageName(version string) string {
	return fmt.Sprintf("%s:%s", txsimDockerSrcURL, version)
}

type TxSim struct {
	Name     string
	Instance *knuu.Instance
	ticker   *time.Ticker
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
		Msg("setting image for txsim node")
	err = instance.SetImage(image)
	if err != nil {
		log.Err(err).
			Str("name", name).
			Str("image", image).
			Msg("failed to set image for txsim node")
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
		fmt.Sprintf("-a %d ", 1),
		fmt.Sprintf("-s %s ", blobRange),
	}

	err = instance.SetArgs(args...)
	if err != nil {
		return nil, err
	}

	return &TxSim{
		Name:     name,
		Instance: instance,
		ticker:   time.NewTicker(defaultTickerTime),
	}, nil
}

func (txsim *TxSim) StartRoutine() {
	err := txsim.Instance.Start()
	if err != nil {
		log.Err(err).
			Str("name", txsim.Name).
			Msg("txsim failed to start")
	}
	log.Info().
		Str("name", txsim.Name).
		Msg("txsim started")

	txsim.ticker.Reset(defaultTickerTime)
	// check the state of the txsim every 20 seconds
	for {
		select {
		case <-txsim.ticker.C:
			if txsim.needsRestart() {
				log.Info().
					Str("name", txsim.Name).
					Msg("txsim is stopped, trying to restart it")

				err = txsim.Instance.Start()
				if err != nil {
					log.Err(err).
						Str("name", txsim.Name).
						Msg("txsim failed to re-start, trying later")
				}
				log.Info().
					Str("name", txsim.Name).
					Msg("txsim re-started")
			}
		}
	}
}

func (txsim *TxSim) CleanUp() {
	txsim.ticker.Stop()
	if txsim.Instance.IsInState(knuu.Started) {
		err := txsim.Instance.Stop()
		if err != nil {
			log.Err(err).
				Str("name", txsim.Name).
				Msg("txsim failed to stop")
		}
		err = txsim.Instance.WaitInstanceIsStopped()
		if err != nil {
			log.Err(err).
				Str("name", txsim.Name).
				Msg("txsim failed to stop")
		}
		err = txsim.Instance.Destroy()
		if err != nil {
			log.Err(err).
				Str("name", txsim.Name).
				Msg("txsim failed to cleanup")
		}
	}
}

func (txsim *TxSim) needsRestart() bool {
	// check if the txsim is running
	if isRunning, err := txsim.Instance.IsRunning(); err != nil && !isRunning {
		return true
	}
	// check if the txsim is stopped
	return txsim.Instance.IsInState(knuu.Stopped)
}
