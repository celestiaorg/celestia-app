package ethrpc

import txethereum "github.com/celestiaorg/celestia-app/v9/pkg/tx/ethereum"

const (
	// DevEthereumChainID is the numeric Ethereum chain ID used by local
	// celestiadev networks.
	DevEthereumChainID uint64 = txethereum.DevEthereumChainID

	mainnetEthereumChainID uint64 = 4200000001
	mochaEthereumChainID   uint64 = 4200000004
	arabicaEthereumChainID uint64 = 4200000011
)

// EthereumChainIDForCelestia returns the explicit numeric Ethereum chain ID
// assigned to a Celestia chain ID.
func EthereumChainIDForCelestia(chainID string) (uint64, error) {
	return txethereum.ChainIDForCelestia(chainID)
}
