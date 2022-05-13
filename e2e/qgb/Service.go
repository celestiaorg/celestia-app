package e2e

import "fmt"

type Service int64

const (
	Core0 Service = iota
	Core0Orch
	Core1
	Core1Orch
	Core2
	Core2Orch
	Core3
	Core3Orch
	Deployer
	Relayer
	Ganache
)

func (n Service) toString() (string, error) {
	switch n {
	case Core0:
		return "core0", nil
	case Core0Orch:
		return "core0-orch", nil
	case Core1:
		return "core1", nil
	case Core1Orch:
		return "core1-orch", nil
	case Core2:
		return "core2", nil
	case Core2Orch:
		return "core2-orch", nil
	case Core3:
		return "core3", nil
	case Core3Orch:
		return "core3-orch", nil
	case Deployer:
		return "deployer", nil
	case Relayer:
		return "relayer", nil
	case Ganache:
		return "ganache", nil
	}
	return "", fmt.Errorf("Unknown service")
}
