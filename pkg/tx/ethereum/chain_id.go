package ethereum

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
)

const (
	// DevEthereumChainID is the numeric Ethereum chain ID used by local
	// celestiadev networks.
	DevEthereumChainID uint64 = 12345

	mainnetEthereumChainID uint64 = 4200000001
	mochaEthereumChainID   uint64 = 4200000004
	arabicaEthereumChainID uint64 = 4200000011
)

// ChainIDForCelestia returns the explicit numeric Ethereum chain ID assigned
// to a Celestia chain ID.
func ChainIDForCelestia(chainID string) (uint64, error) {
	switch chainID {
	case "celestiadev", appconsts.TestChainID, "test-app":
		return DevEthereumChainID, nil
	case appconsts.MainnetChainID:
		return mainnetEthereumChainID, nil
	case appconsts.MochaChainID:
		return mochaEthereumChainID, nil
	case appconsts.ArabicaChainID:
		return arabicaEthereumChainID, nil
	default:
		return 0, fmt.Errorf("no Ethereum chain ID mapping for Celestia chain ID %q", chainID)
	}
}
