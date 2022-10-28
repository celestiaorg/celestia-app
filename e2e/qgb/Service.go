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
	// represent the docker-compose network details
	// TODO maybe make a struct called Service containing this information
	CORE0               = "core0"
	CORE0ACCOUNTADDRESS = "celestia1ly9zghpffkw7gca42qkqm3awmw2zatc62yey3m"
	CORE0EVMADDRESS     = "0x966e6f22781EF6a6A82BBB4DB3df8E225DfD9488"
	COREOORCH           = "core0-orch"

	CORE1               = "core1"
	CORE1ACCOUNTADDRESS = "celestia1s67n6xvmslyyjj0ea930vrzhtcsty8aus3rpfq"
	CORE1EVMADDRESS     = "0x91DEd26b5f38B065FC0204c7929Da1b2A21877Ad"
	CORE1ORCH           = "core1-orch"

	CORE2               = "core2"
	CORE2ACCOUNTADDRESS = "celestia1l5y4swfghcfz8enf2l2c7fe3r6lna4rad4n862"
	CORE2EVMADDRESS     = "0x3d22f0C38251ebdBE92e14BBF1bd2067F1C3b7D7"
	CORE2ORCH           = "core2-orch"

	CORE3               = "core3"
	CORE3ACCOUNTADDRESS = "celestia1qde6jpujzx2nptvdjj5zzarv6wg0tklp4ymwv8"
	CORE3EVMADDRESS     = "0x3EE99606625E740D8b29C8570d855Eb387F3c790"
	CORE3ORCH           = "core3-orch"

	DEPLOYER = "deployer"
	RELAYER  = "relayer"
	GANACHE  = "ganache"
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
