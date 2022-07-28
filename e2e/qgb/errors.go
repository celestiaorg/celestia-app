package e2e

import "errors"

var (
	ErrNetworkStopped      = errors.New("network is stopping")
	ErrRelayerStart        = errors.New("relayer didn't start correctly")
	ErrOrchestratorStart   = errors.New("orchestrator didn't start correctly")
	ErrQGBContractNotFound = errors.New("couldn't find deployed qgb contract")
	ErrConfirmNotFound     = errors.New("couldn't find confirm")
	ErrValsetNotFound      = errors.New("couldn't find valset")
	ErrHeightNotReached    = errors.New("couldn't reach wanted heigh")
	ErrNodeStart           = errors.New("node didn't start correctly")
	ErrEmpty               = errors.New("empty")
)
