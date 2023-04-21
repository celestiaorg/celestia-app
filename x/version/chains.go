package version

const (
	MochaChainID          = "mocha"
	ArabicaChainID        = "arabica-4"
	BlockspaceraceChainID = "blockspacerace-0"
	MainnetChainID        = "celestia-1"
)

func StandardChainVersions() map[string]ChainVersionConfig {
	version0Only, err := NewChainVersionConfig(map[uint64]int64{
		0: 0,
	})
	if err != nil {
		panic(err)
	}
	mainnetVersions, err := NewChainVersionConfig(map[uint64]int64{
		1: 0,
	})
	if err != nil {
		panic(err)
	}
	return map[string]ChainVersionConfig{
		MochaChainID:          version0Only,
		ArabicaChainID:        version0Only,
		BlockspaceraceChainID: version0Only,
		MainnetChainID:        mainnetVersions,
	}
}
