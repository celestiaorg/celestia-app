package version

const (
	MochaChainID           = "mocha"
	Arabica4ChainID        = "arabica-4"
	Blockspacerace0ChainID = "blockspacerace-0"
)

func InitGetters() map[string]VersionGetter {
	version1Only, err := NewVersionGetter(map[uint64]int64{
		1: 0,
	})
	if err != nil {
		panic(err)
	}
	return map[string]VersionGetter{}
}
