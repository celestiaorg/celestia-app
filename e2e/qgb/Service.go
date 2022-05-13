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

const (
	// represent the docker-compose services names
	CORE0     = "core0"
	COREOORCH = "core0-orch"
	CORE1     = "core1"
	CORE1ORCH = "core1-orch"
	CORE2     = "core2"
	CORE2ORCH = "core2-orch"
	CORE3     = "core3"
	CORE3ORCH = "core3-orch"
	DEPLOYER  = "deployer"
	RELAYER   = "relayer"
	GANACHE   = "ganache"
)

func (n Service) toString() (string, error) {
	switch n {
	case Core0:
		return CORE0, nil
	case Core0Orch:
		return COREOORCH, nil
	case Core1:
		return CORE1, nil
	case Core1Orch:
		return CORE1ORCH, nil
	case Core2:
		return CORE2, nil
	case Core2Orch:
		return CORE2ORCH, nil
	case Core3:
		return CORE3, nil
	case Core3Orch:
		return CORE3ORCH, nil
	case Deployer:
		return DEPLOYER, nil
	case Relayer:
		return RELAYER, nil
	case Ganache:
		return GANACHE, nil
	}
	return "", fmt.Errorf("unknown service")
}
